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
