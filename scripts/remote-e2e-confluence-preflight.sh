#!/bin/sh
set -eu

fail() {
	echo "remote e2e confluence preflight failed: $*" >&2
	exit 1
}

is_set() {
	value=$(printf '%s' "${1:-}" | tr -d '[:space:]')
	[ -n "$value" ]
}

selector_count=0
selector_name=

if is_set "${ESHU_CONFLUENCE_SPACE_ID:-}"; then
	selector_count=$((selector_count + 1))
	selector_name=ESHU_CONFLUENCE_SPACE_ID
fi
if is_set "${ESHU_CONFLUENCE_SPACE_IDS:-}"; then
	selector_count=$((selector_count + 1))
	selector_name=ESHU_CONFLUENCE_SPACE_IDS
fi
if is_set "${ESHU_CONFLUENCE_ROOT_PAGE_ID:-}"; then
	selector_count=$((selector_count + 1))
	selector_name=ESHU_CONFLUENCE_ROOT_PAGE_ID
fi

if ! is_set "${ESHU_CONFLUENCE_BASE_URL:-}"; then
	fail "ESHU_CONFLUENCE_BASE_URL is required"
fi

case "$selector_count" in
	0)
		fail "exactly one bounded selector is required; set one of ESHU_CONFLUENCE_SPACE_ID, ESHU_CONFLUENCE_SPACE_IDS, or ESHU_CONFLUENCE_ROOT_PAGE_ID"
		;;
	1) ;;
	*)
		fail "configure only one of ESHU_CONFLUENCE_SPACE_ID, ESHU_CONFLUENCE_SPACE_IDS, or ESHU_CONFLUENCE_ROOT_PAGE_ID"
		;;
esac

auth_mode=
if is_set "${ESHU_CONFLUENCE_BEARER_TOKEN:-}"; then
	auth_mode=bearer
elif is_set "${ESHU_CONFLUENCE_EMAIL:-}" && is_set "${ESHU_CONFLUENCE_API_TOKEN:-}"; then
	auth_mode=email_api_token
else
	fail "read-only credentials are required; set ESHU_CONFLUENCE_BEARER_TOKEN or both ESHU_CONFLUENCE_EMAIL and ESHU_CONFLUENCE_API_TOKEN"
fi

echo "remote e2e confluence preflight: selector=${selector_name} auth_mode=${auth_mode}"
