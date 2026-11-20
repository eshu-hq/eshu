#!/usr/bin/env bash
# Verify Go coverage floors (Refs eshu-hq/eshu#10983, part of #9315).
#
# Hermetic: reads a `go test -covermode=count` profile plus
# specs/coverage-floors.v1.yaml, runs no tests, needs no network, Docker, or
# credentials. Two modes:
#
# - Scheduled (no --changed): fails when the whole-module canonical percent
#   drops below whole_module_floor, and prints the headroom owners use to
#   raise the rising floor toward 90.
# - PR path (--changed <csv import paths>): fails when a changed package with
#   a floor entry measures below it. Changed packages with no entry are
#   record-only (pass with notice); env_gated entries and packages absent
#   from the profile are skipped with notice, so the job stays hermetic.
#
# --ranking-out <file> writes the published package ranking: every measured
# package sorted by coverage ascending as
# "<pct_int>\t<statements>\t<covered>\t<import-path>".
#
# Exit codes: 0 pass, 1 floor violation, 2 usage or config error.
#
# Self-test: scripts/test-verify-coverage-floors.sh (hermetic RED/GREEN).
set -euo pipefail

profile=""
floors=""
changed=""
ranking_out=""

usage() {
	printf 'usage: verify-coverage-floors.sh --profile <cover.out> --floors <floors.yaml> [--changed <pkg,pkg>] [--ranking-out <file>]\n' >&2
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--profile)
		profile="${2:-}"
		shift 2
		;;
	--floors)
		floors="${2:-}"
		shift 2
		;;
	--changed)
		changed="${2:-}"
		shift 2
		;;
	--ranking-out)
		ranking_out="${2:-}"
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

if [[ -z "${profile}" || -z "${floors}" ]]; then
	usage
	exit 2
fi
if [[ ! -f "${profile}" ]]; then
	printf 'verify-coverage-floors: profile not found: %s\n' "${profile}" >&2
	exit 2
fi
if [[ ! -f "${floors}" ]]; then
	printf 'verify-coverage-floors: floors file not found: %s\n' "${floors}" >&2
	exit 2
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

# --- Parse the floors file (strict shape; see its header). ---
whole_floor="$(sed -n 's/^whole_module_floor: \([0-9][0-9]*\)$/\1/p' "${floors}" | head -n 1)"
if [[ -z "${whole_floor}" ]]; then
	printf 'verify-coverage-floors: %s has no whole_module_floor line\n' "${floors}" >&2
	exit 2
fi

env_list="${tmp_root}/env-gated.txt"
floor_map="${tmp_root}/floors.tsv"
: >"${env_list}"
: >"${floor_map}"
section=""
line_no=0
while IFS= read -r line || [[ -n "${line}" ]]; do
	line_no=$((line_no + 1))
	case "${line}" in
	\#* | "")
		continue
		;;
	env_gated:)
		section="env"
		continue
		;;
	packages:)
		section="packages"
		continue
		;;
	schema_version:* | whole_module_floor:*)
		section=""
		continue
		;;
	esac
	if [[ "${section}" == "env" ]]; then
		if [[ "${line}" =~ ^\ \ -\ ([^[:space:]]+)$ ]]; then
			printf '%s\n' "${BASH_REMATCH[1]}" >>"${env_list}"
		else
			printf 'verify-coverage-floors: %s line %d: bad env_gated entry: %s\n' "${floors}" "${line_no}" "${line}" >&2
			exit 2
		fi
	elif [[ "${section}" == "packages" ]]; then
		if [[ "${line}" =~ ^\ \ ([^[:space:]]+):\ ([0-9]+)$ ]]; then
			printf '%s\t%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" >>"${floor_map}"
		else
			printf 'verify-coverage-floors: %s line %d: bad packages entry: %s\n' "${floors}" "${line_no}" "${line}" >&2
			exit 2
		fi
	else
		printf 'verify-coverage-floors: %s line %d: entry outside a section: %s\n' "${floors}" "${line_no}" "${line}" >&2
		exit 2
	fi
done <"${floors}"

