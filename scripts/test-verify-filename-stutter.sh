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

# new_repo runs inside $(...), so it cannot grow a shell array in this shell.
# Every temp repo is recorded in a file instead, and the EXIT trap removes
# them all; the array form leaked every repo and tripped set -u under
# bash 3.2 at exit.
tmp_list="$(mktemp)"
cleanup() {
  local d
  while IFS= read -r d; do
    [ -n "$d" ] && rm -rf "$d"
  done <"$tmp_list"
  rm -f "$tmp_list"
}
trap cleanup EXIT

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
  printf '%s\n' "$dir" >>"$tmp_list"
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

# 1. RED: the stutter shape -- a staged rename landing entity/entity_checks.go
# (same class as the #6736 entity/entity_intents.go miss).
repo="$(new_repo)"
mkdir -p "$repo/go/internal/projector/semanticentity" "$repo/go/internal/projector/semantic"
printf 'package semanticentity\n' > "$repo/go/internal/projector/semanticentity/entity_checks.go"
export ESHU_STUTTER_REPO_ROOT="$repo"
git -C "$repo" add -A && git -C "$repo" commit -qm base
mkdir -p "$repo/go/internal/projector/semantic/entity"
git -C "$repo" mv go/internal/projector/semanticentity/entity_checks.go go/internal/projector/semantic/entity/entity_checks.go
rc="$(run_gate --staged)"
check "staged rename into stuttering nest is RED" 1 "$rc"

# 2. RED: a plain staged add of a stuttering new file.
repo2="$(new_repo)"
mkdir -p "$repo2/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo2/go/internal/projector/semantic/entity/entity_checks.go"
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
printf 'package entity\n' > "$repo6/go/internal/projector/semantic/entity/entity_checks.go"
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
printf 'package entity\n' > "$repo11/go/internal/projector/semantic/entity/entity_checks.go"
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

# 12b. Bare positional args honor files mode (no silent range scan).
repo12b="$(new_repo)"
mkdir -p "$repo12b/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo12b/go/internal/projector/semantic/entity/entity_checks.go"
export ESHU_STUTTER_REPO_ROOT="$repo12b"
set +e
out="$(bash "$gate" go/internal/projector/semantic/entity/entity_checks.go 2>&1)"
rc=$?
set -e
check "bare positional arg scans the named file RED" 1 "$rc"
case "$out" in
  *go/internal/projector/semantic/entity/entity_checks.go*) printf 'ok   bare-arg diagnostic names the path\n' ;;
  *) printf 'FAIL bare-arg diagnostic names the path: got %q\n' "$out" >&2; failures=$((failures + 1)) ;;
esac

# 12. Default mode: committed stutter RED, clean tree GREEN.
repo12="$(new_repo)"
export ESHU_STUTTER_REPO_ROOT="$repo12"
export ESHU_STUTTER_UPSTREAM="HEAD~1"
git -C "$repo12" commit -q --allow-empty -m base
mkdir -p "$repo12/go/internal/projector/semantic/entity"
printf 'package entity\n' > "$repo12/go/internal/projector/semantic/entity/entity_checks.go"
git -C "$repo12" add -A && git -C "$repo12" commit -qm stutter
rc="$(run_gate)"
check "default mode over committed stutter is RED" 1 "$rc"
git -C "$repo12" mv go/internal/projector/semantic/entity/entity_checks.go go/internal/projector/semantic/entity/intents.go
git -C "$repo12" commit -qm destutter
rc="$(run_gate)"
check "default mode over clean tree is GREEN" 0 "$rc"
unset ESHU_STUTTER_UPSTREAM

# ---------------------------------------------------------------------------
# Issue #6821: directory stutter (naming rule 3) and rule 2 for every file type.
# ---------------------------------------------------------------------------

# stage_case <name> <expected-exit> <path>... : fresh repo, stage the paths as
# new files (parent dirs created on demand), run --staged, check the exit.
stage_case() {
  local name="$1" want="$2" r p rc
  shift 2
  r="$(new_repo)"
  for p in "$@"; do
    mkdir -p "$r/$(dirname "$p")"
    printf 'x\n' > "$r/$p"
  done
  export ESHU_STUTTER_REPO_ROOT="$r"
  git -C "$r" add -A
  rc="$(run_gate --staged)"
  check "$name" "$want" "$rc"
}

