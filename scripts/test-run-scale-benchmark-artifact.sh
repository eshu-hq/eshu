#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
producer="${repo_root}/scripts/run-scale-benchmark-artifact.sh"
verifier="${repo_root}/scripts/verify-scale-benchmark-artifact.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

write_measurements() {
	local file="$1"
	cat >"${file}" <<'JSON'
{
  "metrics": {
    "fact_rows_per_second": 1200,
    "queue_claim_latency_p95_ms": 40,
    "reducer_drain_seconds": 120,
    "graph_write_p95_ms": 65,
    "api_p95_ms": 90,
    "mcp_p95_ms": 95,
    "retry_count": 0,
    "dead_letter_count": 0,
    "memory_high_water_mb": 512
  },
  "backend_matrix": {
    "nornicdb": {
      "status": "pass",
      "artifact": "scale-benchmark-nornicdb"
    }
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
    "queue_claim_latency_p95_ms": {"threshold": 50, "direction": "max"},
    "reducer_drain_seconds": {"threshold": 180, "direction": "max"},
    "graph_write_p95_ms": {"threshold": 80, "direction": "max"},
    "api_p95_ms": {"threshold": 150, "direction": "max"},
    "mcp_p95_ms": {"threshold": 150, "direction": "max"},
    "retry_count": {"threshold": 0, "direction": "max"},
    "dead_letter_count": {"threshold": 0, "direction": "max"},
    "memory_high_water_mb": {"threshold": 1024, "direction": "max"}
  }
}
JSON
}

run_producer() {
	local artifact="$1"
	local measurements="$2"
	local thresholds="$3"
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
		--verify
}

expect_fail() {
	local expected="$1"
	shift
	if "$@" >/tmp/eshu-scale-producer.out 2>/tmp/eshu-scale-producer.err; then
		printf 'expected command to fail: %s\n' "$*" >&2
		exit 1
	fi
	if ! rg --fixed-strings --quiet -- "${expected}" /tmp/eshu-scale-producer.err; then
		printf 'expected failure containing "%s"\n' "${expected}" >&2
		sed -n '1,160p' /tmp/eshu-scale-producer.err >&2
		exit 1
	fi
}

measurements="${tmp_root}/measurements.json"
thresholds="${tmp_root}/thresholds.json"
artifact="${tmp_root}/scale-benchmark-artifact.json"
write_measurements "${measurements}"
write_thresholds "${thresholds}"

run_producer "${artifact}" "${measurements}" "${thresholds}"
"${verifier}" --artifact "${artifact}" >/tmp/eshu-scale-producer-verify.out

jq -e '
  .status == "pass" and
  .run.commit == "123456789012abcdefabcdefabcdefabcdefabcd" and
  .run.metadata_recorded == true and
  .metrics.fact_rows_per_second.threshold_result == "pass" and
  .backend_matrix.compatibility.status == "unsupported"
' "${artifact}" >/dev/null

regressed_measurements="${tmp_root}/regressed-measurements.json"
jq '.metrics.api_p95_ms = 900' "${measurements}" >"${regressed_measurements}"
regressed_artifact="${tmp_root}/scale-benchmark-regressed.json"
run_producer "${regressed_artifact}" "${regressed_measurements}" "${thresholds}"
jq -e '
  .status == "fail" and
  .metrics.api_p95_ms.threshold_result == "fail"
' "${regressed_artifact}" >/dev/null
"${verifier}" --artifact "${regressed_artifact}" >/tmp/eshu-scale-producer-regressed-verify.out

retry_measurements="${tmp_root}/retry-measurements.json"
jq '.metrics.retry_count = 1' "${measurements}" >"${retry_measurements}"
loose_retry_thresholds="${tmp_root}/loose-retry-thresholds.json"
jq '.metrics.retry_count.threshold = 5' "${thresholds}" >"${loose_retry_thresholds}"
retry_artifact="${tmp_root}/scale-benchmark-retry.json"
run_producer "${retry_artifact}" "${retry_measurements}" "${loose_retry_thresholds}"
jq -e '
  .status == "fail" and
  .metrics.retry_count.threshold_result == "pass"
' "${retry_artifact}" >/dev/null
"${verifier}" --artifact "${retry_artifact}" >/tmp/eshu-scale-producer-retry-verify.out

missing_metric="${tmp_root}/missing-metric.json"
jq 'del(.metrics.mcp_p95_ms)' "${measurements}" >"${missing_metric}"
expect_fail "missing required measurement: metrics.mcp_p95_ms" \
	run_producer "${tmp_root}/missing-artifact.json" "${missing_metric}" "${thresholds}"

private_measurements="${tmp_root}/private-measurements.json"
jq '.run_id = ("scale-bench-" + "https" + "://private.example.invalid")' \
	"${measurements}" >"${private_measurements}"
expect_fail "measurements looks like private data" \
	run_producer "${tmp_root}/private-artifact.json" "${private_measurements}" "${thresholds}"

private_handle="https"'://private.example.invalid/results.json'
expect_fail "artifact handle looks like private data" \
	"${producer}" \
		--artifact "${tmp_root}/private-handle.json" \
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
		--results-handle "${private_handle}" \
		--verify

expect_fail "artifact handle looks like private data" \
	"${producer}" \
		--artifact "${tmp_root}/private-baseline-artifact.json" \
		--measurements "${measurements}" \
		--thresholds "${thresholds}" \
		--run-kind after \
		--gate remote-compose \
		--commit 123456789012abcdefabcdefabcdefabcdefabcd \
		--backend-kind nornicdb \
		--backend-version fixture-v1 \
		--corpus-mode representative \
		--corpus-slot medium/representative_20_50 \
		--repository-count 24 \
		--compatibility-status unsupported \
		--compatibility-reason "not configured for this public proof" \
		--optimization-claimed true \
		--baseline-commit 210987654321abcdefabcdefabcdefabcdefabcd \
		--baseline-artifact "team/private-baseline" \
		--comparison-result no_regression \
		--verify

expect_fail "run.commit must be a 40-character lowercase commit SHA" \
	"${producer}" \
		--artifact "${tmp_root}/bad-commit.json" \
		--measurements "${measurements}" \
		--thresholds "${thresholds}" \
		--run-kind baseline \
		--gate remote-compose \
		--commit short \
		--backend-kind nornicdb \
		--backend-version fixture-v1 \
		--corpus-mode representative \
		--corpus-slot medium/representative_20_50 \
		--repository-count 24 \
		--compatibility-status unsupported \
		--compatibility-reason "not configured for this public proof" \
		--verify

printf 'run-scale-benchmark-artifact tests passed\n'
