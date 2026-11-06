#!/usr/bin/env bash
# Graph-backend selection for the Ifá live gates (determinism, dead-letter
# matrix, fault-injection, replay-drive, scheduled proofs).
#
# Neo4j is the supported canonical graph backend (docs/internal/design/
# 7331-neo4j-primary-graph-backend-decision.md), so the Ifá gates run against it
# by default. ESHU_GRAPH_BACKEND=nornicdb restores the retired NornicDB variant
# for a manual run; CI no longer wires it (owner order 2026-11-05).
#
# This file is a plain function library. It does not set shell options: the
# caller owns strict mode. Every function prints its own diagnostic to stderr and
# returns non-zero on failure; callers decide whether to die.
#
# After ifa_graph_backend_init the following globals are set:
#   IFA_GRAPH_SERVICE       compose service name of the graph database
#   IFA_GRAPH_DATABASE      database name every host binary connects to
#   IFA_GRAPH_COMPOSE_FILE  compose file basename under eshu-deploy compose/
#   IFA_GRAPH_LABEL         human label for log and error text

# ifa_graph_backend_init validates ESHU_GRAPH_BACKEND (default neo4j), exports it,
# and sets the IFA_GRAPH_* globals. It is idempotent.
ifa_graph_backend_init() {
	: "${ESHU_GRAPH_BACKEND:=neo4j}"
	case "${ESHU_GRAPH_BACKEND}" in
	neo4j)
		IFA_GRAPH_SERVICE=neo4j
		IFA_GRAPH_DATABASE=neo4j
		IFA_GRAPH_COMPOSE_FILE=docker-compose.neo4j.yml
		IFA_GRAPH_LABEL=Neo4j
		;;
	nornicdb)
		IFA_GRAPH_SERVICE=nornicdb
		IFA_GRAPH_DATABASE=nornic
		IFA_GRAPH_COMPOSE_FILE=docker-compose.yaml
		IFA_GRAPH_LABEL=NornicDB
		;;
	*)
		echo "ifa_graph_backend_init: unsupported ESHU_GRAPH_BACKEND='${ESHU_GRAPH_BACKEND}' (want neo4j or nornicdb)" >&2
		return 1
		;;
	esac
	export ESHU_GRAPH_BACKEND
}

# ifa_graph_compose_file prints the compose file path for the selected backend.
#
# Args: deploy_checkout
ifa_graph_compose_file() {
	local deploy_checkout="${1:?ifa_graph_compose_file: deploy checkout path required}"
	ifa_graph_backend_init || return 1
	printf '%s/compose/%s\n' "${deploy_checkout}" "${IFA_GRAPH_COMPOSE_FILE}"
}

# ifa_graph_probe runs one in-container readiness probe against the graph
# service of a compose project. The probe runs through `compose exec`, so it
# never sees host port remapping. Returns 0 when the database answers.
#
# Args: compose_project compose_file
ifa_graph_probe() {
	local compose_project="$1"
	local compose_file="$2"
	ifa_graph_backend_init || return 1
	case "${ESHU_GRAPH_BACKEND}" in
	neo4j)
		bash scripts/ci/compose.sh -p "${compose_project}" -f "${compose_file}" -- exec -T neo4j \
			cypher-shell -u neo4j -p "${ESHU_NEO4J_PASSWORD:-change-me}" "RETURN 1" >/dev/null 2>&1
		;;
	nornicdb)
		bash scripts/ci/compose.sh -p "${compose_project}" -f "${compose_file}" -- exec -T nornicdb \
			wget --spider -q http://localhost:7474/health >/dev/null 2>&1
		;;
	esac
}

# ifa_graph_up starts the selected graph service plus Postgres for a compose
# project.
#
# Args: compose_project compose_file
ifa_graph_up() {
	local compose_project="$1"
	local compose_file="$2"
	ifa_graph_backend_init || return 1
	bash scripts/ci/compose.sh -p "${compose_project}" -f "${compose_file}" -- up -d "${IFA_GRAPH_SERVICE}" postgres
}

# ifa_graph_restart restarts only the graph service of a compose project (a plain
# restart, not down/up, so the data volume survives).
#
# Args: compose_project compose_file
ifa_graph_restart() {
	local compose_project="$1"
	local compose_file="$2"
	ifa_graph_backend_init || return 1
	bash scripts/ci/compose.sh -p "${compose_project}" -f "${compose_file}" -- restart "${IFA_GRAPH_SERVICE}"
}
