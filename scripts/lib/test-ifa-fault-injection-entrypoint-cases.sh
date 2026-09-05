#!/usr/bin/env bash
# shellcheck disable=SC2154
# Entrypoint, lifecycle, and port-isolation source locks for the IFA fault gate.
# Sourced by scripts/test-verify-ifa-fault-injection.sh.

run_ifa_fault_entrypoint_static_cases() {
	require_source_inventory() {
		local label="$1"
		local needle="$2"
		[[ "$(_ifa_count_code_matches "${needle}" "${sources_lib}")" -ge 1 ]] \
			|| fail "${label}"
	}

	# Strict mode, self-cleanup, and the masking-safe bash>=4.4 guard.
	require_line "strict mode" "set -euo pipefail"
	require_code "exit trap" "trap cleanup EXIT"
	require_code "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
	require_code "sources determinism lib" "scripts/lib/ifa_determinism_common.sh"
	require_code "sources fault-injection lib" "scripts/lib/ifa_fault_injection_common.sh"
	require_source_inventory "source inventory omits driver lib" "scripts/lib/ifa_fault_injection_driver.sh"
	require_code "sources source-inventory lib" "scripts/lib/ifa_fault_injection_sources.sh"
	for source_name in \
		ifa_fault_injection_cells.sh \
		ifa_fault_injection_sql_cells.sh \
		ifa_fault_generic_cells.sh \
		ifa_fault_injection_codeowners_cells.sh \
		ifa_fault_injection_repo_dependency_cells.sh \
		ifa_fault_injection_submodule_pin_cells.sh \
		ifa_inheritance_live.sh \
		ifa_fault_injection_inheritance_cells.sh \
		ifa_shell_exec_live.sh \
		ifa_fault_injection_shell_exec_cells.sh \
		ifa_symbol_runtime_live.sh \
		ifa_fault_injection_symbol_runtime_cells.sh \
		ifa_workload_dependency_live.sh \
		ifa_fault_injection_workload_dependency_cells.sh; do
		[[ "$(_ifa_count_code_matches "scripts/lib/${source_name}" "${sources_lib}")" -ge 1 ]] \
			|| fail "source-inventory lib does not source ${source_name}"
	done
	require_source_inventory "source inventory omits cells lib" "scripts/lib/ifa_fault_injection_cells.sh"
	require_source_inventory "source inventory omits sql cells lib" "scripts/lib/ifa_fault_injection_sql_cells.sh"
	require_source_inventory "source inventory omits code-call live lib" "scripts/lib/ifa_code_call_live.sh"
	require_source_inventory "source inventory omits documentation ACK barrier lib" "scripts/lib/ifa_fault_injection_documentation_ack_barrier.sh"
	require_source_inventory "source inventory omits code-call cells lib" "scripts/lib/ifa_fault_injection_code_call_cells.sh"
	require_source_inventory "source inventory omits rationale live lib" "scripts/lib/ifa_rationale_live.sh"
	require_source_inventory "source inventory omits rationale cells lib" "scripts/lib/ifa_fault_injection_rationale_cells.sh"
	require_source_inventory "source inventory omits collateral-node lib" "scripts/lib/ifa_fault_injection_collateral_nodes.sh"
	# Deliberately NOT a per-family enumeration: an exhaustive list here goes
	# stale every time a family lands (this pin itself used to enumerate eight
	# families by name and was already missing two before this fix), and
	# nothing catches a stale "names ALL families" claim short of someone
	# reading the prose against the current roster. Pin the non-enumerating
	# framing instead, which cannot go stale by construction.
	require "gate overview does not claim an exhaustive per-family cassette list" "deliberately not enumerated by name here"
	require_code "failure log dump" "host binary logs (failure)"
	# The container-log tail alone cannot name a dead-lettered row: its
	# failure_message lives only in Postgres, and one real CI failure spent
	# all 60 tail lines on INFO chatter. Pin the durable dump and its
	# failure_message column so neither can be dropped silently.
	require_code "durable work-item failure dump" "durable work-item failures (Postgres)"
	require_code "durable dump selects failure_message" "failure_class, failure_message FROM fact_work_items"
	# Pinned against the PARSER in the shard lib, not the gate's usage text.
	# Both previously matched only the `printf` usage block, so the flags could
	# have been dropped from the case statement with the mirror green -- and
	# --no-compose had been reclassified as "framing" on exactly that evidence,
	# which is what a pin matching prose looks like from the outside.
	require_shard_lib "--no-compose flag is parsed" '--no-compose) use_compose=0 ;;'
	require_shard_lib "--keep flag is parsed" '--keep) keep=1 ;;'
	require "--no-compose documented in usage" "--no-compose"

	# Isolation: a Compose project name and port triple distinct from every
	# sibling verify-ifa-*.sh script and verify-golden-corpus-gate.sh.
	require_code "isolated compose project default" 'FAULT_COMPOSE_PROJECT:=eshu-ifa-fault-injection-$$'
	for reserved in \
		'ESHU_POSTGRES_PORT:-15432' 'NEO4J_BOLT_PORT:-7687' 'NEO4J_HTTP_PORT:-7474' \
		'ESHU_POSTGRES_PORT:-15532' 'NEO4J_BOLT_PORT:-7788' 'NEO4J_HTTP_PORT:-7575' \
		'ESHU_POSTGRES_PORT:-15635' 'NEO4J_BOLT_PORT:-7792' 'NEO4J_HTTP_PORT:-7679' \
		'ESHU_POSTGRES_PORT:-15636' 'NEO4J_BOLT_PORT:-7793' 'NEO4J_HTTP_PORT:-7680' \
		'ESHU_POSTGRES_PORT:-15637' 'NEO4J_BOLT_PORT:-7794' 'NEO4J_HTTP_PORT:-7681'; do
		if rg --fixed-strings --quiet -- "${reserved}" "${script}"; then
			fail "must not reuse a sibling verify-ifa-*.sh / verify-golden-corpus-gate.sh default port: ${reserved}"
		fi
	done
	require_code "exported Postgres port override" 'export ESHU_POSTGRES_PORT='
	require_code "exported Neo4j bolt port override" 'export NEO4J_BOLT_PORT='
	require_code "exported Neo4j http port override" 'export NEO4J_HTTP_PORT='
}

