# Sourced by test-run-live-postgres-readiness-tests.sh after its shared
# fixtures (fake, proofs, packages, checker, fail, run_runner) are set up.
# The fake event writer and the per-proof fail-closed checks live here to
# keep the parent self-test below the file-size cap.
#
# Cost contract (#8241): a fixture rebuild forks no process per entry, and
# the per-proof checks touch one package stream per seed, so the self-test
# stays linear in the enrolled proof count. The old shape rebuilt every
# stream and ran the full runner three times per proof (O(N^2)) and hit the
# 30-minute CI cap.

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

# check_each_proof requires a skip, a failure, or a missing event for any one
# enrolled proof to fail. Each seed edits one clean package stream in place.
# The results verifier that the runner calls for each package then checks
# that stream directly, with the runner's exact arguments, for every proof.
# The full runner checks the first proof of each package for each seed, which
# proves the runner fails closed on the verifier verdict. The stream is
# restored after each seed, so the next seed starts from clean fixtures.
check_each_proof() {
  local entry pkg name ev action want label subject via_runner checked out
  write_events
  checked=" "
  for entry in "${proofs[@]}"; do
    pkg="${entry%%|*}"
    name="${entry##*|}"
    ev="${fake}/${pkg//[^a-z]/_}.jsonl"
    via_runner=0
    [[ "${checked}" == *" ${pkg} "* ]] || { via_runner=1; checked="${checked}${pkg} "; }
    for action in skip fail missing; do
      cp "${ev}" "${ev}.clean"
      if [[ "${action}" == missing ]]; then
        sed -i.bak "/\"Test\":\"${name}\"/d" "${ev}"
        want=missing
        label="missing ${name} events"
        subject="${label}"
      else
        sed -i.bak "s/\"Action\":\"pass\",\"Test\":\"${name}\"/\"Action\":\"${action}\",\"Test\":\"${name}\"/" "${ev}"
        want="${action^^}"
        label="${name} ${action}"
        subject="${label} event"
      fi
      cmp -s "${ev}" "${ev}.clean" && fail "${label} seed changed nothing"
      out="$(python3 "${checker}" verify-results "${ev}" "${pkg}" 2>&1 </dev/null)" &&
        fail "${subject} passed"
      [[ "${out}" == *"${name}: ${want}"* ]] || fail "${label} not named: ${out}"
      if [[ "${via_runner}" -eq 1 ]]; then
        out="$(run_runner)" && fail "${subject} passed the runner"
        [[ "${out}" == *"${name}: ${want}"* ]] || fail "${label} not named by the runner: ${out}"
      fi
      mv "${ev}.clean" "${ev}"
    done
  done
  rm -f "${fake}"/*.bak
}
