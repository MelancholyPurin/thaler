#!/usr/bin/env bash
# Tests verify-release.sh against throwaway release folders and a fake cosign.
#
# A wrong pass would let a release go out whose checksums nobody signed, or whose
# archives do not match them, so most cases are ways of being wrong. The fake cosign
# accepts only the exact call that checks for the Release workflow's identity, so the
# passing case also shows that the script asks for that identity.
#
# Usage: verify-release_test.sh        (from anywhere)
set -uo pipefail

script="$(cd "$(dirname "$0")" && pwd)/verify-release.sh"
failures=0

# A case is "name | then | want".
#   then   a step after a release that verifies: none, unsigned (cosign rejects the
#          signature), nobundle (no .sigstore.json), changed (an archive changes after
#          the checksums), extra (an archive the checksums do not name), missing (an
#          archive they name is gone), nochecksums (no checksums.txt)
cases=(
	"a signed release that matches | none | pass"
	"the signature does not verify | unsigned | fail"
	"there is no signature | nobundle | fail"
	"an archive changed after the checksums | changed | fail"
	"an archive the checksums do not name | extra | fail"
	"an archive the checksums name is missing | missing | fail"
	"there are no checksums | nochecksums | fail"
)

want_call='verify-blob --bundle checksums.txt.sigstore.json --certificate-identity-regexp ^https://github\.com/MelancholyPurin/thaler/\.github/workflows/release\.yml@refs/heads/(main|release/[0-9]+\.[0-9]+)$ --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt'

for c in "${cases[@]}"; do
	IFS='|' read -r name step want <<<"$c"
	name=$(echo "$name" | xargs)
	step=$(echo "$step" | xargs)
	want=$(echo "$want" | xargs)
	# given
	dir=$(mktemp -d)
	mkdir "$dir/dist"
	cat >"$dir/cosign" <<EOF
#!/usr/bin/env bash
[ "\$*" = '$want_call' ] || exit 1
[ ! -f "$dir/reject" ]
EOF
	chmod +x "$dir/cosign"
	(
		cd "$dir/dist" || exit 1
		echo linux >thaler_0.3.0_linux_amd64.tar.gz
		echo darwin >thaler_0.3.0_darwin_arm64.tar.gz
		if command -v sha256sum >/dev/null; then
			sha256sum ./*.tar.gz | sed 's| \./| |' >checksums.txt
		else
			shasum -a 256 ./*.tar.gz | sed 's| \./| |' >checksums.txt
		fi
		echo '{}' >checksums.txt.sigstore.json
		case "$step" in
		unsigned) touch "$dir/reject" ;;
		nobundle) rm checksums.txt.sigstore.json ;;
		changed) echo more >>thaler_0.3.0_linux_amd64.tar.gz ;;
		extra) echo extra >thaler_0.3.0_linux_arm64.tar.gz ;;
		missing) rm thaler_0.3.0_darwin_arm64.tar.gz ;;
		nochecksums) rm checksums.txt ;;
		esac
	)
	# when
	if COSIGN="$dir/cosign" "$script" "$dir/dist" >/dev/null 2>&1; then got=pass; else got=fail; fi
	# then
	if [ "$got" = "$want" ]; then
		echo "ok    $name"
	else
		echo "FAIL  $name: got $got, want $want"
		failures=$((failures + 1))
	fi
	rm -rf "$dir"
done

if [ "$failures" -ne 0 ]; then
	echo "$failures case(s) failed"
	exit 1
fi
echo "all cases passed"
