#!/usr/bin/env bash
# D4.4 live proof for the Wave-6 core serving binary (Refs eshu-hq/eshu#9669).
#
# Boots the freshly built eshu-core-api against live Postgres + Neo4j and
# demonstrates, in order:
#   leg 1  /readyz 200 with both backends up
#   leg 2  /readyz 503 (no Retry-After) with Postgres down, then 200 restored
#   leg 3  /readyz 503 (no Retry-After) with Neo4j down, then 200 restored
#   leg 4  satellite at pin 08ebde4 with the new binary as --core-base-url:
#          byte-identical relay comparisons (surface-inventory 200=200,
#          slash-twin 404=404, freshness 400=400, derive POST 200=200)
#   leg 5  satellite delegated /readyz 503 (no Retry-After) vs dead core,
#          then 200 on restore
#   leg 6  latency pins: the core readiness probe stays an order of
#          magnitude below the satellite 30 s relay bound, and the
#          satellite suite's own dispatchTimeout == readclient.DefaultTimeout
#          test passes at the pin.
#
# Prereqs: docker with the wave6-pg / wave6-neo4j demo containers (created
# with the pinned CI images when missing, schema via ApplyBootstrap through
# eshu-bootstrap-data-plane), go (repo-pinned toolchain), curl, openssl,
# and a checkout of eshu-hq/eshu-api at SATELLITE_PIN (default: the
# ../eshu-api sibling; falls back to a scratch clone when network allows).
#
# Usage: scripts/prove-core-api-live.sh [--keep]
#   --keep  leave the proof processes running and print their addresses.
#
# Env overrides: CORE_ADDR (default 127.0.0.1:18091),
# SAT_ADDR (default 127.0.0.1:18092), ESHU_POSTGRES_DSN, ESHU_NEO4J_URI,
# ESHU_NEO4J_USERNAME, ESHU_NEO4J_PASSWORD, ESHU_NEO4J_DATABASE,
# SATELLITE_SRC, SATELLITE_BIN, CORE_BIN.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
keep=0
if [[ "${1:-}" == "--keep" ]]; then keep=1; fi

CORE_ADDR="${CORE_ADDR:-127.0.0.1:18091}"
SAT_ADDR="${SAT_ADDR:-127.0.0.1:18092}"
SATELLITE_PIN="${SATELLITE_PIN:-08ebde4}"
SATELLITE_SRC="${SATELLITE_SRC:-$(cd "${repo_root}/../eshu-api" 2>/dev/null && pwd || true)}"
PG_CONTAINER="${PG_CONTAINER:-wave6-pg}"
NEO_CONTAINER="${NEO_CONTAINER:-wave6-neo4j}"
PG_IMAGE='ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
NEO_IMAGE='ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'

export ESHU_POSTGRES_DSN="${ESHU_POSTGRES_DSN:-postgres://eshu:eshu-demo-pw@127.0.0.1:15433/eshu?sslmode=disable}"
export ESHU_GRAPH_BACKEND=neo4j
export ESHU_NEO4J_URI="${ESHU_NEO4J_URI:-bolt://127.0.0.1:17687}"
export ESHU_NEO4J_USERNAME="${ESHU_NEO4J_USERNAME:-neo4j}"
export ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD:-wave6-demo-pw}"
export ESHU_NEO4J_DATABASE="${ESHU_NEO4J_DATABASE:-neo4j}"
export ESHU_AUTH_BOOTSTRAP_MODE=disabled
CORE_KEY="$(openssl rand -hex 16)"
SAT_KEY="$(openssl rand -hex 16)"
export ESHU_API_KEY="${CORE_KEY}"

scratch="$(mktemp -d "${TMPDIR:-/tmp}/d44-proof.XXXXXX")"
logdir="${scratch}/logs"
mkdir -p "${logdir}"
export ESHU_HOME="${scratch}/eshu-home"
mkdir -p "${ESHU_HOME}"
core_pid=""; sat_pid=""

fail() { printf 'D44-FAIL leg %s: %s\n' "$1" "$2" >&2; exit 1; }
pass() { printf 'D44-PASS leg %s: %s\n' "$1" "$2"; }
cleanup() {
  if [[ "${keep}" -eq 1 ]]; then
    printf 'D44-KEEP core=%s sat=%s scratch=%s\n' "${CORE_ADDR}" "${SAT_ADDR}" "${scratch}"
    return
  fi
  [[ -n "${sat_pid}" ]] && kill "${sat_pid}" 2>/dev/null || true
  [[ -n "${core_pid}" ]] && kill "${core_pid}" 2>/dev/null || true
}
trap cleanup EXIT

need() { command -v "$1" >/dev/null 2>&1 || fail setup "missing required command: $1"; }
need docker; need curl; need openssl; need go