# run_ifa_fault_cell_catalog_cases pins the numbered cell catalog in
# docs/internal/ifa-fault-cell-catalog.md against the gate's actual dispatch
# list. The catalog used to be a comment block inside verify-ifa-fault-injection.sh
# and drifted anyway: three workload_dependency cells (#6003) landed dispatched
# and inventoried but never got numbered entries, leaving the list jumping 33 ->
# 37 until #6212. Prose nothing parses is prose nothing protects, so this counts
# both sides and fails when they disagree. It is a count check rather than a
# name-by-name diff because the catalog labels are human descriptions
# ("kill-worker-after-claim-shell-exec"), not the shell identifiers
# (cell_killworker_shell_exec) — a stricter mapping would have to encode that
# translation and would red on wording changes that are not drift.
run_ifa_fault_cell_catalog_cases() {
	local gate="${repo_root}/scripts/verify-ifa-fault-injection.sh"
	# Read the same ${cell_catalog_doc} require_catalog pins, rather than
	# rebuilding the path here: two independent spellings of one path is how a
	# guard ends up asserting against a file nobody else is using.
	local catalog="${cell_catalog_doc}"

	[[ -f "${catalog}" ]] \
		|| fail "cell catalog missing: ${catalog} (verify-ifa-fault-injection.sh's header points at it)"

	local dispatched entries
	dispatched="$(rg --count-matches '^ifa_fault_shard_run ' "${gate}" || printf '0')"
	entries="$(rg --count-matches '^[0-9]+\. ' "${catalog}" || printf '0')"

	if [[ "${dispatched}" -lt 30 ]]; then
		fail "only ${dispatched} ifa_fault_shard_run dispatch(es) found in verify-ifa-fault-injection.sh; the dispatch derivation has collapsed and this comparison would pass vacuously"
	fi
	if [[ "${entries}" != "${dispatched}" ]]; then
		fail "cell catalog lists ${entries} entrie(s) but the gate dispatches ${dispatched} cell(s); add or remove the numbered entry in docs/internal/ifa-fault-cell-catalog.md so the two stay in lockstep"
	fi

	# Ordered roster: the catalog numbers entries in dispatch order and
	# other gate prose cross-references those numbers, so a count match
	# alone lets mislabeled positions through (the six #6309 cells landed
	# dispatched after submodule-pin but numbered 44-49, shifting every
	# later label wrong while the count stayed green). Translate each
	# dispatched cell_* identifier to its catalog label and require the
	# catalog's numbered titles to match in order. The translation is
	# prefix-mechanical except the three symbol kill cells, whose labels
	# name the runner-lease-wait variant explicitly.
	local cell rest label actual_title pos=0
	while IFS= read -r cell; do
		pos=$((pos + 1))
		cell="${cell#cell_}"
		# Catalog labels hyphenate the whole remainder (code_calls reads
		# code-calls there), so translate first, hyphenate after.
		case "${cell}" in
			baseline_*) rest="${cell#baseline_}"; label="baseline-${rest//_/-}" ;;
			baseline) label="baseline" ;;
			killworker_handles_route|killworker_runs_in|killworker_invokes_cloud_action)
				rest="${cell#killworker_}"; label="kill-worker-after-runner-lease-wait-${rest//_/-}" ;;
			killworker_*) rest="${cell#killworker_}"; label="kill-worker-after-claim-${rest//_/-}" ;;
			killworker) label="kill-worker-after-claim" ;;
			expirelease) label="expire-lease-mid-handler" ;;
			failgraphwrite_*) rest="${cell#failgraphwrite_}"; label="fail-graph-write-once-then-succeed-${rest//_/-}" ;;
			failgraphwrite) label="fail-graph-write-once-then-succeed" ;;
			restartbackend) label="restart-backend-between-phase-groups" ;;
			duplicatedelivery) label="duplicate-delivery" ;;
			deltaretract) label="delta-retract" ;;
			*) fail "no catalog-label translation for dispatched cell ${cell}; extend the roster check, not just the catalog" ;;
		esac
		actual_title="$(sed -n "s/^${pos}\\. \\([^ ]*\\) .*/\\1/p" "${catalog}" | head -n 1)"
		if [[ "${actual_title}" != "${label}" ]]; then
			fail "catalog entry ${pos} is ${actual_title:-<missing>} but dispatch position ${pos} is ${label}; renumber docs/internal/ifa-fault-cell-catalog.md to dispatch order"
		fi
	done < <(rg --no-filename -o '^ifa_fault_shard_run cell_[a-z_0-9]+' "${gate}" | sed 's/^ifa_fault_shard_run //' || printf '')
	[[ "${pos}" == "${dispatched}" ]] \
		|| fail "roster walk saw ${pos} dispatched cell(s), want ${dispatched}; the dispatch derivation drifted from the count check above"
}

