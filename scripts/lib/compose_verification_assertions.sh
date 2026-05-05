#!/usr/bin/env bash

eshu_assert_json_query() {
	local file="$1" query="$2" description="$3"
	jq -e "$query" "$file" >/dev/null || {
		echo "$description" >&2
		cat "$file" >&2
		return 1
	}
}

eshu_api_post_json() {
	local path="$1" payload="$2" output_file="$3"
	if [[ -n "$API_KEY" ]]; then
		curl -fsS \
			-X POST \
			-H "Authorization: Bearer $API_KEY" \
			-H "Content-Type: application/json" \
			-d "$payload" \
			"$API_BASE_URL$path" \
			>"$output_file"
	else
		curl -fsS \
			-X POST \
			-H "Content-Type: application/json" \
			-d "$payload" \
			"$API_BASE_URL$path" \
			>"$output_file"
	fi
}

eshu_api_expect_status() {
	local method="$1" path="$2" payload="$3" expected_status="$4" status_file="$5" output_file="$6"
	local -a curl_args=(-sS -o "$output_file" -w "%{http_code}")
	if [[ -n "$API_KEY" ]]; then
		curl_args+=(-H "Authorization: Bearer $API_KEY")
	fi
	if [[ "$method" == "POST" ]]; then
		curl_args+=(-X POST -H "Content-Type: application/json" -d "$payload")
	fi
	curl_args+=("$API_BASE_URL$path")
	curl "${curl_args[@]}" >"$status_file"
	if [[ "$(<"$status_file")" != "$expected_status" ]]; then
		echo "Expected HTTP $expected_status for $method $path, got $(<"$status_file")" >&2
		cat "$output_file" >&2
		return 1
	fi
}

eshu_neo4j_query_to_file() {
	local query="$1" output_file="$2"
	"${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
		-u neo4j \
		-p "${ESHU_NEO4J_PASSWORD:-change-me}" \
		--format plain \
		"$query" >"$output_file"
}

eshu_neo4j_count_equals() {
	local query="$1" expected="$2" description="$3" output_file="$4"
	eshu_neo4j_query_to_file "$query" "$output_file"
	local result
	result="$(tail -n 1 "$output_file" | tr -d '\r')"
	if [[ "$result" != "$expected" ]]; then
		echo "$description: expected $expected, got $result" >&2
		cat "$output_file" >&2
		return 1
	fi
}
