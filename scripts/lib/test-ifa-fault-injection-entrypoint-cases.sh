#!/usr/bin/env bash
# shellcheck disable=SC2154
# Entrypoint, lifecycle, and port-isolation source locks for the IFA fault gate.
# Sourced by scripts/test-verify-ifa-fault-injection.sh.

run_ifa_fault_entrypoint_static_cases() {
	# Strict mode, self-cleanup, and the masking-safe bash>=4.4 guard.
	require_line "strict mode" "set -euo pipefail"
	require_code "exit trap" "trap cleanup EXIT"
	require_code "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
	require_code "sources determinism lib" "scripts/lib/ifa_determinism_common.sh"
	require_code "sources fault-injection lib" "scripts/lib/ifa_fault_injection_common.sh"
	require_code "sources driver lib" "scripts/lib/ifa_fault_injection_driver.sh"
	require_code "sources cells lib" "scripts/lib/ifa_fault_injection_cells.sh"
	require_code "sources sql cells lib" "scripts/lib/ifa_fault_injection_sql_cells.sh"
	require_code "sources code-call live lib" "scripts/lib/ifa_code_call_live.sh"
	require_code "sources documentation ACK barrier lib" "scripts/lib/ifa_fault_injection_documentation_ack_barrier.sh"
	require_code "sources code-call cells lib" "scripts/lib/ifa_fault_injection_code_call_cells.sh"
	require_code "sources rationale live lib" "scripts/lib/ifa_rationale_live.sh"
	require_code "sources rationale cells lib" "scripts/lib/ifa_fault_injection_rationale_cells.sh"
	require_code "sources collateral-node lib" "scripts/lib/ifa_fault_injection_collateral_nodes.sh"
	require "gate overview names all exact-set cassette families" "relationship, code-call, documentation, rationale, and repository-dependency family cassettes"
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
