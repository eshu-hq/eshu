#!/bin/sh
# Docker entrypoint for the Eshu release image (issue #11453).
#
# Contract:
# - `eshu-api` with no --core-base-url flag and ESHU_CORE_BASE_URL set
#   execs with the flag appended (--core-base-url=$ESHU_CORE_BASE_URL).
# - `eshu-api` with an explicit --core-base-url flag (--core-base-url=x or
#   --core-base-url x), or any other command, execs untouched.
# - `eshu-api` with no flag and no ESHU_CORE_BASE_URL prints a usage naming
#   both forms to stderr and exits 1 (fail-closed, like the binary itself).
set -eu

has_core_flag() {
	for arg in "$@"; do
		case "${arg}" in
			--core-base-url | --core-base-url=*) return 0 ;;
		esac
	done
	return 1
}

if [ "${1:-}" = "eshu-api" ] && ! has_core_flag "$@"; then
	if [ -n "${ESHU_CORE_BASE_URL:-}" ]; then
		exec "$@" "--core-base-url=${ESHU_CORE_BASE_URL}"
	fi
	cat >&2 <<'USAGE'
usage: eshu-api requires the core origin.
  Pass it as a flag:
    docker run ... <image> eshu-api --core-base-url=http://core:8080
  or as an environment variable:
    docker run -e ESHU_CORE_BASE_URL=http://core:8080 ... <image> eshu-api
USAGE
	exit 1
fi

exec "$@"