# Leg 0: backends exist (recreate with pinned images + ApplyBootstrap when missing).
if ! docker inspect "${PG_CONTAINER}" >/dev/null 2>&1; then
  docker run -d --name "${PG_CONTAINER}" -e POSTGRES_USER=eshu \
    -e POSTGRES_PASSWORD=eshu-demo-pw -e POSTGRES_DB=eshu \
    -p 15433:5432 "${PG_IMAGE}" >/dev/null || fail 0 "cannot create ${PG_CONTAINER}"
fi
if ! docker inspect "${NEO_CONTAINER}" >/dev/null 2>&1; then
  docker run -d --name "${NEO_CONTAINER}" -e NEO4J_AUTH=neo4j/wave6-demo-pw \
    -p 17474:7474 -p 17687:7687 "${NEO_IMAGE}" >/dev/null || fail 0 "cannot create ${NEO_CONTAINER}"
fi
docker start "${PG_CONTAINER}" >/dev/null 2>&1 || true
docker start "${NEO_CONTAINER}" >/dev/null 2>&1 || true
# Schema on a recreated (empty) Postgres: the binary only seeds rows, so
# apply the embedded migrations through the repo's own bootstrap binary.
if [[ "$(docker exec "${PG_CONTAINER}" psql -U eshu -d eshu -tc "select count(*) from pg_tables where schemaname='public';" 2>/dev/null | tr -d ' ')" == "0" ]]; then
  (cd "${repo_root}/go" && go run ./cmd/bootstrap-data-plane) >>"${logdir}/bootstrap.log" 2>&1 \
    || fail 0 "bootstrap-data-plane failed (see ${logdir}/bootstrap.log)"
fi
pass 0 "backends up: ${PG_CONTAINER} + ${NEO_CONTAINER}"

# Leg 0b: binaries (fresh builds unless overridden).
CORE_BIN="${CORE_BIN:-${scratch}/eshu-core-api}"
if [[ ! -x "${CORE_BIN}" ]]; then
  (cd "${repo_root}/go" && go build -o "${CORE_BIN}" ./cmd/core-api) || fail 0 "core build failed"
fi
if [[ -z "${SATELLITE_SRC}" ]]; then
  SATELLITE_SRC="${scratch}/eshu-api-pin"
  git clone -q https://github.com/eshu-hq/eshu-api.git "${SATELLITE_SRC}" || fail 0 "satellite clone failed"
  git -C "${SATELLITE_SRC}" checkout -q "${SATELLITE_PIN}" || fail 0 "satellite pin checkout failed"
fi
[[ "$(git -C "${SATELLITE_SRC}" rev-parse HEAD)" == "${SATELLITE_PIN}"* ]] \
  || fail 0 "satellite at $(git -C "${SATELLITE_SRC}" rev-parse --short HEAD), want ${SATELLITE_PIN}"
SATELLITE_BIN="${SATELLITE_BIN:-${scratch}/eshu-api}"
if [[ ! -x "${SATELLITE_BIN}" ]]; then
  (cd "${SATELLITE_SRC}" && go build -o "${SATELLITE_BIN}" ./cmd/eshu-api) || fail 0 "satellite build failed"
fi
pass 0 "binaries built: core + satellite@${SATELLITE_PIN}"

export ESHU_AUTH_SECRET_ENC_KEY="$(openssl rand -base64 32)"
wait_code() { # addr want tries
  local code=""
  for _ in $(seq 1 "$3"); do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 "http://$1/readyz" || true)"
    [[ "${code}" == "$2" ]] && break
    sleep 3
  done
  [[ "${code}" == "$2" ]] || fail "$4" "/readyz settled at ${code}, want $2"
  printf '%s' "${code}"
}
no_retry_after() { # addr leg
  if curl -s -D - -o /dev/null --max-time 8 "http://$1/readyz" | rg -qi '^retry-after:'; then
    fail "$2" "/readyz carries Retry-After on a permanent 503"
  fi
}

# Leg 1: both backends up -> 200.
# The DEK below is a per-run throwaway for the demo backends; the proof
# exercises no provider secrets (writes fail closed without real ones).
(setsid -f "${CORE_BIN}" --addr "${CORE_ADDR}" </dev/null >>"${logdir}/core.log" 2>&1)
sleep 1
core_pid="$(pgrep -f "${CORE_BIN} --addr ${CORE_ADDR}" | head -1 || true)"
wait_code "${CORE_ADDR}" 200 20 1 >/dev/null
pass 1 "/readyz 200 with both backends up"

# Leg 2: Postgres down -> 503 without Retry-After; restore -> 200.
docker stop "${PG_CONTAINER}" >/dev/null || true
wait_code "${CORE_ADDR}" 503 15 2 >/dev/null
no_retry_after "${CORE_ADDR}" 2
docker start "${PG_CONTAINER}" >/dev/null
pass 2 "/readyz 503 (no Retry-After) with Postgres down"
wait_code "${CORE_ADDR}" 200 20 2b >/dev/null
pass 2b "/readyz 200 with Postgres restored"