# 13. RED: a staged new directory that repeats its parent's name (rule 3).
stage_case "staged new dir query/queryauth is RED" 1 go/internal/query/queryauth/handler.go
stage_case "staged new dir with suffix stutter auth-query is RED" 1 go/internal/query/authquery/handler.go
stage_case "staged new dir query/query-auth (hyphen) is RED" 1 docs/internal/query/query-auth/notes.md
stage_case "staged new dir query/query_auth (underscore) is RED" 1 docs/internal/query/query_auth/notes.md
stage_case "staged new dir query/auth_query (underscore suffix) is RED" 1 docs/internal/query/auth_query/notes.md
stage_case "dir stutter is case-insensitive" 1 go/internal/Query/QueryAuth/handler.go
stage_case "dir equal to its parent (query/query) is RED" 1 go/internal/query/query/handler.go
stage_case "stuttering dir deep in a new tree is RED" 1 go/internal/reducer/deep/security/securityalert/handler.go

# 14. GREEN: clean nested directories, and near-miss names that are not
# a prefix or suffix of the parent.
stage_case "clean nested dir query/auth is GREEN" 0 go/internal/query/auth/handler.go
stage_case "dir sharing only the middle of the parent is GREEN" 0 go/internal/query/subqueryish/handler.go
stage_case "top-level dir has no parent to stutter against" 0 queryauth/handler.go

# 15. GREEN: exempt parents and fixture trees never trip the directory check.
stage_case "parent docs is exempt" 0 docs/docsite/index.md
stage_case "parent internal is exempt" 0 go/internal/internalapi/handler.go
stage_case "parent cmd is exempt" 0 go/cmd/cmdrunner/main.go
stage_case "parent scripts is exempt" 0 scripts/scriptsupport/run.sh
stage_case "parent specs is exempt" 0 specs/specsheet/a.yaml
stage_case "parent testdata is exempt" 0 go/internal/parser/testdata/testdatafoo/a.txt
stage_case "parent tests is exempt" 0 go/tests/testsuite/a.go
stage_case "fixture tree under testdata is exempt" 0 go/internal/parser/testdata/fixtures/fixtures-x/sample/sample-a.txt

# Tool-mandated dot-directories (fixtures expect codex/.codex, aider/.aider)
# are named by external tools, not by us.
stage_case "dot-directory named for its parent is exempt" 0 docs/internal/codex/.codex/config.toml
stage_case "non-dot directory with the same name is still RED" 1 docs/internal/codex/codex/config.toml

# 16. RED: rule 2 for non-Go files.
stage_case "stuttering markdown file is RED" 1 docs/internal/evidence/6821-evidence.md
stage_case "stuttering yaml file is RED" 1 specs/plans/plans_v1.yaml
stage_case "stuttering shell script is RED" 1 scripts/verify/verify-thing.sh
stage_case "stuttering sql file is RED" 1 go/internal/storage/postgres/schema/schema_v2.sql
stage_case "non-Go stuttering file with hyphen segment is RED" 1 docs/internal/design/design-notes.md

# 17. GREEN: non-Go files that obey rule 2 and the idiomatic exemptions.
stage_case "non-Go near-miss stem is GREEN" 0 docs/internal/evidence/6821-notes.md
stage_case "non-Go stem equal to leaf dir is GREEN" 0 go/internal/query/openapi/openapi.yaml
stage_case "README.md in a readme dir is exempt" 0 docs/internal/readme/README.md
stage_case "AGENTS.md in an agents dir is exempt" 0 docs/internal/agents/AGENTS.md
stage_case "CLAUDE.md in a claude dir is exempt" 0 docs/internal/claude/CLAUDE.md
stage_case "doc.go in a doc dir is exempt" 0 go/internal/doc/doc.go
stage_case "extensionless file without a stutter is GREEN" 0 scripts/lib/Makefile
stage_case "dotfile without a stutter is GREEN" 0 scripts/lib/.gitkeep
stage_case "multi-dot non-Go near-miss is GREEN" 0 go/internal/query/schema/values.schema.json
stage_case "fixture file under testdata is exempt from rule 2" 0 go/internal/parser/testdata/sample/sample-sample.txt

