#!/usr/bin/env bash
# Shared inheritance_edges live-gate helpers (#5996), mirroring
# scripts/lib/ifa_codeowners_live.sh's shape exactly. Callers own strict mode
# and cleanup.
#
# SOURCED by both scripts/verify-ifa-determinism.sh and
# scripts/verify-ifa-fault-injection.sh. The family is registered through
# scripts/lib/ifa_family_registry/rows/07_inheritance_edges.sh, which is what
# decides which cells drive it; this file only supplies the two callbacks that
# row names.

# ifa_inheritance_drive replays the committed family cassette into one matrix
# cell. The caller performs the aggregate fact_work_items non-vacuity check.
ifa_inheritance_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	printf '\n=== %s: drive inheritance family cassette (-workers %s) ===\n' "${label}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-inheritance-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-inheritance-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-inheritance-${label}.log"
}

# ifa_inheritance_assert rejects an empty, incomplete, duplicated, or spurious
# live materialization. The committed expectation is the five-edge exact set
# covering all four relationship types this family writes: INHERITS from a
# declared base, IMPLEMENTS from an implemented interface, OVERRIDES from a
# PHP-style `insteadof` trait adaptation, and TWO ALIASES edges from a single
# `as` adaptation -- one class-to-trait and one method-to-method, which carry
# different child ids so neither can be masked by the other's dedup key.
#
# Four types across two writer files: canonical_inheritance_edges.go holds the
# INHERITS/OVERRIDES/ALIASES templates and canonical_implements_edges.go holds
# IMPLEMENTS. A count-only assertion would pass on INHERITS alone and undercount
# the family fourfold, which is why this is an exact multiset assertion over the
# committed expected-edge file rather than a threshold.
ifa_inheritance_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert inheritance materialized edges (five-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain inheritance_edges \
		-expected "${expected_edges}"
}
