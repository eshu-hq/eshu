# Sourced by test-run-live-postgres-readiness-tests.sh after its shared fixtures are initialized.
# Shard fixture checks live here to keep the parent self-test below the file-size cap.
# Sharded execution selects a time-weighted contiguous slice of the enrolled
# proofs through the shards planner (the same pipe the CI matrix uses), runs only those
# packages, and leaves a fail-closed per-shard report. The subset comes from
# the planner, so this file still carries no test-name copy.
shards_lib="${repo_root}/scripts/lib/live_postgres_readiness_shards.py"
shard_proofs() {
  python3 "${checker}" list-packages |
    python3 "${shards_lib}" plan --shard "$1" |
    while IFS=$'\t' read -r pkg _pattern names; do
      for name in ${names}; do printf '%s|%s\n' "${pkg}" "${name}"; done
    done
}
write_subset_events() {
  local entry pkg name seen_pkgs
  rm -f "${fake}"/*.jsonl "${fake}"/*.names "${fake}"/*.exit "${fake}"/*.bak
  seen_pkgs=""
  for entry in "$@"; do
    pkg="${entry%%|*}"
    name="${entry##*|}"
    printf '%s\n' "${name}" >>"${fake}/$(pkg_key "${pkg}").names"
    printf '{"Action":"run","Test":"%s"}\n' "${name}" >>"$(events_of "${pkg}")"
    printf '{"Action":"pass","Test":"%s","Elapsed":0.25}\n' "${name}" >>"$(events_of "${pkg}")"
    [[ "${seen_pkgs}" == *" ${pkg} "* ]] || seen_pkgs="${seen_pkgs} ${pkg} "
  done
  for pkg in ${seen_pkgs}; do
    printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/%s","Elapsed":1.0}\n' \
      "${pkg#./}" >>"$(events_of "${pkg}")"
  done
}
run_shard() { bash "${runner}" --shard "$1" --report "${fake}/shard-${1%/*}.json" --head "$2" 2>&1; }
report_ok() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["ok"])' "$1"
}
report_count() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["expected_count"])' "$1"
}
shard_head="a000000000000000000000000000000000000000"
covered=""
for shard in 1/4 2/4 3/4 4/4; do
  mapfile -t sproofs < <(shard_proofs "${shard}")
  [[ "${#sproofs[@]}" -gt 0 ]] || fail "shard ${shard} selected no proofs"
  write_subset_events "${sproofs[@]}"
  : >"${fake}/calls"
  out="$(run_shard "${shard}" "${shard_head}")" || fail "shard ${shard} run failed: ${out}"
  [[ "$(report_ok "${fake}/shard-${shard%/*}.json")" == "True" ]] || fail "shard ${shard} report not ok: ${out}"
  [[ "$(report_count "${fake}/shard-${shard%/*}.json")" -eq "${#sproofs[@]}" ]] ||
    fail "shard ${shard} report count differs from selection: ${out}"
  for entry in "${sproofs[@]}"; do covered="${covered}${entry}"$'\n'; done
done
[[ "$(printf '%s' "${covered}" | sort -u | wc -l | tr -d '[:space:]')" -eq "${total}" ]] ||
  fail "four shard selections do not cover every enrolled proof"
[[ "$(printf '%s' "${covered}" | sort | uniq -d | wc -l | tr -d '[:space:]')" -eq 0 ]] ||
  fail "shard selections overlap"
# A shard run with a dropped test event fails naming the missing run.
mapfile -t sproofs < <(shard_proofs "2/4")
write_subset_events "${sproofs[@]}"
second_pkg="${sproofs[0]%%|*}"
second_name="${sproofs[0]##*|}"
sed -i.bak "/\"Test\":\"${second_name}\"/d" "$(events_of "${second_pkg}")"
out="$(run_shard "2/4" "${shard_head}")" && fail "shard 2/4 with a dropped test passed"
[[ "${out}" == *"missing or duplicate event"* ]] || fail "dropped shard test not named: ${out}"
# A shard run with an event for a test outside its selection fails closed.
write_subset_events "${sproofs[@]}"
printf '{"Action":"run","Test":"%s"}\n{"Action":"pass","Test":"%s","Elapsed":0.1}\n' \
  "${first_proof}" "${first_proof}" >>"$(events_of "${second_pkg}")"
out="$(run_shard "2/4" "${shard_head}")" && fail "shard 2/4 with an unexpected test passed"
[[ "${out}" == *"unexpected test event"* ]] || fail "unexpected shard test not named: ${out}"
# An invalid shard identifier fails before any go test starts.
write_subset_events "${sproofs[@]}"
: >"${fake}/calls"
out="$(run_shard "5/4" "${shard_head}")" && fail "invalid shard passed"
[[ "${out}" == *"invalid shard"* ]] || fail "invalid shard not named: ${out}"
[[ ! -s "${fake}/calls" ]] || fail "go test ran for an invalid shard"
# Subtests resolve to their enrolled parent: `go test -run` matches the
# parent and emits Parent/child events, which the shard subset must accept
# exactly as the full suite does. A child of an unenrolled parent fails.
mapfile -t sproofs < <(shard_proofs "1/4")
write_subset_events "${sproofs[@]}"
first_parent="${sproofs[0]##*|}"
printf '{"Action":"run","Test":"%s/seeded-child"}\n{"Action":"pass","Test":"%s/seeded-child","Elapsed":0.1}\n' \
  "${first_parent}" "${first_parent}" >>"$(events_of "${sproofs[0]%%|*}")"
out="$(run_shard "1/4" "${shard_head}")" || fail "shard 1/4 with subtest events failed: ${out}"
write_subset_events "${sproofs[@]}"
printf '{"Action":"run","Test":"TestZzzDecoyNotEnrolled/seeded-child"}\n{"Action":"pass","Test":"TestZzzDecoyNotEnrolled/seeded-child","Elapsed":0.1}\n' \
  >>"$(events_of "${sproofs[0]%%|*}")"
out="$(run_shard "1/4" "${shard_head}")" && fail "shard 1/4 with a foreign subtest passed"
[[ "${out}" == *"unexpected test event"* ]] || fail "foreign subtest not named: ${out}"
# Shard-plus-degrade: without a Neo4j backend a shard excuses its graph
# proofs, and an excused proof that fails still fails the leg by name.
shard_graph_tests="$(python3 "${checker}" list-neo4j-tests)" || fail "list-neo4j-tests rejected"
degraded_shard=""
degraded_graph=""
for shard in 1/4 2/4 3/4 4/4; do
  mapfile -t sproofs < <(shard_proofs "${shard}")
  for entry in "${sproofs[@]}"; do
    for g in ${shard_graph_tests}; do
      if [[ "${entry##*|}" == "${g}" ]]; then degraded_shard="${shard}"; degraded_graph="${g}"; fi
    done
  done
done
[[ -n "${degraded_shard}" ]] || fail "no shard carries a graph proof"
cp "${fake}/envs" "${fake}/envs.shard-degrade"
rg -v '^(ESHU_NEO4J_|ESHU_GRAPH_BACKEND)' "${fake}/envs.shard-degrade" >"${fake}/envs"
unset ESHU_NEO4J_URI ESHU_NEO4J_USERNAME ESHU_NEO4J_PASSWORD ESHU_GRAPH_BACKEND
mapfile -t sproofs < <(shard_proofs "${degraded_shard}")
write_subset_events "${sproofs[@]}"
for entry in "${sproofs[@]}"; do
  if [[ "${entry##*|}" == "${degraded_graph}" ]]; then
    sed -i.bak "/\"Test\":\"${degraded_graph}\"/d" "$(events_of "${entry%%|*}")"
  fi
done
out="$(run_shard "${degraded_shard}" "${shard_head}")" || fail "degraded shard ${degraded_shard} failed: ${out}"
write_subset_events "${sproofs[@]}"
for entry in "${sproofs[@]}"; do
  if [[ "${entry##*|}" == "${degraded_graph}" ]]; then
    sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"${degraded_graph}\"/\"Action\":\"fail\",\"Test\":\"${degraded_graph}\"/" "$(events_of "${entry%%|*}")"
  fi
done
out="$(run_shard "${degraded_shard}" "${shard_head}")" && fail "degraded shard with graph failure passed"
[[ "${out}" == *"does not cover failure"* ]] || fail "excused shard failure not named: ${out}"
cp "${fake}/envs.shard-degrade" "${fake}/envs"
export ESHU_NEO4J_URI="${ESHU_EXPECTED_NEO4J_URI}"
export ESHU_NEO4J_USERNAME="${ESHU_EXPECTED_NEO4J_USERNAME}"
export ESHU_NEO4J_PASSWORD="${ESHU_EXPECTED_NEO4J_PASSWORD}"
export ESHU_GRAPH_BACKEND=neo4j
write_events
printf '{"Action":"run","Test":"%s/seeded-child"}\n{"Action":"pass","Test":"%s/seeded-child","Elapsed":0.1}\n' \
  "${first_proof}" "${first_proof}" >>"$(events_of "${first_pkg}")"
out="$(run_runner)" || fail "full run with subtest events failed: ${out}"

# The env-excuse loop and the dropped-package assertion above both shell out
# to the search tool, so a host without rg must fail at the guard naming it
# instead of passing with those checks silently satisfied. Seed that absence:
# re-invoke this script with every tool it needs except rg and require the
# guard to fire before any package runs. The farm keeps the seeded run honest:
# with the guard removed the nested run would execute the whole suite and
# pass, proving the fail-open instead of dying on a missing dirname.
probe_bin="$(mktemp -d)"
for tool in bash dirname mktemp rm chmod cat sed awk cmp cp ln mkdir wc tr seq python3 env; do
  target="$(command -v "${tool}")" || fail "probe needs ${tool} on PATH"
  ln -s "${target}" "${probe_bin}/${tool}"
done
[[ ! -e "${probe_bin}/rg" ]] || fail "probe farm must not contain rg"
probe_out="$(PATH="${probe_bin}" "${BASH}" "${repo_root}/scripts/test-run-live-postgres-readiness-tests.sh" 2>&1)" && fail "self-test passed without rg"
[[ "${probe_out}" == *"requires rg"* ]] || fail "missing rg not named: ${probe_out}"
rm -rf "${probe_bin}"

printf 'test-run-live-postgres-readiness-tests: PASS\n'
