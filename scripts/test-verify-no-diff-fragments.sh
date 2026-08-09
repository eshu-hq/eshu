#!/usr/bin/env bash
#
# test-verify-no-diff-fragments.sh — mirror test for verify-no-diff-fragments.sh.
#
# Reproduces the #5612 corruption shape (a unified-diff blob written INTO a Go
# source file) in a throwaway git repo, and pins the exemptions so the gate
# cannot be quietly narrowed into uselessness.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
gate="${script_dir}/verify-no-diff-fragments.sh"
failures=0

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
  mkdir -p "$dir/scripts"
  cp "$gate" "$dir/scripts/verify-no-diff-fragments.sh"
  printf '%s\n' "$dir"
}

# 1. The real #5612 shape: a diff blob inside a Go file's godoc.
repo="$(new_repo)"
cat > "$repo/corrupt.go" <<'EOF'
package main

// This test asserts a warn-level log
diff --git a/x_test.go b/x_test.go
--- a/x_test.go
+++ b/x_test.go
@@ -24 +24 @@
-// debug
+// warn
func main() {}
EOF
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh >/dev/null 2>&1)
rc=$?
set -e
check "rejects a diff blob in a Go file" 1 "$rc"
rm -rf "$repo"

# 2. Clean source passes.
repo="$(new_repo)"
printf 'package main\n\nfunc main() {}\n' > "$repo/clean.go"
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh >/dev/null 2>&1)
rc=$?
set -e
check "accepts clean source" 0 "$rc"
rm -rf "$repo"

# 3. Conflict markers are rejected too — same class of corruption.
repo="$(new_repo)"
printf 'package main\n\n<<<<<<< HEAD\nfunc a() {}\n=======\nfunc b() {}\n>>>>>>> other\n' > "$repo/conflict.go"
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh >/dev/null 2>&1)
rc=$?
set -e
check "rejects conflict markers" 1 "$rc"
rm -rf "$repo"

# 4. Markdown may legitimately SHOW a diff. If this ever starts failing, the
#    gate has become unusable for docs and someone will disable it entirely.
repo="$(new_repo)"
cat > "$repo/doc.md" <<'EOF'
Example patch:

```
diff --git a/a.go b/a.go
@@ -1 +1 @@
-old
+new
```
EOF
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh >/dev/null 2>&1)
rc=$?
set -e
check "exempts markdown showing a diff" 0 "$rc"
rm -rf "$repo"

# 5. A .patch file IS a diff.
repo="$(new_repo)"
printf 'diff --git a/a b/a\n@@ -1 +1 @@\n-x\n+y\n' > "$repo/fix.patch"
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh >/dev/null 2>&1)
rc=$?
set -e
check "exempts .patch files" 0 "$rc"
rm -rf "$repo"

# 6. --staged reads the STAGED blob, not the worktree. This is the case that
#    matters at commit time: the corruption is in what is about to be committed.
repo="$(new_repo)"
printf 'package main\n\ndiff --git a/x b/x\nfunc main() {}\n' > "$repo/staged.go"
git -C "$repo" add -A
# Clean the worktree copy AFTER staging: a gate reading the file on disk would
# now pass and let the corrupt blob through.
printf 'package main\n\nfunc main() {}\n' > "$repo/staged.go"
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh --staged >/dev/null 2>&1)
rc=$?
set -e
check "--staged reads the staged blob, not the worktree" 1 "$rc"
rm -rf "$repo"

# 7. A Go left-shift line must not read as a conflict marker.
repo="$(new_repo)"
printf 'package main\n\nconst x = 1\n\nfunc main() { _ = x << 2 }\n' > "$repo/shift.go"
git -C "$repo" add -A
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh >/dev/null 2>&1)
rc=$?
set -e
check "does not flag a left-shift expression" 0 "$rc"
rm -rf "$repo"

# 8. A REAL merge conflict under a widened conflict-marker-size. Git's
#    per-path attribute makes markers longer than seven, and a gate matching
#    exactly seven let a genuinely conflicted file through both the hook and CI
#    (#6005 review, reproduced by codex).
repo="$(new_repo)"
printf '*.go conflict-marker-size=10\n' > "$repo/.gitattributes"
printf 'package main\n\nfunc v() int { return 1 }\n' > "$repo/m.go"
git -C "$repo" add -A
git -C "$repo" commit -qm base
git -C "$repo" checkout -q -b side
printf 'package main\n\nfunc v() int { return 2 }\n' > "$repo/m.go"
git -C "$repo" commit -qam side
git -C "$repo" checkout -q -
printf 'package main\n\nfunc v() int { return 3 }\n' > "$repo/m.go"
git -C "$repo" commit -qam main
git -C "$repo" merge side >/dev/null 2>&1 || true
grep -q '<<<<<<<<<<' "$repo/m.go" || printf 'SKIP: merge produced no widened markers\n' >&2
git -C "$repo" add m.go 2>/dev/null || true
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh --staged >/dev/null 2>&1)
rc=$?
set -e
check "rejects widened conflict markers from a real merge" 1 "$rc"
rm -rf "$repo"

# 9. An operational git grep failure must not read as "clean". Treating every
#    non-zero status as no-matches disabled the check exactly when something was
#    already wrong (#6005 review, reproduced with a corrupt index).
repo="$(new_repo)"
printf 'package main\n' > "$repo/ok.go"
git -C "$repo" add -A
printf 'not an index' > "$repo/.git/index"
set +e
(cd "$repo" && ./scripts/verify-no-diff-fragments.sh --staged >/dev/null 2>&1)
rc=$?
set -e
if [ "$rc" = "0" ]; then
  printf 'FAIL %s: exit=0, want non-zero\n' "a broken index must not report clean" >&2
  failures=$((failures + 1))
else
  printf 'ok   %s\n' "a broken index must not report clean"
fi
rm -rf "$repo"

if [ "$failures" -gt 0 ]; then
  printf '\ntest-verify-no-diff-fragments: %d case(s) failed\n' "$failures" >&2
  exit 1
fi
echo "test-verify-no-diff-fragments: all cases passed"
