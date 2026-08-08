#!/usr/bin/env bash
#
# golden-corpus-readiness.sh — host-side TCP readiness probe for the B-7
# gate's "wait for backends" loop (#5813).
#
# The official Postgres image's docker-entrypoint runs a TEMPORARY server
# during initdb with listen_addresses='' (unix socket only, no TCP), runs the
# init scripts, then shuts it down and starts the REAL server with TCP
# enabled. `pg_isready` run INSIDE the container (as the gate's own probe
# does via `docker compose exec`) talks to that unix socket and reports
# "accepting connections" throughout, including during the initdb window —
# but every consumer that follows (bootstrap-index, the collectors, the
# reducer/projector, this script's own pg() helper) connects from the HOST
# over TCP to 127.0.0.1:<port>, which is refused/reset until the REAL server
# starts listening on TCP. Observed twice in real runs: bootstrap-index died
# with a "connection reset by peer" ping error at the exact moment `docker
# ps` still showed the compose healthcheck as "(health: starting)" — the
# compose healthcheck was right, the in-container-only probe was not.
#
# host_tcp_port_open tests what the consumers actually need: a HOST-side TCP
# connect, using bash's /dev/tcp pseudo-device (built into bash's exec
# redirection; no nc/psql dependency). This is a bash extension, not POSIX
# sh, and is NOT supported by zsh — callers must run it under bash (this
# repo's scripts already declare `#!/usr/bin/env bash`).
host_tcp_port_open() {
	local host="$1" port="$2"
	(exec 3<>"/dev/tcp/${host}/${port}") 2>/dev/null
}

# start_golden_corpus_backends owns Compose startup and the paired graph plus
# Postgres readiness loop. The caller provides the gate's configuration, log(),
# and die(); keeping orchestration here gives the top-level gate file headroom.
start_golden_corpus_backends() {
	local backends_ready graph_ready
	[[ "${use_compose}" -eq 1 ]] || return 0

	log "start Postgres + ${graph_service}"
	ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}" ESHU_POSTGRES_PASSWORD="${ESHU_POSTGRES_PASSWORD}" \
		docker compose "${compose_args[@]}" up -d "${graph_service}" postgres

	log "wait for backends"
	backends_ready=false
	for _ in $(seq 1 60); do
		graph_ready=false
		# NornicDB is one process whose HTTP and Bolt listeners start together;
		# this health request performs a real TCP and HTTP round trip.
		if [[ "${graph_service}" == "nornicdb" ]]; then
			docker compose "${compose_args[@]}" exec -T nornicdb wget --spider -q http://localhost:7474/health >/dev/null 2>&1 && graph_ready=true
		else
			docker compose "${compose_args[@]}" exec -T neo4j cypher-shell -u neo4j -p "${ESHU_NEO4J_PASSWORD}" "RETURN 1" >/dev/null 2>&1 && graph_ready=true
		fi
		# Require both the container socket probe and the host TCP path used by
		# every following consumer; either check alone can report too early.
		if [[ "${graph_ready}" == "true" ]] && \
			docker compose "${compose_args[@]}" exec -T postgres pg_isready -U eshu -d eshu >/dev/null 2>&1 && \
			host_tcp_port_open 127.0.0.1 "${ESHU_POSTGRES_PORT}"; then
			backends_ready=true
			break
		fi
		sleep 2
	done
	[[ "${backends_ready}" == "true" ]] ||
		die "Postgres + ${graph_service} did not become ready within budget"
}
