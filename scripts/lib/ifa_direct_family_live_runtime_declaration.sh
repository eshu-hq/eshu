#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the runtime_declaration
# DIRECT-materialization family (#6228 runtime_declaration live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh -- always right after
# scripts/lib/ifa_direct_family_live_code_taint_evidence.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits near the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain runtime_declaration` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_runtime_declaration_drive replays the runtime-declaration cassette
# (one scope: the fixture repository carrying the pin-file declarations
# plus the collector's shared_followup projector trigger).
ifa_runtime_declaration_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive runtime-declaration \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_runtime_declaration_assert pins the four-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The four edges are the two tokens on the .tool-versions line (order 0 and
# 1), the .python-version pin, and the web/.nvmrc lts/iron alias -- each
# resolving its RuntimeDeclaration node against the five-part MERGE key the
# shared derivation renders, proving the join resolves rather than
# collapsing to fewer edges. A regression that resolved every declaration
# against the first node would still produce "some edges" and fail only
# against an exact set. The fixture's non-producers -- the tombstoned
# .ruby-version fact, the blank-runtime .go-version fact, and the same-key
# .python-version replay -- must contribute nothing; the writer's MERGE only
# ever produces a node per distinct key, and this set is what holds it to
# that.
#
# The relationship type is DECLARES_RUNTIME, read off the runtime
# declaration writer's MERGE (repo)-[rel:DECLARES_RUNTIME]->(d). It is NOT
# derived from the port or family name.
ifa_runtime_declaration_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert runtime_declaration materialized edges (four-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain runtime_declaration \
		-expected "${expected_edges}"
}
