#!/usr/bin/env bash
# Shared code_calls live-gate helpers (#5991). This file is sourced by the
# determinism and fault-injection drivers; callers own strict mode and cleanup.

# ifa_code_call_drive replays the committed family cassette into one matrix
# cell. The caller performs the aggregate fact_work_items non-vacuity check.
ifa_code_call_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	printf '\n=== %s: drive code-call family cassette (-workers %s) ===\n' "${label}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-code-calls-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-code-calls-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-code-calls-${label}.log"
}

# ifa_code_call_assert rejects an empty, incomplete, duplicated, or spurious
# live materialization. The committed expectation is the five-edge exact set
# covering all four code-call writer relationship types.
ifa_code_call_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert code-call materialized edges (five-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain code_calls \
		-expected "${expected_edges}"
}