# 18. RED/GREEN: a renamed path landing in a stuttering directory.
repo18="$(new_repo)"
mkdir -p "$repo18/go/internal/query/auth"
printf 'package auth\n' > "$repo18/go/internal/query/auth/handler.go"
export ESHU_STUTTER_REPO_ROOT="$repo18"
git -C "$repo18" add -A && git -C "$repo18" commit -qm base
mkdir -p "$repo18/go/internal/query/queryauth"
git -C "$repo18" mv go/internal/query/auth/handler.go go/internal/query/queryauth/handler.go
rc="$(run_gate --staged)"
check "staged rename landing in new stuttering dir is RED" 1 "$rc"
git -C "$repo18" commit -qm rename
rc="$(run_gate --range HEAD~1)"
check "--range rename landing in new stuttering dir is RED" 1 "$rc"

# 19. GREEN: legacy stuttering directory left alone. A rename or add inside a
# directory that already exists at the base must not re-flag the directory.
repo19="$(new_repo)"
mkdir -p "$repo19/go/internal/query/queryauth"
printf 'package queryauth\n' > "$repo19/go/internal/query/queryauth/handler.go"
printf 'package queryauth\n' > "$repo19/go/internal/query/queryauth/old_name.go"
export ESHU_STUTTER_REPO_ROOT="$repo19"
git -C "$repo19" add -A && git -C "$repo19" commit -qm base
printf 'package queryauth\n// touched\n' > "$repo19/go/internal/query/queryauth/handler.go"
git -C "$repo19" add -A
rc="$(run_gate --staged)"
check "modified file in legacy stuttering dir is GREEN" 0 "$rc"
printf 'package queryauth\n' > "$repo19/go/internal/query/queryauth/extra.go"
git -C "$repo19" mv go/internal/query/queryauth/old_name.go go/internal/query/queryauth/new_name.go
git -C "$repo19" add -A
rc="$(run_gate --staged)"
check "new and renamed files in legacy stuttering dir are GREEN" 0 "$rc"
git -C "$repo19" commit -qm touch
rc="$(run_gate --range HEAD~1)"
check "--range over legacy stuttering dir is GREEN" 0 "$rc"
export ESHU_STUTTER_UPSTREAM="HEAD~1"
rc="$(run_gate)"
check "default mode over legacy stuttering dir is GREEN" 0 "$rc"
unset ESHU_STUTTER_UPSTREAM

# 20. Legacy file stutter in a legacy dir stays GREEN when untouched, while a
# new stuttering directory added next to it is still caught.
repo20="$(new_repo)"
mkdir -p "$repo20/go/internal/query/queryauth"
printf 'x\n' > "$repo20/go/internal/query/queryauth/queryauth_legacy.md"
export ESHU_STUTTER_REPO_ROOT="$repo20"
git -C "$repo20" add -A && git -C "$repo20" commit -qm base
mkdir -p "$repo20/go/internal/query/querycontract"
printf 'x\n' > "$repo20/go/internal/query/querycontract/a.md"
git -C "$repo20" add -A
rc="$(run_gate --staged)"
check "new stuttering dir beside a legacy one is RED" 1 "$rc"

# 21. Diagnostics name the offending directory and its parent.
repo21="$(new_repo)"
mkdir -p "$repo21/go/internal/query/queryauth"
printf 'x\n' > "$repo21/go/internal/query/queryauth/a.md"
export ESHU_STUTTER_REPO_ROOT="$repo21"
git -C "$repo21" add -A
set +e
out="$(bash "$gate" --staged 2>&1)"
rc=$?
set -e
check "dir stutter diagnostic run is RED" 1 "$rc"
case "$out" in
  *go/internal/query/queryauth*query*) printf 'ok   dir diagnostic names the directory and parent\n' ;;
  *) printf 'FAIL dir diagnostic names the directory and parent: got %q\n' "$out" >&2; failures=$((failures + 1)) ;;
esac

# 22. --files mode applies both checks (no base tree: every directory is new).
repo22="$(new_repo)"
export ESHU_STUTTER_REPO_ROOT="$repo22"
rc="$(run_gate --files go/internal/query/queryauth/handler.go)"
check "--files stuttering dir is RED" 1 "$rc"
rc="$(run_gate --files docs/internal/evidence/6821-evidence.md)"
check "--files stuttering non-Go file is RED" 1 "$rc"
rc="$(run_gate --files go/internal/query/auth/handler.go)"
check "--files clean nested path is GREEN" 0 "$rc"

