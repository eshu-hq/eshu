#!/usr/bin/env bash
# Fake gh for scripts/ci/test-prune-ghcr-buildcache.sh: serves a two-page
# package-versions listing and records every DELETE in MOCK_GH_CALLS.
#   MOCK_GH_LIST=ok|404|500   listing outcome (default ok)
#   MOCK_GH_DELETE=ok|fail    DELETE outcome (default ok)
set -euo pipefail
printf '%s\n' "$*" >> "${MOCK_GH_CALLS}"
if [[ "$*" == *"-X DELETE"* ]]; then
	[[ "${MOCK_GH_DELETE:-ok}" == ok ]] || { echo "gh: Server Error (HTTP 500)" >&2; exit 1; }
	exit 0
fi
case "${MOCK_GH_LIST:-ok}" in
	404) echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
	500) echo "gh: Server Error (HTTP 500)" >&2; exit 1 ;;
esac
cat "${MOCK_GH_PAGES}"
