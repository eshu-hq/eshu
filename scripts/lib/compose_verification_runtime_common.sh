#!/usr/bin/env bash

ESHU_RESERVED_HOST_PORTS=()
ESHU_PICKED_PORT=""

eshu_require_tool() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required tool: $1" >&2
		exit 1
	}
}

eshu_compose_wait_for_http() {
	local url="$1" attempts="$2" sleep_seconds="$3"
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		curl -fsS "$url" >/dev/null 2>&1 && return 0
		/bin/sleep "$sleep_seconds"
	done
	echo "Timed out waiting for $url" >&2
	return 1
}

# Wait for a one-shot compose service to exit cleanly. Generic version of
# eshu_compose_wait_for_bootstrap_exit: takes the service name as an argument
# so verifiers can wait on any one-shot service (db-migrate, bootstrap-index,
# future seeders, etc.) without re-defining the loop.
#
# Args:
#   $1 service_name
#   $2 timeout_seconds
eshu_compose_wait_for_named_exit() {
	local service="$1" timeout_seconds="$2"
	local deadline=$((SECONDS + timeout_seconds))
	while ((SECONDS < deadline)); do
		local container_id state exit_code
		container_id="$("${COMPOSE_CMD[@]}" ps -a -q "$service")"
		if [[ -z "$container_id" ]]; then
			sleep 2
			continue
		fi
		state="$(docker inspect --format='{{.State.Status}}' "$container_id" 2>/dev/null || true)"
		if [[ "$state" == "exited" ]]; then
			exit_code="$(docker inspect --format='{{.State.ExitCode}}' "$container_id" 2>/dev/null || true)"
			if [[ "$exit_code" != "0" ]]; then
				echo "$service exited with code $exit_code" >&2
				return 1
			fi
			return 0
		fi
		sleep 2
	done
	echo "Timed out waiting for $service to exit" >&2
	return 1
}

eshu_compose_wait_for_bootstrap_exit() {
	local timeout_seconds="$1"
	local deadline=$((SECONDS + timeout_seconds))

	while ((SECONDS < deadline)); do
		local container_id state exit_code
		container_id="$("${COMPOSE_CMD[@]}" ps -a -q bootstrap-index)"
		if [[ -z "$container_id" ]]; then
			/bin/sleep 2
			continue
		fi

		state="$(docker inspect --format='{{.State.Status}}' "$container_id" 2>/dev/null || true)"
		if [[ -z "$state" ]]; then
			/bin/sleep 2
			continue
		fi

		if [[ "$state" == "exited" ]]; then
			exit_code="$(docker inspect --format='{{.State.ExitCode}}' "$container_id" 2>/dev/null || true)"
			if [[ -z "$exit_code" ]]; then
				/bin/sleep 2
				continue
			fi
			if [[ "$exit_code" != "0" ]]; then
				echo "bootstrap-index exited with code $exit_code" >&2
				return 1
			fi
			return 0
		fi

		/bin/sleep 2
	done

	echo "Timed out waiting for bootstrap-index to finish" >&2
	return 1
}

eshu_compose_wait_for_index_completion() {
	local attempts="$1" sleep_seconds="$2" output_file="$3"
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		if api_get "/index-status" "$output_file" &&
			jq -e '
				(.status // "") == "healthy" and
				((.queue.outstanding // 0) == 0) and
				((.queue.in_flight // 0) == 0) and
				((.queue.pending // 0) == 0) and
				((.queue.retrying // 0) == 0) and
				((.queue.failed // 0) == 0)
			' "$output_file" >/dev/null; then
			return 0
		fi
		/bin/sleep "$sleep_seconds"
	done

	echo "Timed out waiting for /index-status to report queue completion" >&2
	return 1
}

eshu_compose_read_api_key() {
	"${COMPOSE_CMD[@]}" exec -T eshu sh -lc '
		token="${ESHU_API_KEY:-}"
		if [ -n "$token" ]; then
			printf %s "$token"
			exit 0
		fi
		home="${ESHU_HOME:-/data/.eshu}"
		if [ -f "$home/.env" ]; then
			sed -n "s/^ESHU_API_KEY=//p" "$home/.env" | tail -n 1 | tr -d "\n"
		fi
	'
}

eshu_pick_port() {
	local start_port="$1"
	local port
	for ((port = start_port; port < start_port + 200; port++)); do
		if ! nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
			echo "$port"
			return 0
		fi
	done
	echo "no free port found near $start_port" >&2
	return 1
}

eshu_reset_reserved_ports() {
	ESHU_RESERVED_HOST_PORTS=()
	ESHU_PICKED_PORT=""
}

eshu_pick_reserved_port() {
	local start_port="$1"
	local port
	for ((port = start_port; port < start_port + 200; port++)); do
		if [[ " ${ESHU_RESERVED_HOST_PORTS[*]} " == *" $port "* ]]; then
			continue
		fi
		if ! nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
			ESHU_RESERVED_HOST_PORTS+=("$port")
			ESHU_PICKED_PORT="$port"
			return 0
		fi
	done
	echo "no free reserved port found near $start_port" >&2
	return 1
}

eshu_assign_reserved_port() {
	local name="$1"
	local start_port="$2"
	eshu_pick_reserved_port "$start_port"
	printf -v "$name" '%s' "$ESHU_PICKED_PORT"
	export "$name"
}
