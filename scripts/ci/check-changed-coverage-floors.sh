#!/usr/bin/env bash
# PR-path coverage-floors check (Refs eshu-hq/eshu#10983, part of #9315).
#
# Finds Go packages changed since a base revision, runs their tests once with
# a coverage profile, and checks the result against
# specs/coverage-floors.v1.yaml via scripts/verify-coverage-floors.sh.
# Hermetic by construction: env_gated packages (live DSN, deploy checkout)
# are never tested here, and a PR with no Go changes passes without running
# anything. Unit tests for changed packages still run in the go-core lanes;
# this job only judges the coverage delta.
#
# Usage: check-changed-coverage-floors.sh [--base-rev <git-rev>]
# Env overrides (CI defaults shown):
#   ESHU_FLOORS_BASE_REV  origin/<base-ref|main>; --base-rev wins when given
#   ESHU_FLOORS_BASE_REF  ${github.base_ref} from the workflow (falls back to
#                         $GITHUB_BASE_REF, then main on push events)
#   ESHU_FLOORS_VERIFIER  $repo_root/scripts/verify-coverage-floors.sh
#   ESHU_FLOORS_FILE      $repo_root/specs/coverage-floors.v1.yaml
#
# Exit codes: 0 floors hold (or nothing to check), 1 floor violation or test
# failure, 2 usage or config error.
#
# Self-test: scripts/ci/test-check-changed-coverage-floors.sh (temp git repo
# plus a stub `go`; no network, no real test run).
set -euo pipefail

base_rev=""

usage() {
	printf 'usage: check-changed-coverage-floors.sh --base-rev <git-rev>\n' >&2
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--base-rev)
		base_rev="${2:-}"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage
		exit 2
		;;
	esac
done

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
	printf 'check-changed-coverage-floors: not inside a git checkout\n' >&2
	exit 2
fi

if [[ -z "${base_rev}" ]]; then
	if [[ -n "${ESHU_FLOORS_BASE_REV:-}" ]]; then
		base_rev="${ESHU_FLOORS_BASE_REV}"
	else
		# The reusable workflow passes ${{ github.base_ref }} through
		# ESHU_FLOORS_BASE_REF; on push events that context is empty, so
		# fall back to the runner env and then to main.
		base_ref="${ESHU_FLOORS_BASE_REF:-${GITHUB_BASE_REF:-}}"
		if [[ -z "${base_ref}" ]]; then
			base_ref="main"
		fi
		base_rev="origin/${base_ref}"
	fi
fi

verifier="${ESHU_FLOORS_VERIFIER:-${repo_root}/scripts/verify-coverage-floors.sh}"
floors="${ESHU_FLOORS_FILE:-${repo_root}/specs/coverage-floors.v1.yaml}"
for f in "${verifier}" "${floors}" "${repo_root}/go/go.mod"; do
	if [[ ! -f "${f}" ]]; then
		printf 'check-changed-coverage-floors: required file missing: %s\n' "${f}" >&2
		exit 2
	fi
done

if ! git -C "${repo_root}" rev-parse --verify --quiet "${base_rev}^{commit}" >/dev/null; then
	printf 'check-changed-coverage-floors: base revision not found: %s\n' "${base_rev}" >&2
	exit 2
fi

module="$(awk '/^module /{ print $2; exit }' "${repo_root}/go/go.mod")"
if [[ -z "${module}" ]]; then
	printf 'check-changed-coverage-floors: no module line in go/go.mod\n' >&2
	exit 2
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

# Changed Go files -> deduplicated package dirs relative to go/ (a file
# directly under go/ maps to the module root, printed as ".").
git -C "${repo_root}" diff --name-only "${base_rev}" HEAD -- go/ |
	grep '\.go$' |
	sed -e 's|^go/||' |
	while IFS= read -r f || [[ -n "${f}" ]]; do
		if [[ "${f}" == */* ]]; then
			printf '%s\n' "${f%/*}"
		else
			printf '.\n'
		fi
	done |
	LC_ALL=C sort -u >"${tmp_root}/rel-dirs.txt" || true

if [[ ! -s "${tmp_root}/rel-dirs.txt" ]]; then
	printf 'check-changed-coverage-floors: no Go files changed; nothing to check\n'
	exit 0
fi

# env_gated import paths, parsed with the verifier's strict shape.
sed -n '/^env_gated:$/,/^packages:$/p' "${floors}" |
	sed -n 's/^  - //p' >"${tmp_root}/gated.txt"

test_pkgs=()
changed_pkgs=()
while IFS= read -r reldir || [[ -n "${reldir}" ]]; do
	[[ -n "${reldir}" ]] || continue
	if [[ "${reldir}" == "." ]]; then
		import="${module}"
		relpkg="."
	else
		import="${module}/${reldir}"
		relpkg="./${reldir}"
	fi
	if grep -Fxq -- "${import}" "${tmp_root}/gated.txt"; then
		printf 'check-changed-coverage-floors: SKIP %s is env_gated (hermetic job does not test it)\n' "${import}"
		continue
	fi
	test_pkgs+=("${relpkg}")
	changed_pkgs+=("${import}")
done <"${tmp_root}/rel-dirs.txt"

if [[ "${#test_pkgs[@]}" -eq 0 ]]; then
	printf 'check-changed-coverage-floors: only env_gated packages changed; nothing to check\n'
	exit 0
fi

profile="${tmp_root}/changed-coverage.out"
(
	cd "${repo_root}/go" &&
		go test -count=1 -covermode=count -coverprofile="${profile}" "${test_pkgs[@]}"
)

csv="$(IFS=,; printf '%s' "${changed_pkgs[*]}")"
bash "${verifier}" --profile "${profile}" --floors "${floors}" --changed "${csv}"
