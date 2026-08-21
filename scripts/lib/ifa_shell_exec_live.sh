#!/usr/bin/env bash
# Shared shell_exec live-gate helpers (#6001), mirroring
# scripts/lib/ifa_codeowners_live.sh's shape exactly. Callers own strict mode
# and cleanup.
#
# SOURCED by both scripts/verify-ifa-determinism.sh and
# scripts/verify-ifa-fault-injection.sh. The family is registered through
# scripts/lib/ifa_family_registry/rows/08_shell_exec.sh, which is what decides
# which cells drive it; this file only supplies the two callbacks that row
# names.

# ifa_shell_exec_drive replays the committed family cassette into one matrix
# cell. The caller performs the aggregate fact_work_items non-vacuity check.
ifa_shell_exec_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	printf '\n=== %s: drive shell-exec family cassette (-workers %s) ===\n' "${label}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-shell-exec-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-shell-exec-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-shell-exec-${label}.log"
}

# ifa_shell_exec_assert rejects an empty, incomplete, duplicated, or spurious
# live materialization. The committed expectation is the two-edge exact set:
# both EXECUTES_SHELL edges come from services/deploy/deploy.py's
# deploy_service, and they are the two distinct (line_number, api) commands
# that survive after an exact-duplicate command collapses via
# ExtractShellExecRows' own edgeKey dedup. That dedup is the reason a
# count-only assertion is not enough here -- a regression that stopped
# collapsing the duplicate would still produce "some edges".
#
# The target uid is the sha256 edge_writer_shell_exec.go's shellExecTargetID
# derives over (repo_id, source_path, function_entity_id, line_number, api),
# so an identity drift in any of those five inputs changes the multiset and
# fails here rather than netting out.
ifa_shell_exec_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert shell-exec materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain shell_exec \
		-expected "${expected_edges}"
}
