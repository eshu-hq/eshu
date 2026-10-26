#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat >"$work/bin/curl" <<'CURL'
#!/usr/bin/env bash
cat "$MCP_TEST_RESPONSE"
CURL
chmod +x "$work/bin/curl"
export PATH="$work/bin:$PATH" MCP_TEST_RESPONSE="$work/response.json"

extract_function() {
	local script="$1" name="$2"
	rg -U -o "^${name}\\(\\) \\{[\\s\\S]*?^\\}" "$repo_root/$script"
}

source "$repo_root/scripts/lib/remote_e2e_target_story_common.sh"
eval "$(extract_function scripts/verify_local_public_collector_proof.sh mcp_tool_envelope | sed 's/^mcp_tool_envelope()/local_public_mcp_tool_envelope()/')"
eval "$(extract_function scripts/verify_dead_iac_compose.sh verify_mcp)"
eval "$(extract_function scripts/run-remote-e2e-component-extension.sh capture_component_mcp)"
source "$repo_root/scripts/lib/compose_verification_assertions.sh"

docker() { cat "$MCP_TEST_RESPONSE"; }
die() { printf '%s\n' "$*" >&2; exit 1; }
dead_iac_repo_names() { printf '%s\n' fixture; }
MCP_URL=http://fixture/mcp/message
MCP_BASE_URL=http://fixture
MCP_TOKEN=
API_KEY=
API_TIMEOUT_SECONDS=5
MCP_PORT=8081
MCP_RESPONSE_FILE="$work/dead-response.json"
TMP_DIR="$work"
mcp=fixture

make_response() {
	local variant="$1"
	jq -n --arg variant "$variant" '
		{data:{id:"preferred",truth_basis:"materialized_reducer_rows",analysis_status:"materialized_reachability",findings_count:10},truth:{source:"fixture"},error:null} as $good |
		($good | .data.id="resource" | .data.findings_count=9) as $old |
		{type:"resource",resource:{uri:"eshu://tool-result/envelope",text:($old|tojson)}} as $resource |
		{jsonrpc:"2.0",id:1,result:{content:[$resource],structuredContent:$good}} |
		if $variant == "structured" then .result.content=[]
		elif $variant == "dual" then .
		elif $variant == "resource" then del(.result.structuredContent) | .result.content[0].resource.text=($good|tojson)
		elif $variant == "array" then .result.content=[] | .result.structuredContent.data=[{id:"preferred"}]
		elif $variant == "null" then .result.structuredContent=null
		elif $variant == "malformed" then .result.structuredContent={data:{},truth:{}}
		elif $variant == "envelope_error" then .result.structuredContent.error={code:"failed"}
		elif $variant == "resource_error" then del(.result.structuredContent) | .result.content[0].resource.text=($good | .error={code:"failed"} | tojson)
		elif $variant == "jsonrpc_error" then .error={code:-1,message:"failed"}
		elif $variant == "jsonrpc_version" then .jsonrpc="1.0"
		elif $variant == "tool_error" then .result.isError=true
		else error("unknown variant") end
	' >"$MCP_TEST_RESPONSE"
}

check_case() {
	local parser="$1" variant="$2" expected="$3" actual=""
	make_response "$variant"
	case "$parser" in
		target) mcp_tool_envelope example '{}' "$work/output.json" 2>"$work/stderr" && actual=pass || actual=fail ;;
		local) local_public_mcp_tool_envelope example '{}' "$work/output.json" 2>"$work/stderr" && actual=pass || actual=fail ;;
		dead) verify_mcp 2>"$work/stderr" && actual=pass || actual=fail ;;
		component) (capture_component_mcp example '{}' "$work/output.json") 2>"$work/stderr" && actual=pass || actual=fail ;;
	esac
	if [[ "$actual" != "$expected" ]]; then
		printf 'FAIL %s %s: expected %s, got %s\n' "$parser" "$variant" "$expected" "$actual" >&2
		cat "$work/stderr" >&2
		return 1
	fi
	if [[ "$expected" == pass && "$parser" != dead ]]; then
		local query='.data.id == "preferred"'
		[[ "$variant" != array ]] || query='.data == [{"id":"preferred"}]'
		if ! jq -e "$query" "$work/output.json" >/dev/null; then
			printf 'FAIL %s %s: wrong envelope selected\n' "$parser" "$variant" >&2
			return 1
		fi
	fi
}

failures=0
for parser in target local dead component; do
	for variant in structured dual resource array null malformed envelope_error resource_error jsonrpc_error jsonrpc_version tool_error; do
		expected=pass
		case "$variant" in null|malformed|envelope_error|resource_error|jsonrpc_error|jsonrpc_version|tool_error) expected=fail ;; esac
		[[ "$parser" != dead || "$variant" != array ]] || expected=fail
		check_case "$parser" "$variant" "$expected" || failures=$((failures + 1))
	done
done
if ((failures > 0)); then
	printf '%d MCP parser cases failed\n' "$failures" >&2
	exit 1
fi
printf 'PASS: 44 MCP parser cases\n'
