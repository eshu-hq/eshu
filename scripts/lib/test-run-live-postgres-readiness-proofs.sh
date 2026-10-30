# Sourced by test-run-live-postgres-readiness-tests.sh after its shared
# fixtures (fake, seed_dir, proofs, packages, checker, cases_lib, fail,
# run_runner) are set up.
# The fake event writer and the per-proof fail-closed checks live here to
# keep the parent self-test below the file-size cap.
#
# Cost contract (#8241): a fixture rebuild forks no process per entry, the
# direct per-proof checks run in one python process, and the full runner runs
# once per package per seed, so the self-test stays linear in the enrolled
# proof count. The old shape rebuilt every stream and ran the full runner
# three times per proof (O(N^2)) and hit the 30-minute CI cap.

# write_events rebuilds every fake package stream: one run/pass pair per
# enrolled proof, then the passing terminal of each package. The file key is
# pkg_key's mapping, inlined so no entry forks a subshell.
write_events() {
  local entry pkg name key
  rm -f "${fake}"/*.jsonl "${fake}"/*.names "${fake}"/*.exit "${fake}"/*.bak
  for entry in "${proofs[@]}"; do
    pkg="${entry%%|*}"
    name="${entry##*|}"
    key="${pkg//[^a-z]/_}"
    printf '%s\n' "${name}" >>"${fake}/${key}.names"
    printf '{"Action":"run","Test":"%s"}\n{"Action":"pass","Test":"%s","Elapsed":0.25}\n' \
      "${name}" "${name}" >>"${fake}/${key}.jsonl"
  done
  for pkg in "${packages[@]}"; do
    printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/%s","Elapsed":1.0}\n' \
      "${pkg#./}" >>"${fake}/${pkg//[^a-z]/_}.jsonl"
  done
}

# check_proofs_direct requires a skip, a failure, or a missing event for any
# one enrolled proof to fail the results verifier by name. One helper process
# plants all three faults per proof in a copy of its clean package stream and
# runs the verifier's verify_results in-process, with the arguments the
# unsharded runner uses. It exits 3 naming the first proof that passed or was
# not named, and prints the case count on success so a vacuous pass is refused.
# The former shape ran one python subprocess per proof and seed (#8241).
check_proofs_direct() {
  local entry pkg cases rc
  local -a records=()
  for entry in "${proofs[@]}"; do
    pkg="${entry%%|*}"
    records+=("${pkg}"$'\t'"${entry##*|}"$'\t'"${fake}/${pkg//[^a-z]/_}.jsonl")
  done
  cases="$(printf '%s\n' "${records[@]}" | python3 "${cases_lib}" "${checker}" "${seed_dir}" 2>&1)" || {
    rc=$?
    [[ "${rc}" -eq 3 ]] && fail "${cases}"
    fail "proof-case helper exited ${rc}: ${cases}"
  }
  [[ "${cases}" == "$((3 * ${#proofs[@]}))" ]] ||
    fail "per-proof cases checked ${cases}, want $((3 * ${#proofs[@]}))"
}

# check_proofs_runner requires the full runner to name every proof: for each
# package and seed, every proof of that package is seeded at once, and the
# runner must exit nonzero and print "<name>: SKIP|FAIL|missing" for each
# proof. This catches a runner that hides or truncates the verifier verdict,
# with one runner call per package per seed instead of one per proof. The
# calls are independent, so they run in the bounded case pool (#9582), each
# on a private copy of the clean streams; every status and output is checked
# after the drain, in package and seed order.
check_proofs_runner() {
  local i pkg name ev action want out key id rc
  local -a names
  pool_init "${seed_dir}/pool-proofs"
  for i in "${!packages[@]}"; do
    pkg="${packages[${i}]}"
    key="${pkg//[^a-z]/_}"
    [[ -s "${fake}/${key}.names" ]] || fail "${pkg} has no enrolled proofs"
    for action in skip fail missing; do
      id="p${i}-${action}"
      pool_prepare "${id}"
      ev="${pool_case}/fake/${key}.jsonl"
      if [[ "${action}" == missing ]]; then
        sed -i.bak '/"Test":"/d' "${ev}"
      else
        sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"/\"Action\":\"${action}\",\"Test\":\"/" "${ev}"
      fi
      rm -f "${ev}.bak"
      cmp -s "${ev}" "${fake}/${key}.jsonl" && fail "${pkg} ${action} seeds changed nothing"
      pool_start "${id}" run_runner
    done
  done
  pool_drain
  for i in "${!packages[@]}"; do
    pkg="${packages[${i}]}"
    mapfile -t names <"${fake}/${pkg//[^a-z]/_}.names"
    for action in skip fail missing; do
      id="p${i}-${action}"
      want="${action^^}"
      [[ "${action}" != missing ]] || want=missing
      rc="$(pool_rc "${id}")"
      out="$(pool_out "${id}")"
      [[ "${rc}" -ne 0 ]] || fail "${pkg} ${action} events passed the runner"
      for name in "${names[@]}"; do
        [[ $'\n'"${out}" == *$'\n'"${name}: ${want}"* ]] ||
          fail "${pkg} ${action} events: ${name} not named by the runner: ${out}"
      done
    done
  done
}

# check_package_exits requires a package whose go test exits nonzero to fail
# the run naming that package. Each package's case runs in the pool on a
# private copy of the clean streams with that package's exit planted.
check_package_exits() {
  local i pkg id rc out
  write_events
  pool_init "${seed_dir}/pool-exits"
  for i in "${!packages[@]}"; do
    pkg="${packages[${i}]}"
    id="e${i}"
    pool_prepare "${id}"
    printf '1\n' >"${pool_case}/fake/${pkg//[^a-z]/_}.exit"
    pool_start "${id}" run_runner
  done
  pool_drain
  for i in "${!packages[@]}"; do
    pkg="${packages[${i}]}"
    rc="$(pool_rc "e${i}")"
    out="$(pool_out "e${i}")"
    [[ "${rc}" -ne 0 ]] || fail "go nonzero exit in ${pkg} passed"
    [[ "${out}" == *"${pkg}: go test exited 1"* ]] || fail "go exit in ${pkg} not reported: ${out}"
  done
}

# check_each_proof runs the direct phase, then the all-names runner phase, on
# one clean set of fake event streams.
check_each_proof() {
  write_events
  check_proofs_direct
  check_proofs_runner
}

# Seed every package in two full-run cases. Verify each package's runner
# verdict and callback, so an earlier failure cannot hide a middle package.
# The per-proof runner cases above remain isolated for exact test diagnostics.
check_batched_package_failures() {
  local scenario pkg out events called count total_calls
  local -A callbacks
  for scenario in zero terminal; do
    write_events
    for pkg in "${packages[@]}"; do
      events="$(events_of "${pkg}")"
      if [[ "${scenario}" == zero ]]; then
        printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/%s","Elapsed":0.1}\n' \
          "${pkg#./}" >"${events}"
      else
        sed -i.bak 's/"Action":"pass","Package"/"Action":"fail","Package"/' "${events}"
      fi
    done
    : >"${fake}/calls"
    out="$(LIVE_READINESS_DEBUG_DIR="${seed_dir}/.readiness-debug" run_runner)" &&
      fail "batched ${scenario} violations passed"
    callbacks=()
    while IFS= read -r called; do
      callbacks["${called}"]=$(( ${callbacks["${called}"]:-0} + 1 ))
    done <"${fake}/calls"
    for pkg in "${packages[@]}"; do
      [[ "${out}" == *"live-postgres-readiness: ${pkg}: expected tests did not all pass"* ]] ||
        fail "batched ${scenario} did not report verifier failure for ${pkg}: ${out}"
      if [[ "${scenario}" == terminal ]]; then
        [[ "${out}" == *"package terminal (${pkg})"* ]] ||
          fail "batched terminal package terminal of ${pkg} not named: ${out}"
      fi
      count="${callbacks["${pkg}"]:-0}"
      [[ "${count}" -eq 1 ]] || fail "batched ${scenario} callback count for ${pkg}: ${count}"
    done
    total_calls="$(wc -l <"${fake}/calls")"
    [[ "${total_calls}" -eq "${#packages[@]}" ]] ||
      fail "batched ${scenario} invoked ${total_calls}/${#packages[@]} packages"
  done
}
