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
#      The advisory list is only trusted once the query output is recognized
#      (see check_query_envelope): empty, non-JSON or reshaped output is a
#      FAIL, never "no advisory".
#   3. FAIL on a replace with no replacement version (a local path): nothing
#      reviewable pins it -- except a nested module of the main module that
#      lives in this repo. When ALL of the following hold, the replace skips
#      the upstream query/differential above and instead gets
#      `govulncheck -scan package ./...` inside the replacement directory
#      (a failure there is a FAIL); otherwise the local-path FAIL stands:
#        - the replacement has no version and its path is relative (./ or ../);
#        - the replacement directory (absolutized by `go list`) lies under
#          `git rev-parse --show-toplevel`;
#        - <dir>/go.mod is git-tracked (absolute path: a cwd-relative
#          pathspec fails from the module directory);
#        - that go.mod's module line equals the replaced module path exactly;
#        - the replaced path is strictly under the main-module path: nested
#          only, so an in-repo-but-not-nested copy still fails closed;
#        - go.mod requires a version of the replaced module to look up.
# Nothing here is a hand-kept list: the modules come from `go list -m -json
# all` and the advisories from the database. A replaced module with no
# advisory passes with no tests required.
#
# Usage: govulncheck-replace-recheck.sh [module-dir]   (default: .)
# Needs: go, govulncheck, jq, git on PATH; network for the database. A missing
# tool, or a jq that fails, is a failure: this step exists to fail closed, so it
# never reports "0 replaced modules" because it could not read the list.
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

# Fail closed up front when a required tool is absent. A jq that is present but
# broken is caught where jq runs, below.
for tool in go govulncheck jq git; do
	if ! command -v "${tool}" >/dev/null 2>&1; then
		fail "required tool not found on PATH: ${tool}"
	fi
done
if [ "${failures}" -gt 0 ]; then
	exit 1
fi

