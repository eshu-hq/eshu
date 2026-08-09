#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
driver="${repo_root}/scripts/verify_dead_iac_compose.sh"

# The raw-Cypher handler rejects a query whose embedded LIMIT is greater than
# the request envelope's limit. Keep the driver request and query bounds in
# lockstep so the deployed graph assertion reaches the backend.
if ! rg -U --quiet --fixed-strings \
	'payload="$(jq -cn --arg cypher "$cypher" --argjson limit 200 '\''{cypher_query: $cypher, limit: $limit}'\'')"' \
	"${driver}"; then
	printf 'dead-IaC graph request must declare the same 200-row envelope limit as its Cypher query\n' >&2
	exit 1
fi

printf 'test-verify-dead-iac-compose: pass\n'
