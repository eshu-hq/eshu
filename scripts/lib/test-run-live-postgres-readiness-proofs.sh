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
# with one runner call per package per seed instead of one per proof.
check_proofs_runner() {
  local pkg name ev action want out key
  local -a names
  for pkg in "${packages[@]}"; do
    key="${pkg//[^a-z]/_}"
    ev="${fake}/${key}.jsonl"
    mapfile -t names <"${fake}/${key}.names"
    [[ "${#names[@]}" -gt 0 ]] || fail "${pkg} has no enrolled proofs"
    for action in skip fail missing; do
      cp "${ev}" "${ev}.clean"
      if [[ "${action}" == missing ]]; then
        sed -i.bak '/"Test":"/d' "${ev}"
        want=missing
      else
        sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"/\"Action\":\"${action}\",\"Test\":\"/" "${ev}"
        want="${action^^}"
      fi
      cmp -s "${ev}" "${ev}.clean" && fail "${pkg} ${action} seeds changed nothing"
      out="$(run_runner)" && fail "${pkg} ${action} events passed the runner"
      for name in "${names[@]}"; do
        [[ $'\n'"${out}" == *$'\n'"${name}: ${want}"* ]] ||
          fail "${pkg} ${action} events: ${name} not named by the runner: ${out}"
      done
      mv "${ev}.clean" "${ev}"
    done
  done
  rm -f "${fake}"/*.bak
}

# check_each_proof runs the direct phase, then the all-names runner phase, on
# one clean set of fake event streams.
check_each_proof() {
  write_events
  check_proofs_direct
  check_proofs_runner
}
