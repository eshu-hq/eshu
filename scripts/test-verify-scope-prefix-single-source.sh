#!/usr/bin/env bash
#
# test-verify-scope-prefix-single-source.sh — mirror test for
# verify-scope-prefix-single-source.sh.
#
# Reproduces the #8023 drift shape (the scope prefix literal respelled in a
# production Go file) in a throwaway git repo, and pins the allowlist — the
# go/internal/scope/scope.go definition plus *_test.go fixtures — so the gate
# cannot be quietly narrowed into uselessness or widened into noise.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
gate="${script_dir}/verify-scope-prefix-single-source.sh"
failures=0

# #6845: isolate scratch repos from the operator's git config. GLOBAL=/dev/null
# is the working isolation; GIT_CONFIG_NOGLOBAL is not a real git knob.
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_NOSYSTEM=1
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking
# (same isolation family as the config knobs above; cf.
# test-verify-moved-file-refs.sh).
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0

cleanup_repo() {
  local dir="$1" i
  git -C "$dir" maintenance stop >/dev/null 2>&1 || true
  for i in 1 2 3 4 5; do
    if rm -rf "$dir"; then
      return 0
    fi
    sleep 0.2
  done
  rm -rf "$dir"
}

check() {
  # $1 = case name, $2 = expected exit (0|1), $3 = actual exit
  if [ "$2" != "$3" ]; then
    printf 'FAIL %s: exit=%s want=%s\n' "$1" "$3" "$2" >&2
    failures=$((failures + 1))
  else
    printf 'ok   %s\n' "$1"
  fi
}

new_repo() {
  local dir
  dir="$(mktemp -d)"
  git -C "$dir" init -q
  git -C "$dir" config user.email test@example.test
  git -C "$dir" config user.name "Test"
  git -C "$dir" config maintenance.auto false
  git -C "$dir" config gc.auto 0
  mkdir -p "$dir/scripts"
  cp "$gate" "$dir/scripts/verify-scope-prefix-single-source.sh"
  printf '%s\n' "$dir"
}

# 1. The #8023 shape: a production file respelling the literal.
repo="$(new_repo)"
cat > "$repo/drift.go" <<'EOF'
package main

const driftScope = "git-repository-scope:" + "acme/app"
EOF
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh >/dev/null 2>&1)
rc=$?
set -e
check "rejects a respelled literal in a production file" 1 "$rc"
cleanup_repo "$repo"

# 1b. The raw-string twin is not an escape hatch.
repo="$(new_repo)"
cat > "$repo/raw.go" <<'EOF'
package main

const rawScope = `git-repository-scope:acme/app`
EOF
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh >/dev/null 2>&1)
rc=$?
set -e
check "rejects a backtick respelling in a production file" 1 "$rc"
cleanup_repo "$repo"

# 2. Clean production source passes.
repo="$(new_repo)"
printf 'package main\n\nconst driftScope = "other-prefix:" + "acme/app"\n' > "$repo/clean.go"
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh >/dev/null 2>&1)
rc=$?
set -e
check "accepts clean production source" 0 "$rc"
cleanup_repo "$repo"

# 3. Test fixtures may spell the literal (the wire-value pin).
repo="$(new_repo)"
cat > "$repo/pin_test.go" <<'EOF'
package main

import "testing"

func TestPin(t *testing.T) {
	want := "git-repository-scope:"
	if want == "" {
		t.Fatal("unreachable")
	}
}
EOF
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh >/dev/null 2>&1)
rc=$?
set -e
check "exempts _test.go fixtures" 0 "$rc"
cleanup_repo "$repo"

# 4. The single definition may spell the literal.
repo="$(new_repo)"
mkdir -p "$repo/go/internal/scope"
cat > "$repo/go/internal/scope/scope.go" <<'EOF'
package scope

const GitRepositoryScopePrefix = "git-repository-scope:"
EOF
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh >/dev/null 2>&1)
rc=$?
set -e
check "exempts the scope.go definition" 0 "$rc"
cleanup_repo "$repo"

# 5. A double-quoted occurrence in a production comment fails closed: quotes
#    in a comment still invite copy-paste drift.
repo="$(new_repo)"
cat > "$repo/commented.go" <<'EOF'
package main

// Do not respell "git-repository-scope:" here; name the constant.
const commentedScope = "other:"
EOF
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh >/dev/null 2>&1)
rc=$?
set -e
check "fails closed on a quoted literal in a production comment" 1 "$rc"
cleanup_repo "$repo"

# 6. Docs are out of scope: the guard scans *.go only.
repo="$(new_repo)"
cat > "$repo/note.md" <<'EOF'
The definition spells "git-repository-scope:" exactly once.
EOF
printf 'package main\n' > "$repo/ok.go"
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh >/dev/null 2>&1)
rc=$?
set -e
check "ignores docs outside *.go" 0 "$rc"
cleanup_repo "$repo"

# 7. --staged reads the STAGED blob, not the worktree.
repo="$(new_repo)"
printf 'package main\n\nconst stagedScope = "git-repository-scope:" + "x"\n' > "$repo/staged.go"
git -C "$repo" add -A
printf 'package main\n\nconst stagedScope = "other:" + "x"\n' > "$repo/staged.go"
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh --staged >/dev/null 2>&1)
rc=$?
set -e
check "--staged reads the staged blob, not the worktree" 1 "$rc"
cleanup_repo "$repo"

# 8. An operational git grep failure must not read as "clean".
repo="$(new_repo)"
printf 'package main\n' > "$repo/ok.go"
git -C "$repo" add -A
printf 'not an index' > "$repo/.git/index"
set +e
(cd "$repo" && ./scripts/verify-scope-prefix-single-source.sh --staged >/dev/null 2>&1)
rc=$?
set -e
if [ "$rc" = "0" ]; then
  printf 'FAIL %s: exit=0, want non-zero\n' "a broken index must not report clean" >&2
  failures=$((failures + 1))
else
  printf 'ok   %s\n' "a broken index must not report clean"
fi
cleanup_repo "$repo"

if [ "$failures" -gt 0 ]; then
  printf '\ntest-verify-scope-prefix-single-source: %d case(s) failed\n' "$failures" >&2
  exit 1
fi
echo "test-verify-scope-prefix-single-source: all cases passed"
