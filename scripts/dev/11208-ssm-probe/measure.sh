#!/usr/bin/env bash
# Measurement shim for #11208, not a gate. Runs EXPLAIN (ANALYZE, BUFFERS) of
# the SSM host read against a DISPOSABLE postgres:18 container you started
# yourself. It never starts or removes a container. It exits 2, before any
# psql, unless the container name matches ^perf11208- (see test-measure-guard.sh).
#
#   measure.sh <container> seed <nscopes> <nfacts> <naws>
#   measure.sh <container> ssm        # add the SSM rows (ssm_facts.sql)
#   measure.sh <container> index      # add the candidate partial index
#   measure.sh <container> explain <probe_base.sql|probe_c1.sql>
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
container="${1:-}"
action="${2:-}"
if [[ -z "${container}" || -z "${action}" ]]; then
  echo "usage: $0 <container> seed|ssm|index|explain ..." >&2
  exit 2
fi
shift 2

# Refuse any container that is not a perf11208- fixture before psql can run.
# seed.sql drops tables, so a mistyped name must never reach a real database.
if [[ ! "${container}" =~ ^perf11208- ]]; then
  echo "refusing container '${container}': the name must match ^perf11208-" >&2
  exit 2
fi

psql_in() {
  docker exec -i "${container}" psql -U postgres -q -X -At -v ON_ERROR_STOP=1 "$@"
}

case "${action}" in
  seed)
    psql_in -v nscopes="${1:?nscopes}" -v nfacts="${2:?nfacts}" -v naws="${3:?naws}" -f - <"${here}/seed.sql"
    ;;
  ssm)
    psql_in -f - <"${here}/ssm_facts.sql"
    ;;
  index)
    psql_in -f - <"${here}/partial_index.sql"
    ;;
  explain)
    probe="${1:?probe sql file}"
    run() { { echo "EXPLAIN (ANALYZE, BUFFERS)"; cat "${probe}"; } | psql_in; }
    run >/dev/null # warm the cache; every figure below is a warm read
    for i in 1 2 3; do
      plan="$(run)"
      printf 'run %s: ' "${i}"
      printf '%s\n' "${plan}" | rg -m1 '^ *Buffers: shared' | tr -s ' ' | tr '\n' ' '
      printf '%s\n' "${plan}" | rg 'Execution Time'
    done
    printf '%s\n' "${plan}" | rg 'Index Searches|rows=.* loops=.*fact_records|fact_records.*loops'
    ;;
  *)
    echo "unknown action: ${action}" >&2
    exit 2
    ;;
esac
