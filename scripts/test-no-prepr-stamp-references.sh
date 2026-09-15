#!/usr/bin/env bash
# Guard: the per-SHA `make pre-pr` push stamp is removed (replaced by
# scripts/dev/pre-push.sh, the fast local floor run before every push). This
# pins the removal so it cannot silently regress:
#
#   1. .pre-commit-config.yaml carries no pre-push stamp hook.
#   2. scripts/dev/prepr-stamp-verify.sh and its self-test no longer exist.
#   3. No git-tracked file (this guard's own source excepted, since it must
#      name the needles to check for them) mentions the removed hook's id
#      (prepr-stamp-verify) or its bypass environment variable
#      (ESHU_ALLOW_UNSTAMPED_PUSH). The stamp directory name itself
#      (eshu-prepr-stamp) is intentionally NOT a needle here: a regression
#      test elsewhere asserts that directory is never recreated, which
#      requires naming it.
#
#   .agents/, .claude/skills/, and .codex/skills/ are excluded FOR NOW: a
#   separate, concurrent effort is rewording every skill under .agents/skills/
#   (the source of truth the other two symlink to) to drop stamp/mandatory-
#   make-pre-pr language, and this guard must not fight that in-flight work or
#   fail on files this branch does not own. Tighten this back to the full tree
#   once that effort lands.
#
# CI remains the blocking authority for Ifá/Odù contracts, performance, and
# end-to-end proof via the required-gates-complete aggregate — see
# docs/public/reference/local-testing.md. This guard is about the LOCAL
# push-time stamp only, not about weakening that CI floor.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
self="scripts/test-no-prepr-stamp-references.sh"

fail() { printf 'test-no-prepr-stamp-references: %s\n' "$*" >&2; exit 1; }

if rg -q 'id: prepr-stamp-verify$|entry: scripts/dev/prepr-stamp-verify\.sh' \
	"${repo_root}/.pre-commit-config.yaml"; then
	fail ".pre-commit-config.yaml still carries the prepr-stamp-verify pre-push hook"
fi

for removed in scripts/dev/prepr-stamp-verify.sh scripts/dev/test-prepr-stamp-verify.sh; do
	[[ ! -e "${repo_root}/${removed}" ]] || fail "${removed} must be deleted, not left behind"
done

status=0
# One rg pass over every tracked file (rg's own startup cost dominates a
# per-file loop across a repo this size), excluding this guard's own source
# (it must name the needles literally to check for them). Piped through a
# process substitution, never a `<<<` here-string: Homebrew bash 5.3.15 on
# macOS deadlocks feeding a while-read loop past roughly a 1KB here-string.
while IFS= read -r f; do
	[[ -z "${f}" || "${f}" == "${self}" ]] && continue
	printf 'test-no-prepr-stamp-references: %s still references the removed stamp mechanism\n' "${f}" >&2
	status=1
done < <(cd "${repo_root}" && git ls-files -z -- . ':!.agents' ':!.claude/skills' ':!.codex/skills' \
	| xargs -0 rg -l -e 'prepr-stamp-verify' -e 'ESHU_ALLOW_UNSTAMPED_PUSH' 2>/dev/null || true)

[[ "${status}" -eq 0 ]] || fail "one or more tracked files still reference the removed stamp mechanism (see above)"

printf 'test-no-prepr-stamp-references: pass\n'
