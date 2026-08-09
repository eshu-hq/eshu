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

if [ "$failures" -gt 0 ]; then
  printf '\ntest-verify-no-diff-fragments: %d case(s) failed\n' "$failures" >&2
  exit 1
fi
echo "test-verify-no-diff-fragments: all cases passed"
