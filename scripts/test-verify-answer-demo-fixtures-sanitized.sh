#!/usr/bin/env bash
#
# test-verify-answer-demo-fixtures-sanitized.sh -- mirror test for
# verify-answer-demo-fixtures-sanitized.sh (#7918).
#
# Seeds each scrubber pattern arm in a throwaway repo and pins the widened
# target list: a real-name fixture fails in every covered directory,
# including go/internal/query as a whole-directory target (#7803 batch 2),
# while the generic same-shape replacements this drive ships stay green.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
gate="${script_dir}/verify-answer-demo-fixtures-sanitized.sh"
failures=0

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

gate_targets() {
  # The gate script's scan targets, one per line, parsed from its targets
  # array so this skeleton cannot drift from the gate (#7918 P2): adding a
  # target to the gate without updating this mirror changes the parse output
  # the skeleton is built from, never a hardcoded list. Only lines starting
  # with optional whitespace plus a quote are target lines: a future comment
  # containing quotes must not inject a phantom skeleton target.
  sed -n '/^targets=(/,/^)/p' "$gate" | rg '^[[:space:]]*"' | rg -o '"[^"]+"' | tr -d '"'
}

new_repo() {
  # A missing target is a gate error (exit 2), never a pass, so every temp
  # repo carries the full target skeleton before seeding. The skeleton is
  # derived from gate_targets, not restated, so coverage and mirror cannot
  # drift apart.
  local dir target
  dir="$(mktemp -d)"
  printf '%s\n' "$dir" >>"$tmp_list"
  while IFS= read -r target; do
    [ -n "$target" ] || continue
    case "$target" in
      *.go)
        mkdir -p "$dir/$(dirname "$target")"
        : >"$dir/$target"
        ;;
      *)
        mkdir -p "$dir/$target"
        ;;
    esac
  done < <(gate_targets)
  # An empty parse (gate format drift) must fail loudly, never seed silently:
  # the go/internal/query directory target anchors the parse output.
  if [ ! -d "$dir/go/internal/query" ]; then
    printf 'FAIL skeleton: go/internal/query missing, gate targets parse changed\n' >&2
    return 1
  fi
  printf '%s\n' "$dir"
}

seed() {
  # $1 = repo, $2 = relative path, rest = content lines.
  local repo="$1" path="$2"
  shift 2
  mkdir -p "$repo/$(dirname "$path")"
  printf '%s\n' "$@" >"$repo/$path"
}

run_gate() {
  # $1 = repo dir.
  local rc
  set +e
  ESHU_FIXTURE_SCRUBBER_ROOT="$1" bash "$gate" >/dev/null 2>&1
  rc=$?
  set -e
  printf '%s' "$rc"
}

# 1. RED: the api-node- convention name in a covered console fixture.
repo="$(new_repo)"
seed "$repo" apps/console/src/api/fixture.test.ts 'label: "api-node-boats",'
check "api-node- name under apps/console/src is RED" 1 "$(run_gate "$repo")"

# 2. RED: the same name in the widened Go fixture directory.
repo="$(new_repo)"
seed "$repo" go/internal/query/visualization/packet_test.go '"service_name": "api-node-boats",'
check "api-node- name under go/internal/query/visualization is RED" 1 "$(run_gate "$repo")"

# 3. RED: the same name in a go/internal/query file (directory target).
repo="$(new_repo)"
seed "$repo" go/internal/query/entity_resolve_contract_test.go '{"name":"api-node-boats","type":"workload"}'
check "api-node- name in go/internal/query file target is RED" 1 "$(run_gate "$repo")"

# 4. RED: a machine user path in a covered fixture.
repo="$(new_repo)"
seed "$repo" apps/console/src/e2e/route.test.ts 'url: "http://localhost/@fs/Users/operator/tree/node_modules/vite/env.mjs",'
check "/Users/ path under apps/console/src is RED" 1 "$(run_gate "$repo")"

# 5. RED: the environment-token arm still applies inside fixture trees. The
# token is assembled at runtime: this mirror's own added lines must not
# carry the literal (the identifier gate scans added lines).
env_prefix="ops-"
repo="$(new_repo)"
seed "$repo" go/internal/query/service/story_test.go "// measured on ${env_prefix}qa in staging"
check "environment token under go/internal/query/service is RED" 1 "$(run_gate "$repo")"

