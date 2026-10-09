#!/usr/bin/env bash
# Append selected, renderer-derived route rows to an existing work-budget table.
# The caller establishes GREEN report provenance; this helper checks table shape
# and keeps every existing byte, including the default and old route budgets.
set -euo pipefail
export LC_ALL=C

die() { printf 'extend-read-api-work-budgets: %s\n' "$*" >&2; exit 2; }
baseline=''
rendered=''
provenance=''
out=''
routes=()
while [[ $# -gt 0 ]]; do
	case "$1" in
		--baseline) [[ $# -ge 2 ]] || die '--baseline requires a file'; baseline="$2"; shift 2 ;;
		--rendered) [[ $# -ge 2 ]] || die '--rendered requires a file'; rendered="$2"; shift 2 ;;
		--route) [[ $# -ge 2 ]] || die '--route requires a key'; routes+=("$2"); shift 2 ;;
		--provenance) [[ $# -ge 2 ]] || die '--provenance requires text'; provenance="$2"; shift 2 ;;
		--out) [[ $# -ge 2 ]] || die '--out requires a file'; out="$2"; shift 2 ;;
		*) die "unknown argument: $1" ;;
	esac
done
[[ -f "${baseline}" && -f "${rendered}" ]] || die 'both table files are required'
[[ ${#routes[@]} -gt 0 ]] || die 'at least one --route is required'
[[ -n "${provenance}" && "${provenance}" != *$'\n'* && "${provenance}" != *$'\r'* ]] || die 'single-line provenance is required'
[[ -z "${out}" || "${out}" != "${baseline}" ]] || die 'output must differ from baseline'
for route in "${routes[@]}"; do
	[[ -n "${route}" && "${route}" != default && "${route}" != *$'\n'* && "${route}" != *$'\r'* && "${route}" != *$'\t'* ]] || die 'route must be one non-default table key'
done

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
printf '%s\n' "${routes[@]}" >"${work}/routes"
LC_ALL=C sort "${work}/routes" >"${work}/selected"
[[ "$(wc -l <"${work}/selected")" -eq "$(LC_ALL=C sort -u "${work}/selected" | wc -l)" ]] || die 'duplicate selected route'
awk -F '\t' '
	NF && $1 !~ /^#/ {
		if (NF < 4 || $1 == "" || $2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/ || $4 !~ /^[0-9]+$/ || seen[$1]++) exit 1
		if ($1 == "default") default_count++
	}
	END { if (default_count != 1) exit 1 }
' "${baseline}" || die 'malformed or duplicate baseline row'
awk -F '\t' -v selected="${work}/selected" '
	BEGIN { while ((getline route < selected) > 0) { if (route == "" || route == "default") exit 1; wanted[route] = 1; need++ } }
	FILENAME == ARGV[1] && NF && $1 !~ /^#/ { if ($1 in wanted) exit 1; next }
	FILENAME == ARGV[2] && NF && $1 !~ /^#/ {
		if (!($1 in wanted)) next
		if (++found[$1] != 1 || NF != 5 || $2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/ || $4 !~ /^[0-9]+$/ || $5 != "GREEN-derived work guard") exit 1
		print $0
	}
	END { for (route in wanted) if (found[route] != 1) exit 1 }
' "${baseline}" "${rendered}" >"${work}/rows" || die 'selected route missing, duplicated, malformed, or already present'
LC_ALL=C sort -t $'\t' -k1,1 "${work}/rows" >"${work}/sorted"
[[ "$(wc -l <"${work}/sorted")" -eq ${#routes[@]} ]] || die 'selected row count mismatch'

result="${work}/result"
cat "${baseline}" >"${result}"
[[ "$(tail -c 1 "${baseline}" | wc -l)" -eq 1 ]] || die 'baseline must end with a newline'
printf '\n# Added rows from %s; existing budgets retained byte-for-byte.\n' "${provenance}" >>"${result}"
cat "${work}/sorted" >>"${result}"
if [[ -n "${out}" ]]; then
	[[ -d "$(dirname "${out}")" ]] || die 'output directory does not exist'
	tmp="$(mktemp "${out}.XXXXXX")"
	cp "${result}" "${tmp}"
	mv "${tmp}" "${out}"
else
	cat "${result}"
fi
