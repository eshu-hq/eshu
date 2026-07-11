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
	# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
	# the entire heredoc body to a pipe before forking the reader, and macOS's
	# 512-byte pipe buffer deadlocks on any body over that size (#5074).
	cat "${repo_root}/scripts/lib/test-run-scale-benchmark-measurements-runtime-summary.json" >"${file}"
}

write_thresholds() {
	local file="$1"
	# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
	# the entire heredoc body to a pipe before forking the reader, and macOS's
	# 512-byte pipe buffer deadlocks on any body over that size (#5074).
	cat "${repo_root}/scripts/lib/test-run-scale-benchmark-measurements-thresholds.json" >"${file}"
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
