#!/bin/sh
set -eu

fail() {
	echo "remote e2e observability preflight failed: $*" >&2
	exit 1
}

is_set() {
	value=$(printf '%s' "${1:-}" | tr -d '[:space:]')
	[ -n "$value" ]
}

env_value() {
	env_name=$1
	case "$env_name" in
		'' | *[!A-Za-z0-9_]*)
			fail "invalid configured environment variable name"
			;;
	esac
	eval "printf '%s' \"\${$env_name:-}\""
}

require_named_env_value() {
	label=$1
	env_ref=$2
	if ! is_set "$env_ref"; then
		echo optional
		return
	fi
	env_value=$(env_value "$env_ref")
	if ! is_set "$env_value"; then
		fail "${env_ref} is required when ${label} names it"
	fi
	echo required
}

collector=${ESHU_OBSERVABILITY_COLLECTOR:-}
enable_env=${ESHU_OBSERVABILITY_ENABLE_ENV:-}
base_url_env=${ESHU_OBSERVABILITY_BASE_URL_ENV:-}

if ! is_set "$collector"; then
	fail "ESHU_OBSERVABILITY_COLLECTOR is required"
fi
if ! is_set "$enable_env"; then
	fail "ESHU_OBSERVABILITY_ENABLE_ENV is required"
fi
if ! is_set "$base_url_env"; then
	fail "ESHU_OBSERVABILITY_BASE_URL_ENV is required"
fi

if [ "$(printf '%s' "${ESHU_OBSERVABILITY_ENABLED:-}" | tr -d '[:space:]')" != "true" ]; then
	fail "${enable_env} must be true when the ${collector} profile is selected"
fi

if ! is_set "${ESHU_OBSERVABILITY_BASE_URL:-}"; then
	fail "${base_url_env} is required when the ${collector} profile is selected"
fi

token_state=$(require_named_env_value ESHU_OBSERVABILITY_TOKEN_ENV "${ESHU_OBSERVABILITY_TOKEN_ENV:-}")
tenant_state=$(require_named_env_value ESHU_OBSERVABILITY_TENANT_ID_ENV "${ESHU_OBSERVABILITY_TENANT_ID_ENV:-}")

echo "remote e2e observability preflight: collector=${collector} enabled=true target=configured token=${token_state} tenant=${tenant_state}"
