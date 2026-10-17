#!/usr/bin/env bash
# Test mirror for scripts/verify-merge-group-slim-vet.sh (#7859 P3).
#
# RED/GREEN through the real scripts on real trees:
#   - #7847 class: the scoped vet fails on 24792a77b (a test-only
#     `undefined: catalogTestScope` break a whole-module build cannot see)
#     and passes on its first parent. A whole-module build passes on the
#     break tree, proving build alone would not catch it.
#   - #8219 class: test-context-stories-doc-split.sh fails on a4b0ef7b4
#     (docs/public/reference/http-api.md at 463 lines against the 450 limit)
#     and passes on its first parent.
#
# The RED/GREEN pairs need the break commits in the local object database and
# take minutes (each vet run compiles). They fail closed with a clear message
# when the history is absent: a fetch-depth-2 lane checkout cannot provide it,
# so this mirror is local-proof tooling, not a lane step.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_root}/.." && pwd)"
vet_script="${script_root}/verify-merge-group-slim-vet.sh"

red_7847="24792a77b"
red_8219="a4b0ef7b4"
parent_8219="9a976591447061f0e65aac75c9e4922837e6eddd"

failures=0
check() {
	if [[ "$1" != "$2" ]]; then
		printf 'FAIL: %s (got %s, want %s)\n' "$3" "$1" "$2" >&2
		failures=$((failures + 1))
	else
		printf 'ok: %s\n' "$3"
	fi
}

[[ -f "${vet_script}" ]] || {
	printf 'FAIL: %s missing\n' "${vet_script}" >&2
	exit 1
}

# Unit: unknown flag exits 2 without touching git or go.
if "${vet_script}" --bogus >/dev/null 2>&1; then
	check "0" "2" "unknown flag exits non-zero"
else
	check "$?" "2" "unknown flag exits 2"
fi

# Unit: unresolvable base fails closed (exit 1), never passes blind.
if "${vet_script}" --base deadbeefdeadbeefdeadbeefdeadbeefdeadbeef --head HEAD >/dev/null 2>&1; then
	check "0" "1" "unresolvable base fails closed"
else
	check "$?" "1" "unresolvable base exits 1"
fi

require_sha() {
	git -C "${repo_root}" cat-file -e "$1" 2>/dev/null || {
		printf 'FAIL: fixture commit %s absent from local history; fetch it first\n' "$1" >&2
		exit 1
	}
}

require_sha "${red_7847}"
require_sha "${red_8219}"
require_sha "${parent_8219}"

worktree_list="$(mktemp)"
cleanup() {
	while IFS= read -r wt; do
		[[ -n "${wt}" ]] || continue
		git -C "${repo_root}" worktree remove --force "${wt}" >/dev/null 2>&1 || true
	done <"${worktree_list}"
	rm -f "${worktree_list}"
}
trap cleanup EXIT

add_worktree() {
	wt="$(mktemp -d)"
	rmdir "${wt}"
	git -C "${repo_root}" worktree add --detach "${wt}" "$1" >/dev/null 2>&1
	printf '%s\n' "${wt}" >>"${worktree_list}"
	printf '%s' "${wt}"
}

# #7847 RED: scoped vet fails on the break tree naming catalogTestScope.
wt_red="$(add_worktree "${red_7847}")"
cp "${vet_script}" "${wt_red}/scripts/"
if (cd "${wt_red}" && bash scripts/verify-merge-group-slim-vet.sh >/tmp/slim-vet-red.log 2>&1); then
	check "0" "1" "#7847 break tree fails scoped vet"
else
	check "$?" "1" "#7847 break tree fails scoped vet"
fi
if grep -q "catalogTestScope" /tmp/slim-vet-red.log; then
	printf 'ok: #7847 vet failure names catalogTestScope\n'
else
	printf 'FAIL: #7847 vet failure does not name catalogTestScope\n' >&2
	failures=$((failures + 1))
fi

# Corroboration: the whole-module build passes on that same tree, so build
# alone would not catch this class.
if (cd "${wt_red}/go" && go build ./... >/tmp/slim-build-red.log 2>&1); then
	check "0" "0" "whole-module build passes on #7847 break tree"
else
	check "$?" "0" "whole-module build passes on #7847 break tree"
fi

# #7847 GREEN: scoped vet passes on the break tree's first parent.
wt_green="$(add_worktree "${red_7847}^1")"
cp "${vet_script}" "${wt_green}/scripts/"
if (cd "${wt_green}" && bash scripts/verify-merge-group-slim-vet.sh >/tmp/slim-vet-green.log 2>&1); then
	check "0" "0" "#7847 parent passes scoped vet"
else
	check "$?" "0" "#7847 parent passes scoped vet"
fi

# #8219 RED/GREEN through the lane's doc-split step as shipped in each tree.
wt_doc_red="$(add_worktree "${red_8219}")"
if bash "${wt_doc_red}/scripts/test-context-stories-doc-split.sh" >/tmp/slim-docsplit-red.log 2>&1; then
	check "0" "1" "#8219 tree fails doc-split"
else
	check "$?" "1" "#8219 tree fails doc-split"
fi
if grep -q "has 463 lines; limit is 450" /tmp/slim-docsplit-red.log; then
	printf 'ok: #8219 doc-split failure names the 463-line breach\n'
else
	printf 'FAIL: #8219 doc-split failure does not name the 463-line breach\n' >&2
	failures=$((failures + 1))
fi

wt_doc_green="$(add_worktree "${parent_8219}")"
if bash "${wt_doc_green}/scripts/test-context-stories-doc-split.sh" >/tmp/slim-docsplit-green.log 2>&1; then
	check "0" "0" "#8219 parent passes doc-split"
else
	check "$?" "0" "#8219 parent passes doc-split"
fi

if [[ "${failures}" -ne 0 ]]; then
	printf '%d failure(s)\n' "${failures}" >&2
	exit 1
fi
printf 'test-verify-merge-group-slim-vet: pass\n'