# 6. RED: the diagnostic names the offending path.
repo="$(new_repo)"
seed "$repo" apps/console/src/api/fixture.test.ts 'label: "api-node-boats",'
set +e
out="$(ESHU_FIXTURE_SCRUBBER_ROOT="$repo" bash "$gate" 2>&1)"
rc=$?
set -e
check "offending-path diagnostic run is RED" 1 "$rc"
case "$out" in
  *apps/console/src/api/fixture.test.ts*) printf 'ok   diagnostic names the offending path\n' ;;
  *) printf 'FAIL diagnostic names the offending path: got %q\n' "$out" >&2; failures=$((failures + 1)) ;;
esac

# 7. GREEN: the generic same-shape replacements this drive ships.
repo="$(new_repo)"
seed "$repo" apps/console/src/api/fixture.test.ts 'label: "api-demo-shop",' 'label: "iac-eks-gitops",' 'label: "terraform-stack-demo",'
seed "$repo" apps/console/src/components/list.test.tsx 'node("viznode:source", "api-demo-shop", "source_repository");' 'node("viznode:target", "buyer-web", "workload");'
seed "$repo" apps/console/src/e2e/route.test.ts 'url: "http://localhost/@fs/home/demo/fixture-worktree/node_modules/vite/env.mjs",'
seed "$repo" go/internal/query/visualization/packet_test.go '"service_id": "workload:api-demo-shop",'
seed "$repo" go/internal/query/service/story_test.go '"source_repo_name": "iac-eks-gitops",'
seed "$repo" go/internal/query/entity_resolve_contract_test.go '{"name":"api-demo-shop","type":"workload"}'
check "generic same-shape replacements are GREEN" 0 "$(run_gate "$repo")"

# 8. GREEN: a clean tree passes.
repo="$(new_repo)"
seed "$repo" apps/console/src/api/fixture.test.ts 'label: "example-shop",'
check "clean tree is GREEN" 0 "$(run_gate "$repo")"

# 9. Fail closed: a missing target errors (exit 2), never passes.
repo="$(mktemp -d)"
printf '%s\n' "$repo" >>"$tmp_list"
seed "$repo" apps/console/src/api/fixture.test.ts 'label: "example-shop",'
check "missing target directory exits 2" 2 "$(run_gate "$repo")"

# 10. SYNC: every target of the gate appears in this gate's triggers block
# in gates-02.yaml, so a renamed target cannot silently lose CI selection
# (#7918 P2). The search is scoped to the triggers block: a target mentioned
# in a comment or under self_test_triggers elsewhere in the file must not
# satisfy it. File targets match literally; directory targets match their
# tree glob, so deleting the go/internal/query tree trigger fails loudly.
triggers_yaml="${script_dir}/../specs/ci-gates.d/gates-02.yaml"
triggers_block="$(mktemp)"
printf '%s\n' "$triggers_block" >>"$tmp_list"
sed -n '/id: answer-demo-fixtures-sanitized/,/self_test_triggers:/p' "$triggers_yaml" | sed -n '/^ *triggers:/,$p' | sed '$d' >"$triggers_block"
sync_failures=0
while IFS= read -r sync_target; do
  [ -n "$sync_target" ] || continue
  case "$sync_target" in
    *.go)
      if ! rg -q --fixed-strings -- "- \"$sync_target\"" "$triggers_block"; then
        printf 'FAIL trigger sync: gate file target %s not in this gate triggers block\n' "$sync_target" >&2
        sync_failures=$((sync_failures + 1))
      fi
      ;;
    *)
      if ! rg -q --fixed-strings -- "- \"$sync_target/**\"" "$triggers_block"; then
        printf 'FAIL trigger sync: gate directory target %s/** not in this gate triggers block\n' "$sync_target" >&2
        sync_failures=$((sync_failures + 1))
      fi
      ;;
  esac
done < <(gate_targets)
check "gate targets are this gate triggers" 0 "$sync_failures"

if [ "$failures" -ne 0 ]; then
  printf '%s case(s) failed\n' "$failures" >&2
  exit 1
fi
printf 'answer-demo fixture scrubber mirror passed\n'