# check_query_envelope proves a `govulncheck -mode query -format json` result
# is output this script understands before an empty advisory list may read as
# "no advisory". The workflow installs govulncheck@latest, so the schema can
# change under the script; without this check a changed shape would yield an
# empty list and a false green. The envelope is what a real run prints
# (scanner v1.6.0, protocol v1.0.0): a stream of top-level JSON objects, each
# keyed config, progress or osv, with exactly the config message naming the
# scanner, the query mode and a v1 protocol. Anything else is a failure.
# Returns 0 when recognized; otherwise prints the reason on stdout and returns 1.
check_query_envelope() {
	local file="$1" markers unknown
	if [ ! -s "${file}" ]; then
		echo "govulncheck produced no output"
		return 1
	fi
	if ! jq -e . "${file}" >/dev/null 2>"${tmp_root}/envelope.err"; then
		echo "govulncheck output is not valid JSON: $(head -c 200 "${tmp_root}/envelope.err")"
		return 1
	fi
	if ! markers="$(jq -n '[inputs | select(type == "object" and .config != null
		and .config.scanner_name == "govulncheck"
		and .config.scan_mode == "query"
		and ((.config.protocol_version // "") | startswith("v1.")))] | length' "${file}")"; then
		echo "jq failed while checking the govulncheck output envelope"
		return 1
	fi
	if [ "${markers}" != "1" ]; then
		echo "unrecognized govulncheck query output: want exactly one config message with scanner_name govulncheck, scan_mode query and protocol_version v1.x, found ${markers}; the output schema may have changed"
		return 1
	fi
	if ! unknown="$(jq -n -r '[inputs | if type == "object" then keys[] else "<non-object>" end
		| select(. != "config" and . != "progress" and . != "osv")] | unique | join(",")' "${file}")"; then
		echo "jq failed while checking the govulncheck message types"
		return 1
	fi
	if [ -n "${unknown}" ]; then
		echo "unknown message type in govulncheck query output (${unknown}); the output schema may have changed"
		return 1
	fi
}

# recheck_advisory proves one advisory against one replaced module.
recheck_advisory() {
	local path="$1" version="$2" id="$3" query_out="$4"
	local fixed pattern files pkgs dir

	# The ID becomes part of a regex and a go test -run pattern: accept only the
	# Go vulnerability database shape.
	if ! [[ "${id}" =~ ^GO-[0-9]{4}-[0-9]+$ ]]; then
		fail "advisory ID '${id}' for ${path}@${version} is not of the form GO-YYYY-NNNN"
		return
	fi

	if ! fixed="$(jq -r --arg id "${id}" --arg path "${path}" \
		'select(.osv != null and .osv.id == $id) | .osv.affected[]? | select(.package.name == $path) | .ranges[]? | .events[]? | .fixed // empty' \
		"${query_out}" | sort -u | paste -sd, -)"; then
		fail "jq failed while reading the fixed versions of ${id} for ${path}@${version}"
		return
	fi
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

# is_nested_module_replace reports whether a version-less (local) replace
# points at a nested module of the main module that lives in this repo.
# Every condition must hold; a missing main path, a non-relative replacement,
# a directory outside the repo, an untracked go.mod, a module line that does
# not name the replaced module exactly, or a replaced path that is not
# strictly under the main module all report false and keep the local-path
# FAIL. Needs: git, awk on PATH (git is checked up front; awk is POSIX).
is_nested_module_replace() {
	local path="$1" repl_path="$2" repl_dir="$3" top nested_go_mod nested_mod
	[ -n "${main_path}" ] || return 1
	case "${repl_path}" in
	./* | ../*) ;;
	*) return 1 ;;
	esac
	[ -n "${repl_dir}" ] || return 1
	top="$(git rev-parse --show-toplevel 2>/dev/null)" || return 1
	case "${repl_dir}/" in
	"${top}/"*) ;;
	*) return 1 ;;
	esac
	nested_go_mod="${repl_dir}/go.mod"
	git ls-files --error-unmatch "${nested_go_mod}" >/dev/null 2>&1 || return 1
	nested_mod="$(awk '$1 == "module" { print $2; exit }' "${nested_go_mod}")" || return 1
	[ "${nested_mod}" = "${path}" ] || return 1
	case "${path}" in
	"${main_path}/"*) ;;
	*) return 1 ;;
	esac
	return 0
}

# Write the replaced-module list to a file instead of feeding the loop through a
# process substitution: set -e cannot see a failed `< <(jq ...)`, which would
# read as zero replaced modules and pass.
replaced_tsv="${tmp_root}/replaced.tsv"
if ! jq -r 'select(.Replace != null) | [.Path, (.Version // "-"), .Replace.Path, (.Replace.Version // "-"), (.Replace.Dir // "-")] | @tsv' \
	"${modules_json}" >"${replaced_tsv}"; then
	fail "jq failed while listing replaced modules from go list -m -json all"
	exit 1
fi

# The main-module path bounds the nested-module exemption below: only a
# replaced path strictly under it may take the nested branch. This runs after
# the replaced-listing gate so a broken jq still reports that first failure.
if ! main_path="$(jq -r '[select(.Main == true) | .Path][0] // empty' "${modules_json}")"; then
	fail "jq failed while reading the main module path from go list -m -json all"
	exit 1
fi

replaced_count=0
while IFS=$'\t' read -r path version repl_path repl_version repl_dir; do
	[ -n "${path}" ] || continue
	# jq emits "-" for an empty field: a tab-separated read collapses empty ones.
	[ "${version}" != "-" ] || version=""
	[ "${repl_version}" != "-" ] || repl_version=""
	[ "${repl_dir}" != "-" ] || repl_dir=""
	replaced_count=$((replaced_count + 1))
	if [ -z "${repl_version}" ]; then
		if [ -n "${version}" ] && is_nested_module_replace "${path}" "${repl_path}" "${repl_dir}"; then
			scan_out="${tmp_root}/nested-${replaced_count}.out"
			if ! (cd "${repl_dir}" && govulncheck -scan package ./...) >"${scan_out}" 2>&1; then
				cat "${scan_out}" >&2
				fail "${path}: govulncheck -scan package ./... failed inside the nested-module replacement (${repl_dir}); the replacement is not clean"
				continue
			fi
			note "OK ${path}@${version} (nested-module replacement at ${repl_path}): govulncheck -scan package ./... is clean"
			continue
		fi
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
	if ! envelope_err="$(check_query_envelope "${query_out}")"; then
		fail "${path}@${version}: ${envelope_err}"
		continue
	fi
	if ! ids="$(jq -r 'select(.osv != null) | .osv.id' "${query_out}" | sort -u)"; then
		fail "jq failed while reading the advisories for ${path}@${version}"
		continue
	fi
	if [ -z "${ids}" ]; then
		note "OK ${path}@${version} (replaced by ${repl_path}@${repl_version}): no advisory against the original module"
		continue
	fi
	while IFS= read -r id; do
		recheck_advisory "${path}" "${version}" "${id}" "${query_out}"
	done <<<"${ids}"
done <"${replaced_tsv}"

if [ "${failures}" -gt 0 ]; then
	printf 'govulncheck-replace-recheck: %d failure(s) across %d replaced module(s)\n' "${failures}" "${replaced_count}" >&2
	exit 1
fi
note "all ${replaced_count} replaced module(s) covered"
