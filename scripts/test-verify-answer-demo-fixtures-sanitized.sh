#!/usr/bin/env bash
#
# test-verify-answer-demo-fixtures-sanitized.sh -- mirror test for
# verify-answer-demo-fixtures-sanitized.sh (#7918).
#
# Seeds each scrubber pattern arm in a throwaway repo and pins the widened
# target list: a real-name fixture fails in every covered directory and in
# the two go/internal/query file targets, while the generic same-shape
# replacements this drive ships stay green.
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

new_repo() {
  # A missing target is a gate error (exit 2), never a pass, so every temp
  # repo carries the full target skeleton before seeding.
  local dir
  dir="$(mktemp -d)"
  printf '%s\n' "$dir" >>"$tmp_list"
  mkdir -p "$dir/apps/console/src" "$dir/apps/console/prototype" \
    "$dir/go/internal/parser/hcl" "$dir/go/internal/parser/rust" \
    "$dir/go/internal/query/service" "$dir/go/internal/query/visualization" \
    "$dir/go/internal/query"
  # The two go/internal/query file targets must exist: rg errors on a
  # missing file target exactly like on a missing directory.
  : >"$dir/go/internal/query/entity_resolve_contract_test.go"
  : >"$dir/go/internal/query/repository_deployment_evidence_test.go"
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

# 3. RED: the same name in a go/internal/query file target.
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

if [ "$failures" -ne 0 ]; then
  printf '%s case(s) failed\n' "$failures" >&2
  exit 1
fi
printf 'answer-demo fixture scrubber mirror passed\n'
