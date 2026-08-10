#!/usr/bin/env bash
#
# verify-remote-validation-artifacts.sh - fail a production-supported
# remote_validation proof-ID that lacks valid deployed evidence and is not
# tracked in the burn-down baseline (#5407, #5552).
# #5336 flagged component_extensions.{inventory,diagnostics} as
# claiming production:supported on an unverifiable remote_validation ref;
# this gate generalizes that finding across the whole matrix instead of
# fixing it in isolation.
#
# The check itself lives in Go (go/internal/capabilitycatalog/
# remote_validation.go, CheckRemoteValidationArtifacts), parallel to the
# capability-budget-proof artifact loader (budget_proof.go): this script is a
# thin wrapper around `go run ./cmd/capability-inventory -mode
# remote-validation`, mirroring verify-capability-budget-proof.sh's shape.
#
# A remote_validation ref resolves against
# docs/internal/remote-validation/<ref>.md. Its content must record a deployed
# source, run date, exact command with direct exit capture, exit 0, and one
# assertion per production capability. Invalid evidence passes only if its ref
# is listed in specs/remote-validation-baseline.txt. The baseline carries a
# FROZEN_MAX ceiling: the gate fails when the entry count EXCEEDS it, so the
# debt set cannot grow. Run
# -update after committing a real evidence artifact (which shrinks the set and
# ratchets FROZEN_MAX down); -update never raises the ceiling. Smuggling a new
# unverified production:supported claim by appending its ref and regenerating
# leaves the count above the ceiling and fails the gate — the offender must
# commit an artifact or raise FROZEN_MAX in an explicit, reviewed one-line edit.
# The ceiling alone cannot catch a constant-count atomic swap (burning down one
# baselined ref while adding another), so the baseline is also bounded by the
# immutable frozen membership set specs/remote-validation-frozen.txt: the gate
# enforces baseline ⊆ frozen and fails closed if that file is missing or
# malformed. -update never writes the frozen set.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
specs="${repo_root}/specs"
root="${repo_root}"
baseline="${repo_root}/specs/remote-validation-baseline.txt"
mode="check"

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/verify-remote-validation-artifacts.sh [-update] [--specs DIR] [--root DIR] [--baseline PATH]

Verifies every production-supported remote_validation proof-ID resolves to a
current-format deployed artifact or is listed in the burn-down baseline. With
-update, regenerates the baseline and slug-to-row inventory.
USAGE
}

die() {
	printf 'verify-remote-validation-artifacts: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

while (($# > 0)); do
	case "$1" in
	-update)
		mode="update"
		shift
		;;
	--specs)
		specs="${2:-}"
		shift 2
		;;
	--root)
		root="${2:-}"
		shift 2
		;;
	--baseline)
		baseline="${2:-}"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "unknown argument: $1"
		;;
	esac
done

require_tool go
[[ -d "${specs}" ]] || die "specs dir not found: ${specs}"
[[ -d "${root}" ]] || die "repo root not found: ${root}"
export GOCACHE="${GOCACHE:-${repo_root}/.gocache}"

if [[ "${mode}" == "update" ]]; then
	(
		cd "${repo_root}/go"
		go run ./cmd/capability-inventory \
			-mode remote-validation \
			-specs "${specs}" \
			-root "${root}" \
			-remote-validation-baseline "${baseline}" \
			-update
	)
else
	(
		cd "${repo_root}/go"
		go run ./cmd/capability-inventory \
			-mode remote-validation \
			-specs "${specs}" \
			-root "${root}" \
			-remote-validation-baseline "${baseline}"
	)
fi
