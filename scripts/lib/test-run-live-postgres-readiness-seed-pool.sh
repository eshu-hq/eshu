# shellcheck shell=bash
# Bounded concurrent seeded violations for the PostgreSQL readiness self-test
# (#9629). Sourced by scripts/test-run-live-postgres-readiness-selftest-seeds.sh
# and by scripts/test-live-postgres-readiness-seed-pool.sh, after
# scripts/lib/test-run-live-postgres-readiness-pool.sh (#9582), whose pool_*
# functions run the seeds.
#
# Each seed used to mutate one shared tree and run the self-test in it, one
# seed at a time. Now each seed gets a private copy of the pristine tree, so
# the seeds share only read-only inputs and run in the bounded pool. The
# self-test in a seed tree takes its scratch dir from mktemp -d, keeps its fake
# go and event streams there, and writes its runner debug dir to the case dir
# pool_start names. It opens no ports and starts no containers.
#
# Determinism: seeds are declared in their report order. They may start in a
# different order (the heavy ones first, so they do not finish last), but the
# drain checks every seed in declared order, so the output and the first
# failure are the same for any start order and pool width.
# shellcheck disable=SC2154  # the sourcing script defines fail, prefix, selftest, pristine.

seed_ids=()
seed_wants=()
seed_matches=()
seed_heavy=()
seed_envs=()
seed_root=""

# seed_tree ID copies the pristine tree into a private tree for seed ID and
# sets seed_root to it. The go/ entry stays a link to the read-only module.
seed_tree() {
  local id="$1"
  [[ "${id}" =~ ^[A-Za-z0-9._-]+$ ]] || fail "seed id '${id}' is not a plain name"
  seed_root="${seed_work}/${id}"
  [[ ! -e "${seed_root}" ]] || fail "seed id ${id} is not unique"
  mkdir -p -- "${seed_root}"
  cp -R -- "${pristine}/scripts" "${pristine}/specs" "${seed_root}/"
  cp -P -- "${pristine}/go" "${seed_root}/go"
}

# seed_queue ID WANT [MATCH [WEIGHT [NAME=VALUE]]] declares that the self-test
# in seed ID's tree, the tree seed_tree made last, must exit nonzero with WANT
# as its first failure line (MATCH exact) or its prefix (MATCH prefix). WEIGHT
# heavy starts the seed before the light ones. NAME=VALUE is set in the seed's
# environment.
seed_queue() {
  [[ "${seed_root}" == "${seed_work}/$1" ]] || fail "seed $1 has no tree"
  [[ "${3:-exact}" == exact || "${3:-}" == prefix ]] || fail "seed $1 match '${3:-}' is not exact or prefix"
  [[ -z "${4:-}" || "${4:-}" == heavy ]] || fail "seed $1 weight '${4:-}' is not heavy"
  [[ -z "${5:-}" || "${5:-}" =~ ^[A-Z_]+=[A-Za-z0-9_.-]+$ ]] || fail "seed $1 env '${5:-}' is not NAME=VALUE"
  seed_ids+=("$1")
  seed_wants+=("$2")
  seed_matches+=("${3:-exact}")
  seed_heavy+=("${4:-}")
  seed_envs+=("${5:-}")
}

# seed_timed ID CMD... runs one seed's CMD and records its wall seconds in the
# seed's pool case dir for seed_report_timing.
seed_timed() {
  local id="$1" started rc=0
  shift
  started="$(date +%s)"
  "$@" || rc=$?
  printf '%s\n' "$(($(date +%s) - started))" >"${pool_dir}/${id}/seconds"
  return "${rc}"
}

# seed_run_all starts every declared seed in the pool, heavy seeds first, and
# waits for all of them. A seed that leaves no exit status fails the drain.
seed_run_all() {
  local i pass
  LIVE_READINESS_SELFTEST_JOBS="${LIVE_READINESS_SEED_JOBS:-${LIVE_READINESS_SELFTEST_JOBS:-}}" \
    pool_init "${seed_work}/.pool"
  seed_jobs="${pool_jobs}"
  fake="${seed_work}/.nofake"
  mkdir -p -- "${fake}"
  for pass in heavy ""; do
    for i in "${!seed_ids[@]}"; do
      [[ "${seed_heavy[${i}]}" == "${pass}" ]] || continue
      pool_prepare "${seed_ids[${i}]}"
      pool_start "${seed_ids[${i}]}" seed_timed "${seed_ids[${i}]}" \
        env ${seed_envs[${i}]:+"${seed_envs[${i}]}"} bash "${seed_work}/${seed_ids[${i}]}/scripts/${selftest}"
    done
  done
  pool_drain
}

# seed_check_all checks every seed in declared order and prints its caught
# line, or fails naming the first seed that passed or reported the wrong
# first failure.
seed_check_all() {
  local i id want line rc
  for i in "${!seed_ids[@]}"; do
    id="${seed_ids[${i}]}"
    want="${seed_wants[${i}]}"
    rc="$(pool_rc "${id}")"
    [[ "${rc}" -ne 0 ]] || fail "seed ${id}: self-test passed"
    line="$(pool_out "${id}" | rg -m1 -F -- "${prefix}" || true)"
    line="${line#"${prefix}"}"
    if [[ "${seed_matches[${i}]}" == prefix ]]; then
      [[ "${line}" == "${want}"* ]] || fail "seed ${id}: wanted '${want}...', got '${line}'"
    else
      [[ "${line}" == "${want}" ]] || fail "seed ${id}: wanted '${want}', got '${line}'"
    fi
    printf 'seed %s: caught (%s)\n' "${id}" "${line:0:120}"
  done
}

# seed_report_timing TOTAL prints each seed's wall seconds in declared order
# and the step's TOTAL seconds, and appends them to the GitHub step summary
# when there is one, so a slow seed is named without reading the log.
seed_report_timing() {
  local total="$1" id secs
  local -a rows=()
  for id in "${seed_ids[@]}"; do
    secs="$(cat -- "${pool_dir}/${id}/seconds" 2>/dev/null || printf '?')"
    printf 'seed %s: %ss\n' "${id}" "${secs}"
    rows+=("| \`${id}\` | ${secs} |")
  done
  printf 'seeds: %d in %ss at width %s\n' "${#seed_ids[@]}" "${total}" "${seed_jobs}"
  [[ -n "${GITHUB_STEP_SUMMARY:-}" ]] || return 0
  {
    printf '### Self-test seeded violations\n\n'
    printf '%d seeds in %ss at pool width %s.\n\n' "${#seed_ids[@]}" "${total}" "${seed_jobs}"
    printf '| Seed | Seconds |\n|---|---|\n'
    printf '%s\n' "${rows[@]}"
  } >>"${GITHUB_STEP_SUMMARY}" || printf 'seeds: step summary not written\n' >&2
}
