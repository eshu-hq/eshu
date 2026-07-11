#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-scale-benchmark-artifact.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

write_valid_artifact() {
	local file="$1"
	# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
	# the entire heredoc body to a pipe before forking the reader, and
	# macOS's 512-byte pipe buffer deadlocks on any body over that size
	# (#5074).
	cat "${repo_root}/scripts/lib/test-verify-scale-benchmark-artifact-valid.json" >"${file}"
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

numeric_commit="${tmp_root}/numeric-commit.json"
jq '
	.run.commit = "123456789012abcdefabcdefabcdefabcdefabcd" |
	.comparison.optimization_claimed = true |
	.comparison.baseline_commit = "210987654321abcdefabcdefabcdefabcdefabcd" |
	.comparison.baseline_artifact = "scale-benchmark-baseline" |
	.comparison.result = "no_regression"
' "${valid}" >"${numeric_commit}"
expect_pass "${numeric_commit}"

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

sample_artifact="${repo_root}/specs/scale-benchmark-artifact.sample.json"
expect_pass "${sample_artifact}"

printf 'verify-scale-benchmark-artifact tests passed\n'
