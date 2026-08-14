#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced library reads driver-owned globals.
# Lifecycle helpers for scripts/verify-ifa-determinism.sh. The driver owns
# strict mode and all referenced globals; this library keeps cleanup ownership
# and runtime endpoint configuration in one auditable place.

# ifa_det_cleanup reports failure logs, reaps owned host processes, and removes
# the matrix stack/work directory unless --keep requested retention.
ifa_det_cleanup() {
	local status=$?
	if [[ "${status}" -ne 0 && -d "${log_dir}" ]]; then
		printf '\n=== host binary logs (failure) ===\n' >&2
		for logf in "${log_dir}"/*.log; do
			[[ -f "${logf}" ]] || continue
			printf '\n--- %s ---\n' "$(basename "${logf}")" >&2
			tail -40 "${logf}" >&2 || true
		done
	fi
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	if [[ "${keep}" -eq 1 ]]; then
		printf '\n[--keep] work dir retained: %s\n' "${work_dir}" >&2
	else
		if [[ "${use_compose}" -eq 1 ]]; then
			docker compose -p "${DETERMINISM_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
		fi
		rm -rf "${work_dir}"
	fi
	exit "${status}"
}

# ifa_det_configure_runtime exports the isolated local endpoints shared by
# every host binary in one matrix run.
ifa_det_configure_runtime() {
	export ESHU_GRAPH_BACKEND=nornicdb
	export NEO4J_URI="bolt://localhost:${NEO4J_BOLT_PORT}"
	export NEO4J_USERNAME=neo4j
	export NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}"
	export NEO4J_DATABASE=nornic
	export ESHU_NEO4J_DATABASE=nornic
	export DEFAULT_DATABASE=nornic
	export ESHU_POSTGRES_DSN="postgresql://eshu:${ESHU_POSTGRES_PASSWORD}@localhost:${ESHU_POSTGRES_PORT}/eshu"
	export ESHU_CONTENT_STORE_DSN="${ESHU_POSTGRES_DSN}"
	# Concurrent lifecycle binaries must not contend for fixed status/metrics
	# ports, so each asks the kernel for an ephemeral port.
	export ESHU_LISTEN_ADDR="127.0.0.1:0"
	export ESHU_METRICS_ADDR="127.0.0.1:0"
	unset ESHU_PPROF_ADDR || true
}
