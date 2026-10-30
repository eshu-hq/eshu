#!/usr/bin/env bash
# RED/GREEN checks for the readiness Go build cache census (#9598): over a
# list of fetched main push runs, every shard of every run must read as a
# cache hit or a miss from its own restore step's log section, and the
# summary must count all-hit, mixed, and all-miss runs. Each case feeds a
# fabricated RUN_DIR (jobs.json plus one shard log per shard) and checks the
# table rows and the summary line. The seeded section breaks the classifier
# in a throwaway copy and requires the same cases to fail, so a census that
# cannot tell a hit from a miss does not pass.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
real="${root}/scripts/dev/readiness-cache-census/census.py"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

fail() { printf 'test-census: %s\n' "$*" >&2; exit 1; }

# make_run builds RUN_DIR for run id $1; remaining args are per-shard
# outcomes in shard order (hit or miss).
make_run() {
  local id="$1" dir="${work}/$1" outcome n=0
  shift
  mkdir -p "${dir}"
  {
    printf '{"jobs": ['
    for outcome in "$@"; do
      n="$((n + 1))"
      ((n > 1)) && printf ','
      printf '{"id": %d, "name": "live-postgres-readiness (postgres18) shard (%d/6)", "conclusion": "success"}' \
        "$((1000 + n))" "${n}"
    done
    printf ']}'
  } >"${dir}/jobs.json"
  n=0
  for outcome in "$@"; do
    n="$((n + 1))"
    {
      printf '##[group]Run actions/cache/restore@v4\n'
      if [[ "${outcome}" == hit ]]; then
        printf 'Cache restored from key: Linux-X64-gobuild-deadbeef\n'
      else
        printf 'Cache not found for input keys: Linux-X64-gobuild-deadbeef\n'
      fi
      # The trailer belongs to a later step, so the census must ignore it:
      # a hit fixture carries a setup-go miss line (the pre-fix double-copy
      # shape) and a miss fixture carries a setup-go restore line. A
      # whole-log grep reads the trailer and misclassifies; the committed
      # section-scoped census must not.
      printf '##[endgroup]\n##[group]Run Set up Go\n'
      if [[ "${outcome}" == hit ]]; then
        printf 'Cache not found for input keys: Linux-X64-setupgo-deadbeef\n'
      else
        printf 'Cache restored from key: Linux-X64-setupgo-deadbeef\n'
      fi
    } >"${dir}/shard${n}.log"
  done
}

census() { python3 -I "$1" --lib "${root}/scripts/lib" "${@:2}"; }

make_run 111 hit hit hit hit hit hit
make_run 222 hit hit hit miss hit hit
make_run 333 miss miss miss miss miss miss

out="$(census "${real}" "111=${work}/111" "222=${work}/222" "333=${work}/333")" \
  || fail "census exits 0 on three readable runs"
has() { [[ "$2" == *"$1"* ]]; }
has "run 111: 6/6 hit => all-hit" "${out}" || fail "all-hit row reads: ${out}"
has "run 222: 5/6 hit => mixed (miss: 4)" "${out}" || fail "mixed row reads: ${out}"
has "run 333: 0/6 hit => all-miss" "${out}" || fail "all-miss row reads: ${out}"
has "census: 3 runs, 1 all-hit, 1 mixed, 1 all-miss; shards 11/18 hit" "${out}" \
  || fail "summary counts runs and shards: ${out}"

# A shard log with neither a hit nor a miss line is refused, not guessed.
mkdir -p "${work}/444"
printf '{"jobs": [{"id": 1001, "name": "live-postgres-readiness (postgres18) shard (1/6)", "conclusion": "success"}]}' \
  >"${work}/444/jobs.json"
printf '##[group]Run actions/cache/restore@v4\nsomething else\n##[group]Run Set up Go\n' \
  >"${work}/444/shard1.log"
if census "${real}" "444=${work}/444" >/dev/null 2>&1; then
  fail "ambiguous restore section must fail closed"
fi

