package core

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"filippo.io/age"
)

// Encrypted backups are age files (https://age-encryption.org) sealed with a
// passphrase: scrypt turns the passphrase into the key, so there is no key file
// to keep safe, and the file can also be opened with the age command line tool.

// ScryptWorkFactor is the log2 of the scrypt cost of turning a passphrase into
// a key: 18, age's default, takes about a second, which is what makes guessing a
// passphrase slow. Only tests lower it, to keep themselves fast.
var ScryptWorkFactor = 18

// recipient is who a backup is sealed for: whoever knows the passphrase.
func recipient(passphrase string) (*age.ScryptRecipient, error) {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	r.SetWorkFactor(ScryptWorkFactor)
	return r, nil
}

// BackupEncrypted is Backup, with the snapshot sealed under passphrase. dest must
// not exist yet. The plain snapshot exists only for a moment, in a private
// folder next to dest, and is deleted again. The result is read back and
// decrypted once before the call returns, so a passphrase that was typed wrong
// cannot silently produce a backup nobody can open.
func (s *Local) BackupEncrypted(ctx context.Context, dest, passphrase string) error {
	if passphrase == "" {
		return errors.New("an encrypted backup needs a passphrase")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	work, err := os.MkdirTemp(dir, ".thaler-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	plain := filepath.Join(work, "snapshot.db")
	if err := s.Backup(ctx, plain); err != nil {
		return err
	}
	sum, err := seal(plain, dest, passphrase)
	if err != nil {
		os.Remove(dest)
		return fmt.Errorf("backup to %s: %w", dest, err)
	}
	if err := checkSealed(dest, passphrase, sum); err != nil {
		os.Remove(dest) // a backup that cannot be opened must not sit among the good ones
		return fmt.Errorf("backup to %s failed its check and was removed: %w", dest, err)
	}
	return nil
}

// DecryptBackup writes the database inside the encrypted backup src to dest,
// which must not exist yet, and checks that it is a healthy database. A wrong
// passphrase or a damaged file leaves nothing behind.
func DecryptBackup(ctx context.Context, src, dest, passphrase string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	r, srcFile, err := openDecrypted(src, passphrase)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		out.Close()
		os.Remove(dest)
		return fmt.Errorf("decrypting %s: %w", src, err)
	}
	if _, err := io.Copy(out, r); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		return fail(err)
	}
	if err := verifySnapshot(ctx, dest); err != nil {
		return fail(fmt.Errorf("the result is not a healthy database: %w", err))
	}
	return nil
}

// openDecrypted opens the encrypted backup src and returns what it contains, as
// a stream, and the open file, to be closed by the caller. A wrong passphrase and
// a file that is not an age file are reported before anything is read.
func openDecrypted(src, passphrase string) (io.Reader, io.Closer, error) {
	in, err := os.Open(src)
	if err != nil {
		return nil, nil, err
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	r, err := age.Decrypt(in, id)
	var noMatch *age.NoIdentityMatchError
	switch {
	case errors.As(err, &noMatch):
		in.Close()
		return nil, nil, fmt.Errorf("decrypting %s: incorrect passphrase", src)
	case err != nil:
		in.Close()
		return nil, nil, fmt.Errorf("decrypting %s: %w", src, err)
	}
	return r, in, nil
}

// seal encrypts the file at plain into a new file at dest and returns the
// SHA-256 of what was encrypted.
func seal(plain, dest, passphrase string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	rcpt, err := recipient(passphrase)
	if err != nil {
		return sum, err
	}
	in, err := os.Open(plain)
	if err != nil {
		return sum, err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return sum, err
	}
	w, err := age.Encrypt(out, rcpt)
	if err != nil {
		out.Close()
		return sum, err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), in); err != nil {
		out.Close()
		return sum, err
	}
	if err := w.Close(); err != nil {
		out.Close()
		return sum, err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, out.Close()
}

// checkSealed decrypts the file at path and compares what comes out with want.
func checkSealed(path, passphrase string, want [sha256.Size]byte) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return err
	}
	r, err := age.Decrypt(in, id)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return err
	}
	var got [sha256.Size]byte
	copy(got[:], h.Sum(nil))
	if got != want {
		return errors.New("decrypting it does not give back what was encrypted")
	}
	return nil
}
