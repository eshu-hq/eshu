#!/usr/bin/env bash
#
# govulncheck-replace-recheck.sh - cover the modules that go.mod replaces.
#
# `govulncheck` looks advisories up by the REPLACEMENT module path (x/vuln
# internal/vulncheck/fetch.go), so a `replace` directive turns the stock scan
# green whether or not the replacement actually fixes anything. A replace that
# points at an UNPATCHED copy goes green too. This step closes that hole for
# the job that runs `govulncheck -scan package ./...` (#10212, GO-2026-6909).
#
# For every module that go.mod replaces:
#   1. Ask the vulnerability database about the ORIGINAL module at the version
#      go.mod requires:  govulncheck -mode query -format json <path>@<version>
#   2. For each advisory returned:
#        - FAIL when the advisory has a fixed version: upgrade and drop the
#          replace instead of keeping a fork.
#        - FAIL when no Go test is named for the advisory. The convention is
#          the advisory ID without dashes in a Test function name, for example
#          GO-2026-6909 -> TestXxxGO20266909.
#        - Run those tests with the real go.mod: they must PASS.
#        - Run them again with a temporary modfile that drops the replace
#          (the upstream module): they must FAIL with a test failure. That
#          differential is the proof that the replacement carries the fix.
#   3. FAIL on a replace with no replacement version (a local path): nothing
#      reviewable pins it.
# Nothing here is a hand-kept list: the modules come from `go list -m -json
# all` and the advisories from the database. A replaced module with no
# advisory passes with no tests required.
#
# Usage: govulncheck-replace-recheck.sh [module-dir]   (default: .)
# Needs: go, govulncheck, jq, git on PATH; network for the database.
# Exit status: 0 when every replaced module is covered, 1 otherwise, 64 on
# bad usage.
set -euo pipefail

if [ "$#" -gt 1 ]; then
	echo "usage: $0 [module-dir]" >&2
	exit 64
fi
module_dir="${1:-.}"
cd "${module_dir}"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

failures=0
fail() {
	printf 'govulncheck-replace-recheck: FAIL: %s\n' "$*" >&2
	failures=$((failures + 1))
}
note() {
	printf 'govulncheck-replace-recheck: %s\n' "$*"
}

# recheck_advisory proves one advisory against one replaced module.
recheck_advisory() {
	local path="$1" version="$2" id="$3" query_out="$4"
	local fixed pattern files pkgs dir

	fixed="$(jq -r --arg id "${id}" --arg path "${path}" \
		'select(.osv != null and .osv.id == $id) | .osv.affected[]? | select(.package.name == $path) | .ranges[]? | .events[]? | .fixed // empty' \
		"${query_out}" | sort -u | paste -sd, -)"
	if [ -n "${fixed}" ]; then
		fail "${id} affects ${path}@${version} and has a fixed version (${fixed}); upgrade and drop the replace instead of keeping a fork"
		return
	fi

	pattern="${id//-/}"
	files="$(git grep --untracked -l -E "func Test[A-Za-z0-9_]*${pattern}" -- '*_test.go' || true)"
	if [ -z "${files}" ]; then
		fail "${id} affects ${path}@${version} but no Go test is named for it (expected func Test*${pattern}*); a replace is not proof of a fix"
		return
	fi
	pkgs="$(printf '%s\n' "${files}" | while IFS= read -r file; do
		dir="$(dirname "${file}")"
		case "${dir}" in
		.) printf './\n' ;;
		*) printf './%s\n' "${dir}" ;;
		esac
	done | sort -u | paste -sd' ' -)"

	# GREEN: with the real go.mod (the replacement) the tests pass.
	# shellcheck disable=SC2086 # pkgs is a space-separated package list
	if ! go test -count=1 -run "${pattern}" ${pkgs} >"${tmp_root}/green.out" 2>&1; then
		cat "${tmp_root}/green.out" >&2
		fail "${id}: regression tests fail with the replacement in place (${pkgs})"
		return
	fi

	# RED: with the replace dropped (upstream module) the tests must fail as
	# tests. A build or download failure is not proof, so require a test
	# failure line in the output.
	local alt="${tmp_root}/alt-${pattern}"
	mkdir -p "${alt}"
	cp go.mod "${alt}/go.mod"
	cp go.sum "${alt}/go.sum"
	go mod edit -dropreplace="${path}" "${alt}/go.mod"
	# shellcheck disable=SC2086
	if GOFLAGS="-mod=mod" go test -count=1 -modfile="${alt}/go.mod" -run "${pattern}" ${pkgs} >"${tmp_root}/red.out" 2>&1; then
		fail "${id}: regression tests PASS on upstream ${path}@${version}; they do not prove the replacement carries the fix"
		return
	fi
	if ! grep -q -e '--- FAIL' "${tmp_root}/red.out"; then
		cat "${tmp_root}/red.out" >&2
		fail "${id}: dropping the replace broke the build instead of failing the tests, so the differential is unproven"
		return
	fi
	note "OK ${path}@${version} ${id}: tests pass with the replacement and fail on upstream"
}

modules_json="${tmp_root}/modules.json"
go list -m -json all >"${modules_json}"

replaced_count=0
while IFS=$'\t' read -r path version repl_path repl_version; do
	[ -n "${path}" ] || continue
	# jq emits "-" for an empty field: a tab-separated read collapses empty ones.
	[ "${version}" != "-" ] || version=""
	[ "${repl_version}" != "-" ] || repl_version=""
	replaced_count=$((replaced_count + 1))
	if [ -z "${repl_version}" ]; then
		fail "${path} is replaced by a local path (${repl_path}); pin a versioned fork so the replacement is reviewable"
		continue
	fi
	if [ -z "${version}" ]; then
		fail "${path} is replaced by ${repl_path}@${repl_version} but go.mod has no required version to look up"
		continue
	fi
	query_out="${tmp_root}/query-${replaced_count}.json"
	if ! govulncheck -mode query -format json "${path}@${version}" >"${query_out}" 2>"${tmp_root}/query.err"; then
		cat "${tmp_root}/query.err" >&2
		fail "govulncheck query failed for ${path}@${version}"
		continue
	fi
	ids="$(jq -r 'select(.osv != null) | .osv.id' "${query_out}" | sort -u)"
	if [ -z "${ids}" ]; then
		note "OK ${path}@${version} (replaced by ${repl_path}@${repl_version}): no advisory against the original module"
		continue
	fi
	while IFS= read -r id; do
		recheck_advisory "${path}" "${version}" "${id}" "${query_out}"
	done <<<"${ids}"
done < <(jq -r 'select(.Replace != null) | [.Path, (.Version // "-"), .Replace.Path, (.Replace.Version // "-")] | @tsv' "${modules_json}")

if [ "${failures}" -gt 0 ]; then
	printf 'govulncheck-replace-recheck: %d failure(s) across %d replaced module(s)\n' "${failures}" "${replaced_count}" >&2
	exit 1
fi
note "all ${replaced_count} replaced module(s) covered"
