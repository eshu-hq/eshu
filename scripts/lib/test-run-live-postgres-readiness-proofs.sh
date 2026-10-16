# Sourced by test-run-live-postgres-readiness-tests.sh after its shared
# fixtures (fake, proofs, packages, checker, fail, run_runner) are set up.
# The fake event writer and the per-proof fail-closed checks live here to
# keep the parent self-test below the file-size cap.
#
# Cost contract (#8241): a fixture rebuild forks no process per entry, the
# per-proof checks touch one package stream per seed, and the full runner
# runs once per package per seed, so the self-test stays linear in the
# enrolled proof count. The old shape rebuilt every
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
# enrolled proof to fail, and requires the full runner to name each of them.
#
# Direct phase: for every proof and seed, one clean package stream is edited
# in place and checked by the results verifier with the exact arguments the
# unsharded runner uses, then restored. This catches a verifier that misses
# one proof among passing ones.
#
# Runner phase: for every package and seed, every proof of that package is
# seeded at once, and the full runner must exit nonzero and print
# "<name>: SKIP|FAIL|missing" for each proof. This catches a runner that
# hides or truncates the verifier verdict, with one runner call per package
# per seed instead of one per proof.
check_each_proof() {
  local entry pkg name ev action want label subject out key
  local -a names
  write_events
  for entry in "${proofs[@]}"; do
    pkg="${entry%%|*}"
    name="${entry##*|}"
    ev="${fake}/${pkg//[^a-z]/_}.jsonl"
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
      [[ $'\n'"${out}" == *$'\n'"${name}: ${want}"* ]] || fail "${label} not named: ${out}"
      mv "${ev}.clean" "${ev}"
    done
  done
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
