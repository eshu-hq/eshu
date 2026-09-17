#!/usr/bin/env bash
#
# test-verify-filename-stutter.sh -- mirror test for verify-filename-stutter.sh.
#
# Reproduces the #6736 miss (a move landing `entity/entity_intents.go`) in a
# throwaway git repo, and pins the exemptions so the gate cannot be quietly
# narrowed into uselessness -- or widened into a legacy-tree blocker.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
gate="${script_dir}/verify-filename-stutter.sh"
failures=0

tmp_roots=()
trap 'rm -rf "${tmp_roots[@]}" 2>/dev/null || true' EXIT

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
  tmp_roots+=("$dir")
  git -C "$dir" init -q
  git -C "$dir" config user.email test@example.test
  git -C "$dir" config user.name "Test"
  git -C "$dir" commit -q --allow-empty -m init
  printf '%s\n' "$dir"
}

run_gate() {
  # $@ = args; ESHU_STUTTER_REPO_ROOT must be exported by the caller.
  local rc
  set +e
  bash "$gate" "$@" >/dev/null 2>&1
  rc=$?
  set -e
  printf '%s' "$rc"
}

# 1. RED: the #6736 shape -- a staged rename landing entity/entity_intents.go.
repo="$(new_repo)"
mkdir -p "$repo/go/internal/projector/semanticentity" "$repo/go/internal/projector/semantic"
printf 'package semanticentity\n' > "$repo/go/internal/projector/semanticentity/entity_intents.go"
export ESHU_STUTTER_REPO_ROOT="$repo"
git -C "$repo" add -A && git -C "$repo" commit -qm base
mkdir -p "$repo/go/internal/projector/semantic/entity"
git -C "$repo" mv go/internal/projector/semanticentity/entity_intents.go go/internal/projector/semantic/entity/entity_intents.go
rc="$(run_gate --staged)"
check "staged rename into stuttering nest is RED" 1 "$rc"

# 2. RED: a plain staged add of a stuttering new file.
repo2="$(new_repo)"
mkdir -p "$repo2/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo2/go/internal/projector/semantic/entity/entity_intents.go"
export ESHU_STUTTER_REPO_ROOT="$repo2"
git -C "$repo2" add -A
rc="$(run_gate --staged)"
check "staged add of stuttering file is RED" 1 "$rc"

# 3. GREEN: idiomatic package-root file catalog/catalog.go.
repo3="$(new_repo)"
mkdir -p "$repo3/go/internal/ask/catalog"
printf 'package catalog\n' > "$repo3/go/internal/ask/catalog/catalog.go"
printf 'package catalog\n' > "$repo3/go/internal/ask/catalog/catalog_test.go"
export ESHU_STUTTER_REPO_ROOT="$repo3"
git -C "$repo3" add -A
rc="$(run_gate --staged)"
check "package-root stem==leaf is GREEN" 0 "$rc"

# 4. GREEN: exact-word rule does not stem -- satisfied_by under satisfaction/.
repo4="$(new_repo)"
mkdir -p "$repo4/go/internal/projector/crossplane/satisfaction"
printf 'package satisfaction\n' > "$repo4/go/internal/projector/crossplane/satisfaction/satisfied_by_intents.go"
printf 'package satisfaction\n' > "$repo4/go/internal/projector/crossplane/satisfaction/doc.go"
printf 'not go\n' > "$repo4/go/internal/projector/crossplane/satisfaction/notes.txt"
export ESHU_STUTTER_REPO_ROOT="$repo4"
git -C "$repo4" add -A
rc="$(run_gate --staged)"
check "near-miss stem plus doc.go plus non-go are GREEN" 0 "$rc"

# 5. GREEN: clean staged set passes; --files mode flags the same RED shape.
repo5="$(new_repo)"
mkdir -p "$repo5/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo5/go/internal/projector/semantic/entity/intents.go"
export ESHU_STUTTER_REPO_ROOT="$repo5"
git -C "$repo5" add -A
rc="$(run_gate --staged)"
check "de-stuttered intents.go is GREEN" 0 "$rc"
rc="$(run_gate --files go/internal/projector/semantic/entity/intents.go)"
check "--files clean path is GREEN" 0 "$rc"

# 6. RED via --range: the stutter lands in a commit on top of base.
repo6="$(new_repo)"
mkdir -p "$repo6/go/internal/projector/semantic"
printf 'package semanticentity\n' > "$repo6/go/internal/projector/semanticentity.go"
export ESHU_STUTTER_REPO_ROOT="$repo6"
git -C "$repo6" add -A && git -C "$repo6" commit -qm base
mkdir -p "$repo6/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo6/go/internal/projector/semantic/entity/entity_intents.go"
git -C "$repo6" add -A && git -C "$repo6" commit -qm stutter
rc="$(run_gate --range HEAD~1)"
check "--range over stutter commit is RED" 1 "$rc"