# run_ifa_fault_lib_cap_coverage_cases caps EVERY sourced case module by globbing
# the directory, not by reading the shell's variable table. The mirror's own
# cap loop walks `compgen -v | rg '_lib$'`, which can only see a binding that
# already exists when the loop runs: test-ifa-fault-injection-deployable-unit-kill-isolation-cases.sh
# is bound inside another case module, sourced AFTER that loop, so the loop never
# saw it and the file went uncapped from the day it was written (#6212). That is
# a lifetime gap, not a list omission, and no static read of the mirror finds it.
#
# Globbing sidesteps binding order entirely: a module that exists is checked,
# whether or not anything has bound it yet, and a module added tomorrow is
# checked without anyone remembering to register it. IFA_LIB_CAP_GLOB_DIR is
# injectable so this case can be proven to fail against a synthetic oversized
# module without writing one into scripts/lib.
run_ifa_fault_lib_cap_coverage_cases() {
	local dir="${IFA_LIB_CAP_GLOB_DIR:-${repo_root}/scripts/lib}"
	local checked=0 f lines

	for f in "${dir}"/test-ifa-*.sh; do
		[[ -f "${f}" ]] || continue
		checked=$((checked + 1))
		lines="$(wc -l <"${f}" | tr -d ' ')"
		[[ "${lines}" -lt 500 ]] \
			|| fail "$(basename "${f}") is ${lines} lines; every scripts/lib/test-ifa-*.sh must stay under 500"
	done

	# Floor, so a glob that matches nothing cannot report success. 20 is
	# hand-written and below the count on disk; it only grows as families land.
	[[ "${checked}" -ge 20 ]] \
		|| fail "line-cap coverage checked only ${checked} test-ifa-*.sh module(s); the glob has collapsed and this case would pass vacuously"

	# The private-data scan's positive control lives in a module both mirrors
	# share, so it is pinned from both sides rather than only from the
	# determinism scan module. Without this, deleting the control leaves the
	# pattern handed over unchecked and every file reading clean (#6161).
	# EXACTLY ONE of each: the control loop's assertion, and the hand-written
	# sample count that catches a deleted sample.
	[[ "$(_ifa_count_code_matches '[[ "${rc}" -eq 0 ]] || {' "${private_data_pattern_lib}")" -eq 1 ]] \
		|| fail "the private-data pattern's positive control no longer asserts that each planted sample matches -- the pattern would be handed over unchecked"
	[[ "$(_ifa_count_code_matches '"${#samples[@]}" -eq 14' "${private_data_pattern_lib}")" -eq 1 ]] \
		|| fail "the private-data pattern's sample set is no longer counted at 14 -- add the sample for the new alternative and bump both numbers together"
}