# A shard listed twice is refused, not collapsed to one vote.
mkdir -p "${work}/555"
printf '{"jobs": [{"id": 1001, "name": "live-postgres-readiness (postgres18) shard (1/6)"}, {"id": 1002, "name": "live-postgres-readiness (postgres18) shard (1/6)"}]}' \
  >"${work}/555/jobs.json"
printf '##[group]Run actions/cache/restore@v4\nCache restored from key: Linux-X64-gobuild-deadbeef\n##[group]Run Set up Go\n' \
  >"${work}/555/shard1.log"
if census "${real}" "555=${work}/555" >/dev/null 2>&1; then
  fail "duplicate shard entry must fail closed"
fi

# A non-string job name is refused with exit 2, not a traceback exit 1.
mkdir -p "${work}/666"
printf '{"jobs": [{"id": 1001, "name": 42}]}' >"${work}/666/jobs.json"
code=0
out="$(census "${real}" "666=${work}/666" 2>&1)" || code="$?"
[[ "${code}" -eq 2 ]] || fail "non-string job name must exit 2, got ${code}: ${out}"
[[ "${out}" != *"Traceback"* ]] || fail "non-string job name must not print a traceback"

# A run missing a shard job is refused, not tallied 5/5 all-hit.
mkdir -p "${work}/777"
printf '{"jobs": [%s]}' "$(for n in 1 2 3 4 5; do printf '%s{"id": %d, "name": "live-postgres-readiness (postgres18) shard (%d/6)"}' "$([ "${n}" -gt 1 ] && printf ,)" "$((1000 + n))" "${n}"; done)" \
  >"${work}/777/jobs.json"
for n in 1 2 3 4 5; do
  printf '##[group]Run actions/cache/restore@v4\nCache restored from key: Linux-X64-gobuild-deadbeef\n##[endgroup]\n##[group]Run Set up Go\n' \
    >"${work}/777/shard${n}.log"
done
if census "${real}" "777=${work}/777" >/dev/null 2>&1; then
  fail "partial shard set must fail closed"
fi

# A run mixing shard totals is refused, not tallied 2/2 all-hit.
mkdir -p "${work}/888"
printf '{"jobs": [{"id": 1001, "name": "live-postgres-readiness (postgres18) shard (1/6)"}, {"id": 1002, "name": "live-postgres-readiness (postgres18) shard (2/8)"}]}' \
  >"${work}/888/jobs.json"
for n in 1 2; do
  printf '##[group]Run actions/cache/restore@v4\nCache restored from key: Linux-X64-gobuild-deadbeef\n##[endgroup]\n##[group]Run Set up Go\n' \
    >"${work}/888/shard${n}.log"
done
if census "${real}" "888=${work}/888" >/dev/null 2>&1; then
  fail "mixed shard totals must fail closed"
fi

# Seeded break: a classifier that reads every restore as a hit must fail the
# mixed and all-miss expectations, so the cases above are load-bearing.
mutant="${work}/census-mutant.py"
sed 's/outcome = pool.restore_outcome(log)/outcome = "hit"/' "${real}" >"${mutant}"
if out="$(census "${mutant}" "111=${work}/111" "222=${work}/222" "333=${work}/333" 2>/dev/null)" \
  && has "run 222: 5/6 hit => mixed (miss: 4)" "${out}" \
  && has "run 333: 0/6 hit => all-miss" "${out}"; then
  fail "mutant that always reads hit must not satisfy the mixed/all-miss rows"
fi

# A section-unaware whole-log grep must fail the decoy rows: every fixture
# carries the opposite marker in a later step's section.
greplog="${work}/census-greplog-mutant.py"
sed 's/outcome = pool.restore_outcome(log)/outcome = "hit" if "Cache restored from key:" in log else "miss"/' \
  "${real}" >"${greplog}"
if out="$(census "${greplog}" "111=${work}/111" "222=${work}/222" "333=${work}/333" 2>/dev/null)" \
  && has "run 111: 6/6 hit => all-hit" "${out}" \
  && has "run 222: 5/6 hit => mixed (miss: 4)" "${out}" \
  && has "run 333: 0/6 hit => all-miss" "${out}"; then
  fail "mutant that greps the whole log must not satisfy the decoy rows"
fi

printf 'test-census: PASS\n'
