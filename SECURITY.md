# Security

Thaler holds a record of your money, so a weakness in it matters even though it runs on your own machine. This page says how to report one, and what Thaler does and does not protect, so that you can judge what else you need.

## Reporting a vulnerability

Report it privately, through GitHub: on the repository's **Security** tab, choose **Report a vulnerability**. Please do not open a public issue, pull request or discussion for it.

Say what an attacker can do, which version (`thaler --version`) and system, and how to reproduce it. A file that triggers it is welcome; build it from invented data, never from a real bank export or a database you use.

What happens then:

- You hear back within a week, with whether it is taken as a vulnerability and what comes next.
- The fix is made on `main` and released at once, without waiting for the next train (see "A fix to a release" in [ROADMAP.md](ROADMAP.md#rel-the-release-train)).
- A GitHub security advisory is published with the release, crediting you unless you would rather not be named.

Only the latest release is supported: a fix goes into a new release, not into older ones.

## What Thaler protects

**Nothing leaves your machine.** Thaler makes no network connection: no bank sync, no telemetry, no update check. It never stores the credentials of a bank, because it never talks to one; transactions come from files you export yourself.

**Your files are yours alone.** The data folder (`~/.local/share/thaler`, or wherever `XDG_DATA_HOME` points) and the folders of backups inside it are created readable by you alone (`0700`), and so are the database and the files SQLite keeps beside it, wherever `--db` or `THALER_DB` puts them, the backups, the copy made before an upgrade, the files you export and a `config.toml` that Thaler writes (`0600`). A database that others can read is made private when Thaler opens it, and one whose permissions Thaler cannot change (another user's) is refused. A database a restore set aside before version `SAF-12` shipped keeps the permissions it had, inside the private folder.

**Encrypted backups.** A backup made with a passphrase is an [age](https://age-encryption.org) file, its key derived with scrypt at a cost of about a second per guess, and it is decrypted once before it is reported as made. The passphrase is never stored: it is typed on the terminal without being shown, or read from a file you name (`--passphrase-file`).

**Nothing is lost by itself.** A backup is checked for integrity before it is reported as done; a restore keeps the database it replaces; an upgrade copies the database before changing it; importing the same file twice adds nothing; pruning lists what it would remove and asks.

**Files from outside are treated as untrusted.** Bank exports (CSV, OFX, QIF) and `config.toml` are read strictly: a file that does not make sense is refused with a reason, not half read, and an unknown key in the config is an error. The parsers are fuzzed (`make fuzz`) for crashes and for reading a value other than the one written.

**The build.** Every tool is pinned to a version. govulncheck scans the dependencies and the standard library on every change and once a week, and Dependabot proposes updates weekly. The release archives come with a `checksums.txt`, signed from `v0.3.0` on by the Release workflow with cosign, keyless through Sigstore: the signature shows that the checksums, and so every archive, were built by `.github/workflows/release.yml` of this repository from `main` or a release branch. The README shows how to check it.

## What it does not protect

- **The live database is not encrypted.** Anyone who can read the file can read your finances. Use full-disk encryption (FileVault on macOS, LUKS on Linux) and a screen lock.
- **Anything that runs as you can read everything Thaler can**: malware, a compromised program, a person at your unlocked computer. Thaler offers no protection against a compromised machine or account.
- **Plain backups are plain copies.** Where you put them is up to you: a backup in a synced cloud folder is your finances in that cloud, unless it is encrypted.
- **A passphrase is as strong as you make it.** An encrypted backup can be attacked offline, without limit on attempts, by whoever holds the file; a short passphrase will fall. A passphrase file is as secret as that file. A lost passphrase cannot be recovered, and neither can the backup.
- **Exports are plain text.** A CSV you export, or print to the terminal, is readable by whatever reads it next.
- **What is on screen and in your shell.** Amounts show on screen; a command such as `thaler cli add --amount …` is kept in your shell's history.
- **The signature vouches for the workflow, not for the code.** It shows which workflow built a release, not that the code it built is sound: someone who could push to `main` or change the workflow could get a release signed. That rests on the repository's protection (`protect-main.sh`) and on GitHub. Releases before `v0.3.0` are not signed, and the binaries are not signed or notarized by Apple.
- **One person, one machine.** There is no login and no access control between people: anyone who can use your account can use your Thaler. The self-hosted server planned in `M-SERVER` will have accounts and private ledgers, and this page will say what it protects when it exists.