# Leg 3: Neo4j down -> 503 without Retry-After; restore -> 200.
docker stop "${NEO_CONTAINER}" >/dev/null || true
wait_code "${CORE_ADDR}" 503 15 3 >/dev/null
no_retry_after "${CORE_ADDR}" 3
docker start "${NEO_CONTAINER}" >/dev/null
pass 3 "/readyz 503 (no Retry-After) with Neo4j down"
wait_code "${CORE_ADDR}" 200 25 3b >/dev/null
pass 3b "/readyz 200 with Neo4j restored"

# Leg 4: satellite relay parity, byte-identical (caller presents the CORE
# key, which is foreign to the satellite gate, so every leg relays).
ESHU_API_KEY="${SAT_KEY}" setsid -f "${SATELLITE_BIN}" --addr "${SAT_ADDR}" \
  --core-base-url="http://${CORE_ADDR}" </dev/null >>"${logdir}/satellite.log" 2>&1
sleep 1
sat_pid="$(pgrep -f "${SATELLITE_BIN} --addr ${SAT_ADDR}" | head -1 || true)"
wait_code "${SAT_ADDR}" 200 20 4 >/dev/null
AUTH="Authorization: Bearer ${CORE_KEY}"
compare() { # desc method path [body]
  local desc="$1" method="$2" path="$3" body="${4:-}" sc dc
  if [[ "${method}" == POST ]]; then
    sc="$(curl -s -o "${scratch}/sat.body" -w '%{http_code}' -X POST -H "${AUTH}" \
      -H 'Content-Type: application/json' -d "${body}" "http://${SAT_ADDR}${path}")"
    dc="$(curl -s -o "${scratch}/core.body" -w '%{http_code}' -X POST -H "${AUTH}" \
      -H 'Content-Type: application/json' -d "${body}" "http://${CORE_ADDR}${path}")"
  else
    sc="$(curl -s -o "${scratch}/sat.body" -w '%{http_code}' -H "${AUTH}" "http://${SAT_ADDR}${path}")"
    dc="$(curl -s -o "${scratch}/core.body" -w '%{http_code}' -H "${AUTH}" "http://${CORE_ADDR}${path}")"
  fi
  [[ "${sc}" == "${dc}" ]] || fail 4 "${desc}: status sat=${sc} core=${dc}"
  cmp -s "${scratch}/sat.body" "${scratch}/core.body" || fail 4 "${desc}: bodies differ"
  pass 4 "${desc} byte-identical sat=${sc} core=${dc}"
}
compare 'surface-inventory 200' GET '/api/v0/surface-inventory'
compare 'slash-twin 404' GET '/api/v0/surface-inventory/'
compare 'freshness 400' GET '/api/v0/freshness/changed-since'
compare 'derive POST 200' POST '/api/v0/visualizations/derive' '{"view":"service_story","source_response":{}}'
rg -q '"via":"relay"' "${logdir}/satellite.log" || fail 4 "no relay-lane rows in satellite log"

# Leg 5: satellite delegated /readyz vs dead core -> 503 without
# Retry-After; restore -> 200 on both.
kill "${core_pid}"; core_pid=""; sleep 2
wait_code "${SAT_ADDR}" 503 15 5 >/dev/null
if curl -s -D - -o /dev/null --max-time 8 "http://${SAT_ADDR}/readyz" | rg -qi '^retry-after:'; then
  fail 5 "satellite /readyz carries Retry-After on a permanent 503"
fi
pass 5 "satellite /readyz 503 (no Retry-After) vs dead core"
(setsid -f "${CORE_BIN}" --addr "${CORE_ADDR}" </dev/null >>"${logdir}/core.log" 2>&1)
sleep 1
core_pid="$(pgrep -f "${CORE_BIN} --addr ${CORE_ADDR}" | head -1 || true)"
wait_code "${CORE_ADDR}" 200 20 5b >/dev/null
wait_code "${SAT_ADDR}" 200 20 5c >/dev/null
pass 5b "core + satellite /readyz 200 restored"

# Leg 6: latency pins.
(cd "${repo_root}/go" && go test ./internal/query/ \
  -run 'TestCoreReadinessProbeStaysFarBelowRelayBound' -count=1) >>"${logdir}/pins.log" 2>&1 \
  || fail 6 "core latency pin test failed"
(cd "${SATELLITE_SRC}" && go test ./api/ \
  -run 'TestDispatch_TimeoutMatchesReadContract' -count=1) >>"${logdir}/pins.log" 2>&1 \
  || fail 6 "satellite dispatchTimeout equality test failed"
pass 6 "latency pins hold (core probe << 30 s relay bound; satellite dispatch == readclient)"

printf 'D44-DONE all legs pass; logs in %s\n' "${logdir}"