# ---------------------------------------------------------------------------
# Review round 1 (#6821): compound directory names, default-mode directory
# RED, fixture roots, ancestors of fixture roots, case, short parents.
# ---------------------------------------------------------------------------

# F1: a leaf directory that itself contains - or _ must still match when the
# stem repeats it as a contiguous run of words.
stage_case "hyphenated leaf repeated in stem is RED" 1 docs/public/run-locally/run-locally-compose.md
stage_case "underscored leaf repeated in stem is RED" 1 scripts/ci_gates/ci_gates_extra.sh
stage_case "hyphenated leaf repeated with underscores is RED" 1 docs/public/run-locally/run_locally_compose.md
stage_case "leaf run in the middle of the stem is RED" 1 docs/public/run-locally/x-run-locally-y.md
stage_case "hyphenated leaf with a partial run is GREEN" 0 docs/public/run-locally/run-compose.md
stage_case "hyphenated leaf split around another word is GREEN" 0 docs/public/run-locally/run-fast-locally.md
stage_case "stem equal to a hyphenated leaf is GREEN" 0 docs/public/run-locally/run-locally.md
stage_case "stem equal to leaf across hyphen and underscore is GREEN" 0 docs/public/run-locally/run_locally.md

# F2: default mode (the make pre-push path) must catch a COMMITTED new
# stuttering directory, not only staged ones.
repo23="$(new_repo)"
export ESHU_STUTTER_REPO_ROOT="$repo23"
mkdir -p "$repo23/go/internal/query/auth"
printf 'package auth\n' > "$repo23/go/internal/query/auth/a.go"
git -C "$repo23" add -A && git -C "$repo23" commit -qm base
mkdir -p "$repo23/go/internal/query/queryauth"
printf 'package queryauth\n' > "$repo23/go/internal/query/queryauth/a.go"
git -C "$repo23" add -A && git -C "$repo23" commit -qm stutter
export ESHU_STUTTER_UPSTREAM="HEAD~1"
rc="$(run_gate)"
check "default mode over a committed new stuttering dir is RED" 1 "$rc"
unset ESHU_STUTTER_UPSTREAM

# F3: fixture corpora mirror third-party conventions (Ruby *_test.rb, pytest
# test_*.py, Dart *_test.dart), so a `fixtures` component exempts like
# `testdata` does.
stage_case "non-Go file under tests/fixtures is exempt" 0 tests/fixtures/ruby_app/test/user_test.rb
stage_case "pytest file under nested fixtures is exempt" 0 go/internal/parser/fixtures/py/test/test_api.py
stage_case "stuttering dir under tests/fixtures is exempt" 0 tests/fixtures/dogfood/dartrepo/dartrepo_core/a.dart
stage_case "stuttering dir under a nested fixtures root is exempt" 0 go/internal/parser/fixtures/query/queryauth/a.txt
stage_case "Go file under fixtures is still checked" 1 go/internal/parser/fixtures/entity/entity_checks.go

# F4: the fixture skip covers the root and everything below it, never the
# directories above it.
stage_case "stuttering dir above testdata is RED" 1 go/internal/query/queryauth/testdata/x.json
stage_case "stuttering dir above fixtures is RED" 1 go/internal/query/queryauth/fixtures/x.json
stage_case "stuttering dir above testdata with a Go file is RED" 1 go/internal/query/queryauth/testdata/fixture.go

# F7: extensions compare case-insensitively.
stage_case "uppercase extension stutter is RED" 1 docs/public/images/logo-images.PNG
stage_case "uppercase Go extension stutter is RED" 1 go/internal/entity/entity_checks.GO

# F13: comparison is case-insensitive in every position. The parent and the
# child differ in case here, so a case-sensitive gate cannot pass these.
stage_case "mixed-case dir stutter (query/QueryAuth) is RED" 1 go/internal/query/QueryAuth/a.go
stage_case "mixed-case file stutter (Entity/entity_x.go) is RED" 1 go/internal/Entity/entity_x.go
stage_case "mixed-case hyphenated file stutter (Run-Locally/run-locally-x.md) is RED" 1 docs/Run-Locally/run-locally-x.md
stage_case "mixed-case leaf with upper-case stem is RED" 1 docs/internal/entity/ENTITY_x.md

