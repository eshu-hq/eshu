#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CONFIG_FILE="${ESHU_MCP_CONFIG_FILE:-$REPO_ROOT/.mcp.json}"
LOCAL_SERVER_NAME="${ESHU_LOCAL_MCP_SERVER_NAME:-eshu-local-compose}"
SKIP_PROBES="${ESHU_SKIP_PROBES:-false}"

COMPOSE_CMD=()

require_tool() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required tool: $1" >&2
		exit 1
	}
}

detect_compose_cmd() {
	if docker compose version >/dev/null 2>&1; then
		COMPOSE_CMD=(docker compose)
	elif command -v docker-compose >/dev/null 2>&1; then
		COMPOSE_CMD=(docker-compose)
	else
		echo "Missing required compose command: docker compose or docker-compose" >&2
		exit 1
	fi
}

discover_compose_port() {
	local service="$1"
	local container_port="$2"
	local mapping
	mapping="$("${COMPOSE_CMD[@]}" port "$service" "$container_port" | tail -n 1)"
	[[ -n "$mapping" ]] || {
		echo "Could not determine published port for $service:$container_port" >&2
		exit 1
	}
	printf '%s\n' "${mapping##*:}"
}

discover_local_mcp_token() {
	"${COMPOSE_CMD[@]}" exec mcp-server sh -lc '
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

write_config() {
	local url="$1"
	local token="$2"
	local tmp_file

	tmp_file="$(mktemp)"
	jq \
		--arg server_name "$LOCAL_SERVER_NAME" \
		--arg url "$url" \
		--arg auth "Bearer $token" \
		'
		(.mcpServers //= {}) |
		.mcpServers[$server_name] = {
			type: "http",
			url: $url,
			headers: {
				Authorization: $auth
			}
		}
	' "$CONFIG_FILE" >"$tmp_file"
	mv "$tmp_file" "$CONFIG_FILE"
}

probe_health() {
	local mcp_url="$1"
	local base_url
	base_url="${mcp_url%/mcp/message}"
	curl -fsS "${base_url}/health" >/dev/null
}

probe_tools_list() {
	local mcp_url="$1"
	local token="$2"
	local response_file
	response_file="$(mktemp)"
	curl -fsS "$mcp_url" \
		-X POST \
		-H 'content-type: application/json' \
		-H "Authorization: Bearer $token" \
		--data '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' >"$response_file"
	jq -e '((.result.tools // []) | length) > 0' "$response_file" >/dev/null
	rm -f "$response_file"
}

probe_index_status() {
	local api_url="$1"
	local token="$2"
	local response_file
	response_file="$(mktemp)"
	curl -fsS "${api_url}/api/v0/index-status" \
		-H "Authorization: Bearer $token" >"$response_file"
	jq -e '(.status // "") != ""' "$response_file" >/dev/null
	rm -f "$response_file"
}

main() {
	require_tool jq
	require_tool curl

	if [[ ! -f "$CONFIG_FILE" ]]; then
		echo "Config file not found: $CONFIG_FILE" >&2
		exit 1
	fi

	local mcp_url token api_port api_url mcp_port
	mcp_url="${ESHU_LOCAL_MCP_URL:-}"
	token="${ESHU_LOCAL_MCP_TOKEN:-}"
	api_url="${ESHU_LOCAL_API_URL:-}"

	if [[ -z "$mcp_url" || -z "$token" || ( "$SKIP_PROBES" != "true" && -z "$api_url" ) ]]; then
		detect_compose_cmd
	fi

	if [[ -z "$mcp_url" ]]; then
		mcp_port="$(discover_compose_port mcp-server 8080)"
		mcp_url="http://127.0.0.1:${mcp_port}/mcp/message"
	fi

	if [[ -z "$token" ]]; then
		token="$(discover_local_mcp_token)"
	fi
	[[ -n "$token" ]] || {
		echo "Could not discover local MCP bearer token." >&2
		exit 1
	}

	write_config "$mcp_url" "$token"

	echo "Updated $CONFIG_FILE"
	echo "Local MCP server: $LOCAL_SERVER_NAME"
	echo "Local MCP URL: $mcp_url"

	if [[ "$SKIP_PROBES" == "true" ]]; then
		echo "Skipped probes because ESHU_SKIP_PROBES=true"
		exit 0
	fi

	if [[ -z "$api_url" ]]; then
		api_port="$(discover_compose_port eshu 8080)"
		api_url="http://127.0.0.1:${api_port}"
	fi

	probe_health "$mcp_url"
	probe_tools_list "$mcp_url" "$token"
	probe_index_status "$api_url" "$token"

	echo "Probe results:"
	echo "  MCP health: ok"
	echo "  MCP tools/list: ok"
	echo "  API index-status: ok"
}

main "$@"
