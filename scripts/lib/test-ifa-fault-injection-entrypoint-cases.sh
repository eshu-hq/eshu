#!/usr/bin/env bash
# shellcheck disable=SC2154
# Entrypoint, lifecycle, and port-isolation source locks for the IFA fault gate.
# Sourced by scripts/test-verify-ifa-fault-injection.sh.

run_ifa_fault_entrypoint_static_cases() {
	# Strict mode, self-cleanup, and the masking-safe bash>=4.4 guard.
	require_line "strict mode" "set -euo pipefail"
	require "exit trap" "trap cleanup EXIT"
	require "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
	require "sources determinism lib" "scripts/lib/ifa_determinism_common.sh"
	require "sources fault-injection lib" "scripts/lib/ifa_fault_injection_common.sh"
	require "sources driver lib" "scripts/lib/ifa_fault_injection_driver.sh"
	require "sources cells lib" "scripts/lib/ifa_fault_injection_cells.sh"
	require "sources sql cells lib" "scripts/lib/ifa_fault_injection_sql_cells.sh"
	require "sources code-call live lib" "scripts/lib/ifa_code_call_live.sh"
	require "sources documentation ACK barrier lib" "scripts/lib/ifa_fault_injection_documentation_ack_barrier.sh"
	require "sources code-call cells lib" "scripts/lib/ifa_fault_injection_code_call_cells.sh"
	require "sources rationale live lib" "scripts/lib/ifa_rationale_live.sh"
	require "sources rationale cells lib" "scripts/lib/ifa_fault_injection_rationale_cells.sh"
	require "sources collateral-node lib" "scripts/lib/ifa_fault_injection_collateral_nodes.sh"
	require "gate overview names all exact-set cassette families" "relationship, code-call, documentation, and rationale family cassettes"
	require "failure log dump" "host binary logs (failure)"
	# The container-log tail alone cannot name a dead-lettered row: its
	# failure_message lives only in Postgres, and one real CI failure spent
	# all 60 tail lines on INFO chatter. Pin the durable dump and its
	# failure_message column so neither can be dropped silently.
	require "durable work-item failure dump" "durable work-item failures (Postgres)"
	require "durable dump selects failure_message" "failure_class, failure_message FROM fact_work_items"
	require "--no-compose flag" "--no-compose"
	require "--keep flag" "--keep"

	# Isolation: a Compose project name and port triple distinct from every
	# sibling verify-ifa-*.sh script and verify-golden-corpus-gate.sh.
	require "isolated compose project default" 'FAULT_COMPOSE_PROJECT:=eshu-ifa-fault-injection-$$'
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
	require "exported Postgres port override" 'export ESHU_POSTGRES_PORT='
	require "exported Neo4j bolt port override" 'export NEO4J_BOLT_PORT='
	require "exported Neo4j http port override" 'export NEO4J_HTTP_PORT='
}