# 7. GREEN via --range: clean commit on top of base.
repo7="$(new_repo)"
export ESHU_STUTTER_REPO_ROOT="$repo7"
git -C "$repo7" commit -q --allow-empty -m base
mkdir -p "$repo7/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo7/go/internal/projector/semantic/entity/intents.go"
git -C "$repo7" add -A && git -C "$repo7" commit -qm clean
rc="$(run_gate --range HEAD~1)"
check "--range over clean commit is GREEN" 0 "$rc"

# 8. RED: trailing segment repeats the leaf (entity/foo_entity.go). The
# segment splitter must see the final segment, not drop it.
repo8="$(new_repo)"
mkdir -p "$repo8/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo8/go/internal/projector/semantic/entity/foo_entity.go"
export ESHU_STUTTER_REPO_ROOT="$repo8"
git -C "$repo8" add -A
rc="$(run_gate --staged)"
check "staged add with trailing-segment stutter is RED" 1 "$rc"

# 9. RED via --files, and the diagnostic names the offending path.
repo9="$(new_repo)"
mkdir -p "$repo9/go/internal/correlation/rules"
printf 'package rules\n' > "$repo9/go/internal/correlation/rules/my_rules.go"
export ESHU_STUTTER_REPO_ROOT="$repo9"
set +e
out="$(bash "$gate" --files go/internal/correlation/rules/my_rules.go 2>&1)"
rc=$?
set -e
check "--files stutter path is RED" 1 "$rc"
case "$out" in
  *go/internal/correlation/rules/my_rules.go*) printf 'ok   --files diagnostic names the path\n' ;;
  *) printf 'FAIL --files diagnostic names the path: got %q\n' "$out" >&2; failures=$((failures + 1)) ;;
esac

# 10. Fail closed: unresolvable --range base exits 2, not 0.
repo10="$(new_repo)"
export ESHU_STUTTER_REPO_ROOT="$repo10"
rc="$(run_gate --range bogus-base-that-cannot-resolve)"
check "unresolvable --range base exits 2" 2 "$rc"

# 11. RED: disconnected history -- valid base commit, no merge base.
# The range diff producer fails; the gate must fail, not scan EOF.
repo11="$(new_repo)"
export ESHU_STUTTER_REPO_ROOT="$repo11"
git -C "$repo11" commit -q --allow-empty -m base
orphan="$(git -C "$repo11" rev-parse HEAD)"
git -C "$repo11" checkout -q --orphan stray
git -C "$repo11" rm -qf . 2>/dev/null || true
mkdir -p "$repo11/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo11/go/internal/projector/semantic/entity/entity_intents.go"
git -C "$repo11" add -A && git -C "$repo11" commit -qm stray
set +e
out="$(bash "$gate" --range "$orphan" 2>&1)"
rc=$?
set -e
check "disconnected-history range is RED" 1 "$rc"
case "$out" in
  *"no merge base"*) printf 'ok   disconnected history names the cause\n' ;;
  *) printf 'FAIL disconnected history names the cause: got %q\n' "$out" >&2; failures=$((failures + 1)) ;;
esac

# 12. Default mode: committed stutter RED, clean tree GREEN.
repo12="$(new_repo)"
export ESHU_STUTTER_REPO_ROOT="$repo12"
export ESHU_STUTTER_UPSTREAM="HEAD~1"
git -C "$repo12" commit -q --allow-empty -m base
mkdir -p "$repo12/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo12/go/internal/projector/semantic/entity/entity_intents.go"
git -C "$repo12" add -A && git -C "$repo12" commit -qm stutter
rc="$(run_gate)"
check "default mode over committed stutter is RED" 1 "$rc"
git -C "$repo12" mv go/internal/projector/semantic/entity/entity_intents.go go/internal/projector/semantic/entity/intents.go
git -C "$repo12" commit -qm destutter
rc="$(run_gate)"
check "default mode over clean tree is GREEN" 0 "$rc"
unset ESHU_STUTTER_UPSTREAM

if [ "$failures" != "0" ]; then
  printf 'test-verify-filename-stutter: %d case(s) failed\n' "$failures" >&2
  exit 1
fi
printf 'test-verify-filename-stutter: all cases pass\n'
