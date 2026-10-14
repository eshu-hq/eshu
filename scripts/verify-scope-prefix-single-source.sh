#!/usr/bin/env bash
#
# verify-scope-prefix-single-source.sh — fail if a production Go file spells
# the git repository scope prefix literal instead of naming the constant.
#
# Why this exists: the #7879 refactor single-sourced the prefix — every
# production builder and reader names scope.GitRepositoryScopePrefix (aliased
# by recovery.GitRepositoryScopePrefix and
# querycontract.GitRepositoryScopePrefix), and exactly one production
# occurrence of the literal remains (the definition in
# go/internal/scope/scope.go). Nothing committed enforced that shape, so a
# future edit spelling the literal again compiled and passed every suite
# (issue #8023). This gate fails that edit.
#
# Allowlist (recorded here, where the guard lives):
#   - go/internal/scope/scope.go — the single definition.
#   - *_test.go fixtures — tests pin the wire value (e.g. the
#     TestBuildScopeIDUsesGitRepositoryScopePrefix pin) and must be free to
#     spell it.
# Scoped to *.go on purpose: docs, SQL evidence, and console fixtures spell
# the prefix in their own quoting and never the double-quoted Go literal, so
# they are out of scope. Single-quoted SQL LIKE spellings
# ('git-repository-scope:%') are likewise out of scope — they carry wildcards
# and are not the Go literal. A double-quoted occurrence in a production
# comment fails closed: quotes in a comment still invite copy-paste drift.
#
# The gate and its own test necessarily contain the literal they match.
#
# Known residual (accepted, tracked in #8112): split-concatenation such as
# "git-repository-" + "scope:" evades this grep guard by construction, as do
# suffixed placeholder spellings outside the bare literal. Broadening the
# pattern would false-positive on placeholders, so the tradeoff stays.
#
# Modes:
#   (default)      tree mode — scan every tracked Go file. Used by CI so a
#                  respelled literal cannot persist on main.
#   --staged       scan staged file content. Used by pre-commit, which is the
#                  point where the drift is introduced.
#
# Exit 0 when clean; 1 listing each offending file, line number and line.
set -euo pipefail

repo_root="${ESHU_SCOPE_PREFIX_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi
cd "$repo_root"

mode="tree"
case "${1:-}" in
  --staged) mode="staged" ;;
  "") ;;
  *) echo "usage: $(basename "$0") [--staged]" >&2; exit 2 ;;
esac

# The exact double-quoted Go literal, plus any raw-string (backtick) spelling —
# a backtick spelling is the only other way to write the literal in Go
# (single quotes hold one rune, so they cannot spell it). The backtick half
# stays open (no closing backtick required) so both `prefix:` + id and a
# fully hardcoded `prefix:id` fail. Extended-regex alternation: comments and
# docs that mention the prefix unquoted, single-quoted, or with a placeholder
# (git-repository-scope:<repo_id>) do not match.
pattern='"git-repository-scope:"|`git-repository-scope:'
# NOTE: single-quoted on purpose — double quotes would command-substitute
# the backtick half.

exclusions=(
  ':(exclude)*_test.go'
  ':(exclude)go/internal/scope/scope.go'
  ':(exclude)scripts/verify-scope-prefix-single-source.sh'
  ':(exclude)scripts/test-verify-scope-prefix-single-source.sh'
)

grep_args=(grep -n -E "$pattern" -- ':(glob)**/*.go')
if [ "$mode" = "staged" ]; then
  grep_args=(grep --cached -n -E "$pattern" -- ':(glob)**/*.go')
fi

# git grep exits 0 with matches, 1 with none, and something else when it could
# not do the search at all. Treating every non-zero status as "clean" would
# turn an operational failure into a silent pass, disabling the check exactly
# when something is already wrong.
grep_stderr="$(mktemp)"
trap 'rm -f "${grep_stderr}"' EXIT
set +e
matches="$(git "${grep_args[@]}" -- "${exclusions[@]}" 2>"${grep_stderr}")"
grep_status=$?
set -e
case "${grep_status}" in
  0) ;;
  1) matches="" ;;
  *)
    echo "verify-scope-prefix-single-source: git grep failed (exit ${grep_status}); the scan did not run" >&2
    cat "${grep_stderr}" >&2
    exit 2
    ;;
esac

violations=0
if [ -n "$matches" ]; then
  while IFS= read -r match; do
    [ -n "$match" ] || continue
    printf 'verify-scope-prefix-single-source: %s\n' "$match" >&2
    violations=$((violations + 1))
  done <<< "$matches"
fi

if [ "$violations" -gt 0 ]; then
  cat >&2 <<'EOF'

A production Go file spells the git repository scope prefix literal. Name
scope.GitRepositoryScopePrefix (or its recovery/querycontract alias) instead:
one edit must not be able to split how packages classify a repository scope
(#7879). Test fixtures (*_test.go) may spell the literal; production code
may not.
EOF
  exit 1
fi

echo "verify-scope-prefix-single-source: no production respellings of the git repository scope prefix"
