# shellcheck shell=bash
# Bounded concurrent runner cases for the PostgreSQL readiness self-test
# (#9582). Sourced by scripts/test-run-live-postgres-readiness-tests.sh, which
# defines fail and fake, and by scripts/test-live-postgres-readiness-pool.sh.
#
# The self-test runs the full runner once per package per seed. Those runs
# share nothing but read-only inputs, so they run in a pool of at most
# pool_jobs at once instead of one at a time on a multi-core CI runner.
#
# Isolation: each case gets a private copy of the fake event dir (the fake go
# appends to <fake>/calls and reads its streams from there) and a private
# LIVE_READINESS_DEBUG_DIR (the runner rm -rf's its debug dir every run, and
# its default is one shared dir under the repo root). The runner's own scratch
# dir comes from mktemp -d. The runner opens no ports and starts no
# containers, so nothing else is shared.
#
# Fail closed: a case writes its exit status to <case>/rc only after the
# runner returns, through a rename, so a crashed or killed case leaves no
# status and pool_drain fails naming it. pool_drain waits for every case, so a
# hung case holds the step until the job cap cancels it; it can never pass.
# Callers check every case's status and output after the drain, in the order
# the cases were started, so the first reported failure does not depend on
# which case finished first.
# shellcheck disable=SC2154,SC2034  # the sourcing test defines fail and fake and reads pool_case.

pool_dir=""
pool_jobs=1
pool_case=""
pool_ids=()
pool_live=()

# pool_init DIR resets the pool to an empty DIR. The width is
# LIVE_READINESS_SELFTEST_JOBS when set, else the CPU count capped at 4 (the
# hosted ubuntu runner has 4 vCPUs).
pool_init() {
  local jobs="${LIVE_READINESS_SELFTEST_JOBS:-}"
  pool_dir="$1"
  rm -rf -- "${pool_dir}"
  mkdir -p -- "${pool_dir}"
  pool_case=""
  pool_ids=()
  pool_live=()
  if [[ -z "${jobs}" ]]; then
    jobs="$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || printf '1')"
    [[ "${jobs}" =~ ^[1-9][0-9]*$ ]] || jobs=1
    [[ "${jobs}" -le 4 ]] || jobs=4
  fi
  [[ "${jobs}" =~ ^[1-9][0-9]*$ ]] ||
    fail "LIVE_READINESS_SELFTEST_JOBS must be a positive integer, got '${jobs}'"
  pool_jobs="${jobs}"
}

# pool_prepare ID makes case ID's private copy of the current fake dir and
# sets pool_case to the case dir. Callers plant the case's seed in
# "${pool_case}/fake" before pool_start.
pool_prepare() {
  local id="$1"
  [[ "${id}" =~ ^[A-Za-z0-9._-]+$ ]] || fail "pool case id '${id}' is not a plain name"
  [[ ! -e "${pool_dir}/${id}" ]] || fail "pool case id ${id} is not unique"
  mkdir -p -- "${pool_dir}/${id}"
  cp -R -- "${fake}" "${pool_dir}/${id}/fake"
  : >"${pool_dir}/${id}/fake/calls"
  pool_case="${pool_dir}/${id}"
}

# pool_start ID CMD... runs CMD for prepared case ID in the background, once a
# pool slot is free, with the case's private fake and debug dirs. Output goes
# to <case>/out; the exit status goes to <case>/rc after CMD returns.
pool_start() {
  local id="$1" case_dir
  shift
  case_dir="${pool_dir}/${id}"
  [[ -d "${case_dir}/fake" && ! -e "${case_dir}/rc" ]] || fail "pool case ${id} was not prepared"
  pool_prune
  while [[ "${#pool_live[@]}" -ge "${pool_jobs}" ]]; do
    wait -n || true
    pool_prune
  done
  (
    export ESHU_FAKE_DIR="${case_dir}/fake"
    export LIVE_READINESS_DEBUG_DIR="${case_dir}/.readiness-debug"
    rc=0
    "$@" >"${case_dir}/out" 2>&1 </dev/null || rc=$?
    printf '%s\n' "${rc}" >"${case_dir}/rc.tmp" && mv -- "${case_dir}/rc.tmp" "${case_dir}/rc"
  ) &
  pool_live+=("$!")
  pool_ids+=("${id}")
}

# pool_prune keeps only the pool pids still running. wait -n may also wake
# for a process substitution, so the slot count is recounted, not decremented.
pool_prune() {
  local pid
  local -a live=()
  for pid in ${pool_live[@]+"${pool_live[@]}"}; do
    if kill -0 "${pid}" 2>/dev/null; then live+=("${pid}"); fi
  done
  pool_live=(${live[@]+"${live[@]}"})
}

# pool_drain waits for every started case and fails naming the first case,
# in start order, that left no exit status.
pool_drain() {
  local id
  wait || true
  pool_live=()
  for id in "${pool_ids[@]}"; do
    [[ -f "${pool_dir}/${id}/rc" ]] || fail "pool case ${id} left no exit status (crashed or killed)"
  done
}

# pool_rc ID prints case ID's exit status; a missing or malformed status fails.
pool_rc() {
  local rc
  rc="$(cat -- "${pool_dir}/$1/rc" 2>/dev/null)" || fail "pool case $1 left no exit status"
  [[ "${rc}" =~ ^[0-9]+$ ]] || fail "pool case $1 exit status is malformed: '${rc}'"
  printf '%s' "${rc}"
}

# pool_out ID prints case ID's combined output.
pool_out() { cat -- "${pool_dir}/$1/out"; }
