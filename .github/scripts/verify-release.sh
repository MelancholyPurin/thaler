#!/usr/bin/env bash
# Checks a built release the way someone who downloads it would: checksums.txt is
# signed by the Release workflow of this repository, and every archive matches it.
#
# The signature is keyless (cosign and Sigstore, REL-11): the workflow proves who it is
# to Sigstore with GitHub's OIDC token, and the certificate in the bundle names the
# workflow and the branch it ran from. So a valid signature says that the checksums
# were written by .github/workflows/release.yml of MelancholyPurin/thaler, run from
# main or a release branch, and not merely that they match the release page.
#
# Usage: verify-release.sh DIR     (DIR holds checksums.txt, its .sigstore.json and the archives)
# COSIGN names the cosign binary (default: cosign on the PATH).
set -euo pipefail

dir="${1:?usage: verify-release.sh DIR}"
cosign="${COSIGN:-cosign}"

# Who may have signed: the Release workflow, run from main (a new minor version) or
# from release/<major>.<minor> (a fix), as the roadmap's release train says. The same
# line is in the README for people to copy.
identity='^https://github\.com/MelancholyPurin/thaler/\.github/workflows/release\.yml@refs/heads/(main|release/[0-9]+\.[0-9]+)$'
issuer='https://token.actions.githubusercontent.com'

cd "$dir"
for f in checksums.txt checksums.txt.sigstore.json; do
	if [ ! -f "$f" ]; then
		echo "FAIL  $dir has no $f"
		exit 1
	fi
done

if ! "$cosign" verify-blob \
	--bundle checksums.txt.sigstore.json \
	--certificate-identity-regexp "$identity" \
	--certificate-oidc-issuer "$issuer" \
	checksums.txt; then
	echo "FAIL  the signature of checksums.txt does not verify"
	exit 1
fi

if command -v sha256sum >/dev/null; then
	sum=(sha256sum)
else
	sum=(shasum -a 256) # macOS
fi
if ! "${sum[@]}" -c --quiet checksums.txt; then
	echo "FAIL  a file does not match checksums.txt"
	exit 1
fi

# An archive the checksums do not name would be published without anything vouching for it.
listed="$(awk '{ print $2 }' checksums.txt)"
for f in *.tar.gz; do
	[ -e "$f" ] || continue
	if ! grep -qxF "$f" <<<"$listed"; then
		echo "FAIL  $f is not in checksums.txt"
		exit 1
	fi
done
echo "ok    checksums.txt is signed by the Release workflow, and every archive matches it"
