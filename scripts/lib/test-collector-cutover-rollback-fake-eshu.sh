#!/usr/bin/env bash
# Fake `eshu` for test-collector-cutover-rollback.sh. It models only the
# `eshu component` subcommands the rollback script calls, against files in
# $FAKE_STATE: activations (one instance ID per line) and grants
# (producer|version|kind|scope|revoked).
set -euo pipefail

state="${FAKE_STATE:?FAKE_STATE is required}"
printf 'eshu %s\n' "$*" >>"${state}/calls.log"

[[ "${1:-}" == "component" ]] || exit 64
sub="${2:-}"
shift 2

has_line() { awk -v want="$2" '$0 == want { found = 1 } END { exit !found }' "$1"; }

case "${sub}" in
disable)
	id="$1"
	shift
	instance=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--instance) instance="$2" ;;
		esac
		shift 2
	done
	if [[ -e "${state}/fail_disable" ]]; then
		printf 'Error: registry is read-only\n' >&2
		exit 1
	fi
	if has_line "${state}/activations" "${instance}"; then
		awk -v drop="${instance}" '$0 != drop' "${state}/activations" >"${state}/activations.new"
		mv "${state}/activations.new" "${state}/activations"
		printf 'disabled %s instance %s\n' "${id}" "${instance}"
	else
		printf 'Error: activation "%s" is not enabled for component "%s"\n' "${instance}" "${id}" >&2
		exit 1
	fi
	;;
revoke-grant)
	producer="$1"
	shift
	version="" kind="" scope=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--version) version="$2" ;;
		--kind) kind="$2" ;;
		--scope) scope="$2" ;;
		esac
		shift 2
	done
	key="${producer}|${version}|${kind}|${scope}|"
	if ! awk -v key="${key}" 'index($0, key) == 1 { found = 1 } END { exit !found }' "${state}/grants"; then
		printf 'Error: grant_not_found: no producer grant for revocation\n' >&2
		exit 1
	fi
	awk -F'|' -v OFS='|' -v key="${key}" 'index($0, key) == 1 { $5 = "true" } { print }' \
		"${state}/grants" >"${state}/grants.new"
	mv "${state}/grants.new" "${state}/grants"
	printf 'revoked grant %s@%s kind %s scope %s\n' "${producer}" "${version}" "${kind}" "${scope}"
	;;
grants)
	producer=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--producer) producer="$2" ;;
		esac
		shift 2
	done
	awk -F'|' -v want="${producer}" '
		want == "" || $1 == want {
			printf "%s\t%s\t%s\tscope=%s\tschemas=1.0.0\texpires=2027-01-01T00:00:00Z\trevoked=%s\n", $1, $2, $3, $4, $5
		}' "${state}/grants"
	;;
*)
	printf 'fake eshu: unsupported component subcommand %s\n' "${sub}" >&2
	exit 64
	;;
esac