# --- Aggregate the profile per package (exclusions identical to
# scripts/generate-code-coverage-report.sh; keep the two in sync). ---
pkg_stats="${tmp_root}/pkg-stats.tsv"
LC_ALL=C awk '
function excluded(path) {
	return path ~ /\/testdata\// ||
		path ~ /\/fixtures?\// ||
		path ~ /\/mocks?\// ||
		path ~ /\/vendor\// ||
		path ~ /\/docs\/site\// ||
		path ~ /\.pb\.go$/ ||
		path ~ /\.gen\.go$/ ||
		path ~ /zz_generated/
}
NR == 1 { next }
{
	colon = index($0, ":")
	if (colon == 0) {
		next
	}
	file = substr($0, 1, colon - 1)
	rest = substr($0, colon + 1)
	split(rest, fields, " ")
	statements = fields[2] + 0
	count = fields[3] + 0
	if (statements <= 0) {
		next
	}
	if (excluded(file)) {
		next
	}
	pkg = file
	sub("/[^/]+$", "", pkg)
	total[pkg] += statements
	if (count > 0) {
		covered[pkg] += statements
	}
}
END {
	for (pkg in total) {
		pct = total[pkg] == 0 ? 0 : int(covered[pkg] * 100 / total[pkg])
		printf "%s\t%d\t%d\t%d\n", pkg, total[pkg], covered[pkg] + 0, pct
	}
}' "${profile}" >"${pkg_stats}"

if [[ -n "${ranking_out}" ]]; then
	LC_ALL=C sort -t "$(printf '\t')" -k4,4n -k1,1 "${pkg_stats}" |
		awk -F'\t' '{ printf "%d\t%d\t%d\t%s\n", $4, $2, $3, $1 }' >"${ranking_out}"
	printf 'verify-coverage-floors: ranking written: %s\n' "${ranking_out}"
fi

lookup() {
	local want="$1" file="$2" col="$3"
	awk -F'\t' -v want="${want}" -v col="${col}" '$1 == want { print $col; found=1; exit } END { if (!found) exit 1 }' "${file}" 2>/dev/null
}

failures=0

if [[ -z "${changed}" ]]; then
	# Scheduled mode: whole-module floor.
	all_total=0
	all_covered=0
	while IFS="$(printf '\t')" read -r _pkg stmts cov _pct; do
		all_total=$((all_total + stmts))
		all_covered=$((all_covered + cov))
	done <"${pkg_stats}"
	if [[ "${all_total}" -le 0 ]]; then
		printf 'verify-coverage-floors: profile holds no statements\n' >&2
		exit 2
	fi
	canonical=$((all_covered * 100 / all_total))
	if [[ "${canonical}" -lt "${whole_floor}" ]]; then
		printf 'verify-coverage-floors: FAIL whole-module %d%% below floor %d%%\n' "${canonical}" "${whole_floor}" >&2
		failures=$((failures + 1))
	else
		printf 'verify-coverage-floors: whole-module %d%% >= floor %d%% (headroom +%d; raise the floor toward 90 when headroom persists)\n' \
			"${canonical}" "${whole_floor}" "$((canonical - whole_floor))"
	fi
else
	# PR mode: per-package floors for changed packages only.
	old_ifs="${IFS}"
	IFS=","
	# shellcheck disable=SC2206
	changed_pkgs=(${changed})
	IFS="${old_ifs}"
	for pkg in "${changed_pkgs[@]}"; do
		[[ -n "${pkg}" ]] || continue
		if grep -Fxq -- "${pkg}" "${env_list}"; then
			printf 'verify-coverage-floors: SKIP %s is env_gated (hermetic job does not test it)\n' "${pkg}"
			continue
		fi
		measured="$(lookup "${pkg}" "${pkg_stats}" 4 || true)"
		if [[ -z "${measured}" ]]; then
			printf 'verify-coverage-floors: SKIP %s has no statements in this profile\n' "${pkg}"
			continue
		fi
		floor="$(lookup "${pkg}" "${floor_map}" 2 || true)"
		if [[ -z "${floor}" ]]; then
			printf 'verify-coverage-floors: NOTICE %s measures %d%% with no floor entry (record-only; add one from an origin/main profile)\n' "${pkg}" "${measured}"
			continue
		fi
		if [[ "${measured}" -lt "${floor}" ]]; then
			printf 'verify-coverage-floors: FAIL %s measures %d%% below floor %d%%\n' "${pkg}" "${measured}" "${floor}" >&2
			failures=$((failures + 1))
		else
			printf 'verify-coverage-floors: ok %s %d%% >= floor %d%%\n' "${pkg}" "${measured}" "${floor}"
		fi
	done
fi

if [[ "${failures}" -ne 0 ]]; then
	printf 'verify-coverage-floors: %d floor violation(s)\n' "${failures}" >&2
	exit 1
fi
printf 'verify-coverage-floors: all floors hold\n'
