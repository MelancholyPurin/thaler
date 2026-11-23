#!/usr/bin/env bash
# Sets the repository's merge settings and the protection of main, as ENG-7 decided.
# The maintainer runs it by hand, with gh signed in as an admin of the repository; it
# is here so that the settings can be read, reviewed and set again, not clicked.
#
#   - Pull requests are merged by squashing only, the commit titled with the pull
#     request's title (the release notes are written from those titles), and the
#     branch is deleted after the merge.
#   - main takes changes only through a pull request whose check "Report Result" (the
#     gate of ci.yml) passed, for admins too; no force push, no deleting it. No review
#     is required: there is one maintainer, who cannot approve their own pull request.
#   - Vulnerabilities can be reported privately (the Security tab), as SECURITY.md says.
#
# Usage: protect-main.sh [OWNER/REPO]    (default: the repository of this clone)
# Running it again sets the same settings again.
set -euo pipefail

repo="${1:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}"

gh api --method PATCH "repos/$repo" \
	-F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false \
	-f squash_merge_commit_title=PR_TITLE -f squash_merge_commit_message=PR_BODY \
	-F delete_branch_on_merge=true >/dev/null

gh api --method PUT "repos/$repo/branches/main/protection" --input - >/dev/null <<'JSON'
{
  "required_status_checks": {"strict": false, "contexts": ["Report Result"]},
  "enforce_admins": true,
  "required_pull_request_reviews": {"required_approving_review_count": 0},
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false
}
JSON

gh api --method PUT "repos/$repo/private-vulnerability-reporting" >/dev/null

echo "$repo: private vulnerability reporting on; squash merge only, branches deleted after merge; main needs a pull request with Report Result passed"
