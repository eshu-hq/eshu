#!/usr/bin/env bash
# G8 Shape-3 reverse/restore PR flow stub (#9669, part of #6707).
#
# DESIGN ONLY: default run prints the planned commands and exits 0 without
# touching git remotes, PRs, or clusters. Live execution happens once, at the
# frozen removal base, with ESHU_G8_LIVE=1 and a named holder.
#
# Usage:
#   scripts/9669-g8-reverse-restore-flow.sh --plan
#   ESHU_G8_LIVE=1 scripts/9669-g8-reverse-restore-flow.sh --revert <removal-sha>
set -euo pipefail

mode="${1:---plan}"
sha="${2:-}"

plan() {
  cat <<'EOF'
plan: git fetch origin
plan: git checkout -b rehearsal/g8-revert-<removal-sha> <removal-base-sha>
plan: git revert --no-edit <removal-commit-sha>   # clean apply only; conflict voids the run
plan: gh pr create --title 'G8 rehearsal: revert <removal-commit-sha>' --body 'Refs #9669.'
plan: record reverse-PR number + head SHA in the rehearsal log
plan: rebuild + re-serve restored core artifact via the standard release path
plan: re-pin rehearsal target to last-good coordinates; capture digests
EOF
}

if [[ "${ESHU_G8_LIVE:-}" != "1" || "${mode}" == "--plan" ]]; then
  plan
  printf 'NOT-EXECUTED: set ESHU_G8_LIVE=1 with --revert <sha> on rehearsal day.\n'
  exit 0
fi

if [[ "${mode}" != "--revert" || -z "${sha}" ]]; then
  printf 'usage: ESHU_G8_LIVE=1 %s --revert <removal-commit-sha>\n' "$0" >&2
  exit 2
fi

git revert --no-edit "${sha}"
printf 'reverted %s; open the reverse PR through the normal review path.\n' "${sha}"
