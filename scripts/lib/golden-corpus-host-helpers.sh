#!/usr/bin/env bash
#
# golden-corpus-host-helpers.sh — host-binary build/launch/query helpers for the
# golden corpus gate orchestrator (scripts/verify-golden-corpus-gate.sh).
# Extracted into a lib chunk so the orchestrator stays under the 500-line cap.
#
# Requires (set by the orchestrator before any of these are called): bin_dir,
# log_dir, use_compose, compose_file, ESHU_POSTGRES_DSN, bg_pids (array), and
# the die() function. Bodies resolve these lazily at call time, so sourcing
# this file before those globals are set is safe as long as they exist by the
# time a given helper actually runs.

build_bin() {
	local cmd="$1"
	CGO_ENABLED=1 go -C go build -o "${bin_dir}/eshu-${cmd}" "./cmd/${cmd}" \
		|| die "build ${cmd} failed"
}

# start_bg <name> <pidvar> <cmd...> launches cmd in the background, records its
# pid in bg_pids (so the cleanup trap can reap it on EVERY exit path), and writes
# the pid into the caller-named variable pidvar. The pid is assigned via
# printf -v in the PARENT shell — a previous version returned it through command
# substitution, whose subshell discarded the bg_pids append, leaving the trap a
# no-op that leaked processes on failure.
start_bg() {
	local name="$1" pidvar="$2"; shift 2
	"$@" >"${log_dir}/${name}.log" 2>&1 &
	local pid=$!
	bg_pids+=("${pid}")
	printf -v "${pidvar}" '%s' "${pid}"
}

# pg runs a single-value SQL query against the gate's Postgres, working in both
# compose mode (via the postgres container) and --no-compose mode (via a local
# psql client). Used to assert the cassette collectors actually committed.
pg() {
	local sql="$1"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose "${compose_args[@]}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -tA -c "${sql}"
	else
		command -v psql >/dev/null 2>&1 || die "psql client required in --no-compose mode"
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -tA -c "${sql}"
	fi
}
