#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
collector="${repo_root}/scripts/run-scale-benchmark-measurements.sh"
producer="${repo_root}/scripts/run-scale-benchmark-artifact.sh"
verifier="${repo_root}/scripts/verify-scale-benchmark-artifact.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

write_summary() {
	local file="$1"
	cat >"${file}" <<'JSON'
{
  "ingestion": {"fact_rows": 24000, "elapsed_seconds": 20},
  "queue": {
    "claim_latency_ms_samples": [9, 11, 12, 13, 14, 17, 18, 21, 23, 25, 27, 29, 31, 35, 37, 40, 42, 45, 47, 50],
    "retry_count": 0,
    "dead_letter_count": 0
  },
  "reducer": {"drain_seconds": 120},
  "graph": {"write_latency_ms_samples": [40, 44, 48, 52, 56, 60, 64, 68, 72, 76, 80, 82, 84, 86, 88, 90, 92, 94, 96, 98]},
  "api": {"latency_ms_samples": [55, 58, 60, 63, 67, 70, 73, 77, 80, 84, 88, 91, 94, 97, 101, 106, 112, 119, 130, 141]},
  "mcp": {"latency_ms_samples": [57, 61, 64, 69, 72, 76, 79, 83, 86, 90, 94, 99, 103, 108, 112, 117, 121, 126, 132, 145]},
  "runtime": {"memory_high_water_mb": 512},
  "backend_matrix": {
    "nornicdb": {"status": "pass", "artifact": "scale-benchmark-nornicdb"}
  },
  "observability": {
    "pprof_status": "pass",
    "logs_status": "pass",
    "resource_snapshot_status": "pass"
  }
}
JSON
}

write_thresholds() {
	local file="$1"
	cat >"${file}" <<'JSON'
{
  "metrics": {
    "fact_rows_per_second": {"threshold": 1000, "direction": "min"},
    "queue_claim_latency_p95_ms": {"threshold": 55, "direction": "max"},
    "reducer_drain_seconds": {"threshold": 180, "direction": "max"},
    "graph_write_p95_ms": {"threshold": 100, "direction": "max"},
    "api_p95_ms": {"threshold": 150, "direction": "max"},
    "mcp_p95_ms": {"threshold": 150, "direction": "max"},
    "retry_count": {"threshold": 0, "direction": "max"},
    "dead_letter_count": {"threshold": 0, "direction": "max"},
    "memory_high_water_mb": {"threshold": 1024, "direction": "max"}
  }
}
JSON
}

expect_fail() {
	local expected="$1"
	shift
	if "$@" >/tmp/eshu-scale-measurements.out 2>/tmp/eshu-scale-measurements.err; then
		printf 'expected command to fail: %s\n' "$*" >&2
		exit 1
	fi
	if ! rg --fixed-strings --quiet -- "${expected}" /tmp/eshu-scale-measurements.err; then
		printf 'expected failure containing "%s"\n' "${expected}" >&2
		sed -n '1,160p' /tmp/eshu-scale-measurements.err >&2
		exit 1
	fi
}

summary="${tmp_root}/runtime-summary.json"
measurements="${tmp_root}/measurements.json"
thresholds="${tmp_root}/thresholds.json"
artifact="${tmp_root}/artifact.json"
write_summary "${summary}"
write_thresholds "${thresholds}"

"${collector}" --summary "${summary}" --measurements "${measurements}"

jq -e '
  .metrics.fact_rows_per_second == 1200 and
  .metrics.queue_claim_latency_p95_ms == 47 and
  .metrics.graph_write_p95_ms == 96 and
  .metrics.api_p95_ms == 130 and
  .metrics.mcp_p95_ms == 132 and
  .metrics.retry_count == 0 and
  .metrics.dead_letter_count == 0 and
  .metrics.memory_high_water_mb == 512 and
  .backend_matrix.nornicdb.status == "pass" and
  .observability.pprof_status == "pass"
' "${measurements}" >/dev/null

"${producer}" \
	--artifact "${artifact}" \
	--measurements "${measurements}" \
	--thresholds "${thresholds}" \
	--run-kind baseline \
	--gate remote-compose \
	--commit 123456789012abcdefabcdefabcdefabcdefabcd \
	--backend-kind nornicdb \
	--backend-version fixture-v1 \
	--corpus-mode representative \
	--corpus-slot medium/representative_20_50 \
	--repository-count 24 \
	--compatibility-status unsupported \
	--compatibility-reason "not configured for this public proof" \
	--verify >/tmp/eshu-scale-measurements-producer.out
"${verifier}" --artifact "${artifact}" >/tmp/eshu-scale-measurements-verify.out

missing_api="${tmp_root}/missing-api.json"
jq 'del(.api.latency_ms_samples)' "${summary}" >"${missing_api}"
expect_fail "missing required non-negative numeric sample array: api.latency_ms_samples" \
	"${collector}" --summary "${missing_api}" --measurements "${tmp_root}/missing-api-measurements.json"

negative_latency="${tmp_root}/negative-latency.json"
jq '.api.latency_ms_samples = [-5]' "${summary}" >"${negative_latency}"
expect_fail "missing required non-negative numeric sample array: api.latency_ms_samples" \
	"${collector}" --summary "${negative_latency}" --measurements "${tmp_root}/negative-latency-measurements.json"

zero_elapsed="${tmp_root}/zero-elapsed.json"
jq '.ingestion.elapsed_seconds = 0' "${summary}" >"${zero_elapsed}"
expect_fail "ingestion.elapsed_seconds must be greater than zero" \
	"${collector}" --summary "${zero_elapsed}" --measurements "${tmp_root}/zero-elapsed-measurements.json"

private_summary="${tmp_root}/private-summary.json"
jq '.runtime.note = ("https" + "://example.invalid")' "${summary}" >"${private_summary}"
expect_fail "summary looks like private data" \
	"${collector}" --summary "${private_summary}" --measurements "${tmp_root}/private-measurements.json"

private_key_summary="${tmp_root}/private-key-summary.json"
jq '.backend_matrix[("https" + "://example.invalid")] = {"status": "pass", "artifact": "bad"}' \
	"${summary}" >"${private_key_summary}"
expect_fail "summary looks like private data" \
	"${collector}" --summary "${private_key_summary}" --measurements "${tmp_root}/private-key-measurements.json"

printf 'run-scale-benchmark-measurements tests passed\n'
