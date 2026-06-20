#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-scale-benchmark-artifact.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

write_valid_artifact() {
	local file="$1"
	cat >"${file}" <<'JSON'
{
  "schema_version": "scale-benchmark-artifact/v1",
  "status": "pass",
  "run": {
    "id": "scale-bench-20260620T000000Z",
    "kind": "baseline",
    "commit": "0123456789abcdef0123456789abcdef01234567",
    "issue": 3171,
    "gate": "remote-compose",
    "metadata_recorded": true,
    "backend": {
      "kind": "nornicdb",
      "version": "fixture-v1"
    }
  },
  "corpus": {
    "contract_version": "scale-lab-corpus/v1",
    "slot": "medium/representative_20_50",
    "mode": "representative",
    "repository_count": 24,
    "privacy_status": "public_safe"
  },
  "backend_matrix": {
    "nornicdb": {
      "status": "pass",
      "artifact": "scale-bench-nornicdb"
    },
    "compatibility": {
      "status": "unsupported",
      "reason": "not configured for this public proof"
    }
  },
  "comparison": {
    "optimization_claimed": false,
    "baseline_commit": null,
    "baseline_artifact": null,
    "result": "not_applicable"
  },
  "artifacts": {
    "results": "scale-benchmark-results.json",
    "thresholds": "scale-benchmark-thresholds.json",
    "pprof": "scale-benchmark-pprof.txt",
    "logs": "scale-benchmark-logs.txt"
  },
  "metrics": {
    "fact_rows_per_second": {"stage": "ingestion", "unit": "rows_per_second", "value": 1200, "threshold": 1000, "threshold_result": "pass"},
    "queue_claim_latency_p95_ms": {"stage": "queue", "unit": "milliseconds", "value": 40, "threshold": 50, "threshold_result": "pass"},
    "reducer_drain_seconds": {"stage": "reducer", "unit": "seconds", "value": 120, "threshold": 180, "threshold_result": "pass"},
    "graph_write_p95_ms": {"stage": "graph", "unit": "milliseconds", "value": 65, "threshold": 80, "threshold_result": "pass"},
    "api_p95_ms": {"stage": "api", "unit": "milliseconds", "value": 90, "threshold": 150, "threshold_result": "pass"},
    "mcp_p95_ms": {"stage": "mcp", "unit": "milliseconds", "value": 95, "threshold": 150, "threshold_result": "pass"},
    "retry_count": {"stage": "queue", "unit": "count", "value": 0, "threshold": 0, "threshold_result": "pass"},
    "dead_letter_count": {"stage": "queue", "unit": "count", "value": 0, "threshold": 0, "threshold_result": "pass"},
    "memory_high_water_mb": {"stage": "runtime", "unit": "mebibytes", "value": 512, "threshold": 1024, "threshold_result": "pass"}
  },
  "observability": {
    "pprof_status": "pass",
    "logs_status": "pass",
    "resource_snapshot_status": "pass"
  },
  "privacy": {
    "status": "pass"
  }
}
JSON
}

run_verifier() {
	local artifact="$1"
	"${verifier}" --artifact "${artifact}" >/tmp/eshu-scale-benchmark.out 2>/tmp/eshu-scale-benchmark.err
}

expect_pass() {
	local artifact="$1"
	if ! run_verifier "${artifact}"; then
		printf 'expected verifier to pass for %s\n' "${artifact}" >&2
		sed -n '1,120p' /tmp/eshu-scale-benchmark.err >&2
		exit 1
	fi
}

expect_fail() {
	local artifact="$1"
	local reason="$2"
	if run_verifier "${artifact}"; then
		printf 'expected verifier to fail for %s\n' "${artifact}" >&2
		exit 1
	fi
	if ! rg --fixed-strings --quiet -- "${reason}" /tmp/eshu-scale-benchmark.err; then
		printf 'expected failure reason "%s" for %s\n' "${reason}" "${artifact}" >&2
		sed -n '1,120p' /tmp/eshu-scale-benchmark.err >&2
		exit 1
	fi
}

valid="${tmp_root}/valid.json"
write_valid_artifact "${valid}"
expect_pass "${valid}"

missing_commit="${tmp_root}/missing-commit.json"
jq 'del(.run.commit)' "${valid}" >"${missing_commit}"
expect_fail "${missing_commit}" "missing required benchmark evidence: run.commit"

missing_metric="${tmp_root}/missing-metric.json"
jq 'del(.metrics.api_p95_ms)' "${valid}" >"${missing_metric}"
expect_fail "${missing_metric}" "missing required benchmark evidence: metrics.api_p95_ms"

missing_threshold="${tmp_root}/missing-threshold.json"
jq 'del(.metrics.graph_write_p95_ms.threshold)' "${valid}" >"${missing_threshold}"
expect_fail "${missing_threshold}" "metrics.graph_write_p95_ms requires numeric value and threshold"

missing_backend_reason="${tmp_root}/missing-backend-reason.json"
jq 'del(.backend_matrix.compatibility.reason)' "${valid}" >"${missing_backend_reason}"
expect_fail "${missing_backend_reason}" "backend_matrix.compatibility unsupported requires reason"

pass_with_backend_fail="${tmp_root}/pass-with-backend-fail.json"
jq '.backend_matrix.nornicdb.status = "fail"' "${valid}" >"${pass_with_backend_fail}"
expect_fail "${pass_with_backend_fail}" "status pass cannot include failing backend nornicdb"

pass_with_nornicdb_skipped="${tmp_root}/pass-with-nornicdb-skipped.json"
jq '.backend_matrix.nornicdb = {"status": "skipped", "reason": "not measured"}' \
	"${valid}" >"${pass_with_nornicdb_skipped}"
expect_fail "${pass_with_nornicdb_skipped}" "status pass requires nornicdb backend evidence to pass"

empty_artifact_handle="${tmp_root}/empty-artifact-handle.json"
jq '.artifacts.pprof = ""' "${valid}" >"${empty_artifact_handle}"
expect_fail "${empty_artifact_handle}" "artifacts.pprof must be a non-empty string"

pass_with_observability_fail="${tmp_root}/pass-with-observability-fail.json"
jq '.observability.logs_status = "fail"' "${valid}" >"${pass_with_observability_fail}"
expect_fail "${pass_with_observability_fail}" "status pass cannot include failing observability logs_status"

string_optimization_flag="${tmp_root}/string-optimization-flag.json"
jq '.comparison.optimization_claimed = "false"' "${valid}" >"${string_optimization_flag}"
expect_fail "${string_optimization_flag}" "comparison.optimization_claimed must be boolean"

optimization_without_baseline="${tmp_root}/optimization-without-baseline.json"
jq '.comparison.optimization_claimed = true | .comparison.result = "improved"' \
	"${valid}" >"${optimization_without_baseline}"
expect_fail "${optimization_without_baseline}" "optimization claims require before/after evidence"

private_value="${tmp_root}/private-value.json"
jq '.run.id = ("scale-bench-" + "https" + "://private.example.invalid")' \
	"${valid}" >"${private_value}"
expect_fail "${private_value}" "artifact looks like private data"

printf 'verify-scale-benchmark-artifact tests passed\n'
