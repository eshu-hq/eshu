#!/usr/bin/env bash
# Hermetic generator tests: deterministic bytes and expected inventory shape.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="${repo_root}/scripts/generate-remote-validation-inventory.sh"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

specs="${tmp_root}/specs"
mkdir -p "${specs}"
printf '%s\n' \
	'capabilities:' \
	'  - capability: cap.example' \
	'    tools: [example]' \
	'    profiles:' \
	'      production: {status: supported, required_runtime: deployed_services, verification: [{remote_validation: prod-example}]}' \
	>"${specs}/capability-matrix.v1.yaml"

"${generator}" --specs "${specs}" --root "${tmp_root}" >/dev/null
inventory="${tmp_root}/docs/internal/remote-validation/inventory.generated.json"
cp "${inventory}" "${inventory}.first"
"${generator}" --specs "${specs}" --root "${tmp_root}" >/dev/null

if ! cmp -s "${inventory}" "${inventory}.first"; then
	printf 'not ok - generator output is not byte-for-byte deterministic\n' >&2
	exit 1
fi
printf 'ok - generator is byte-for-byte deterministic\n'

if ! rg -q --fixed-strings '"slug":"prod-example"' "${inventory}" ||
	! rg -q --fixed-strings '"cap.example/production"' "${inventory}"; then
	printf 'not ok - generated inventory omits the slug or production subject\n' >&2
	exit 1
fi
printf 'ok - generated inventory binds the slug to its production subject\n'

printf '2 passed, 0 failed\n'