# F12: parent-name matching is tiered by the parent's length (owner ruling).
#   1-2 chars: exact whole-word token only.
#   3 chars:   exact token OR the child starts with the parent (no suffix).
#   4+ chars:  prefix or suffix.
stage_case "1-char parent, prefix of a longer word (parser/c/cpp) is GREEN" 0 go/internal/parser/c/cpp/a.go
stage_case "1-char parent, whole word (c/c-api) is RED" 1 go/internal/parser/c/c-api/a.go
stage_case "2-char parent, prefix of a longer word (db/dbmigrate) is GREEN" 0 go/internal/db/dbmigrate/a.go
stage_case "2-char parent, suffix of a longer word (db/nornicdb) is GREEN" 0 go/internal/db/nornicdb/a.go
stage_case "2-char parent, whole word prefix (db/db-auth) is RED" 1 go/internal/db/db-auth/a.go
stage_case "2-char parent, whole word suffix (db/auth_db) is RED" 1 go/internal/db/auth_db/a.go
stage_case "2-char parent repeated exactly (db/db) is RED" 1 go/internal/db/db/a.go
stage_case "3-char parent, glued prefix (api/apiauth) is RED" 1 go/internal/api/apiauth/a.go
stage_case "3-char parent, glued prefix (mcp/mcpserver) is RED" 1 go/internal/mcp/mcpserver/a.go
stage_case "3-char parent, glued prefix (aws/awsiam) is RED" 1 go/internal/aws/awsiam/a.go
stage_case "3-char parent, glued prefix (sql/sqlstore) is RED" 1 go/internal/sql/sqlstore/a.go
stage_case "3-char parent, glued prefix (cli/clitool) is RED" 1 go/internal/cli/clitool/a.go
stage_case "3-char parent, glued prefix (git/github) is RED" 1 docs/internal/git/github/a.md
stage_case "3-char parent, whole word (api/api-auth) is RED" 1 go/internal/api/api-auth/a.go
stage_case "3-char parent, whole word suffix (api/auth_api) is RED" 1 go/internal/api/auth_api/a.go
stage_case "3-char parent, suffix of a longer word (sql/postgresql) is GREEN" 0 go/internal/sql/postgresql/a.go
stage_case "3-char parent, suffix of a longer word (net/dotnet) is GREEN" 0 go/internal/net/dotnet/a.go
stage_case "3-char parent, middle of a longer word (aws/xawsx) is GREEN" 0 go/internal/aws/xawsx/a.go
stage_case "4-char parent, glued prefix (repo/repoauth) is RED" 1 go/internal/repo/repoauth/a.go
stage_case "4-char parent, glued suffix (repo/authrepo) is RED" 1 go/internal/repo/authrepo/a.go
stage_case "4-char parent, middle of a longer word (repo/xrepox) is GREEN" 0 go/internal/repo/xrepox/a.go

# Bulk lowercasing must keep path fields aligned across renames and
# multi-file batches (uppercase directory, rename, unrelated adds in one run).
repo24="$(new_repo)"
mkdir -p "$repo24/go/internal/Query/Auth"
printf 'x\n' > "$repo24/go/internal/Query/Auth/A.go"
export ESHU_STUTTER_REPO_ROOT="$repo24"
git -C "$repo24" add -A && git -C "$repo24" commit -qm base
mkdir -p "$repo24/go/internal/Query/QueryAuth" "$repo24/docs/Clean"
git -C "$repo24" mv go/internal/Query/Auth/A.go go/internal/Query/QueryAuth/A.go
printf 'x\n' > "$repo24/docs/Clean/Notes.md"
git -C "$repo24" add -A
set +e
out="$(bash "$gate" --staged 2>&1)"
rc=$?
set -e
check "mixed-case rename batch is RED" 1 "$rc"
case "$out" in
  *go/internal/Query/QueryAuth*) printf 'ok   diagnostic keeps original path case\n' ;;
  *) printf 'FAIL diagnostic keeps original path case: got %q\n' "$out" >&2; failures=$((failures + 1)) ;;
esac

if [ "$failures" != "0" ]; then
  printf 'test-verify-filename-stutter: %d case(s) failed\n' "$failures" >&2
  exit 1
fi
printf 'test-verify-filename-stutter: all cases pass\n'
