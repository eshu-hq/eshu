#!/usr/bin/env bash
# refresh-read-api-work-budgets.sh - render
# testdata/benchmarks/read-api-route-work-budgets.txt from GREEN work reports.
#
# The read-api latency gate (go/cmd/read-api-latency-gate) writes a per-route
# Postgres work report with -work-report. This script turns one or more GREEN
# reports (the same code that must pass, run on the runner class the gate
# enforces on) into the budget table, using the formulas below over the
# MAX of each counter across reports and routes:
#
#   calls = ceil(max_calls * 1.25) + 5   (+5 absorbs one added statement; a real
#                                         N+1 at 800 scopes is +800)
#   blks  = ceil(max_blks  * 3.0)        (absorbs ANALYZE sampling and minor plan
#                                         drift; a nested-loop flip is >= 10x)
#   rows  = ceil(max_rows  * 2.0)
#
# Named rows are the routes explicitly listed in the latency budget table
# (the SLO-critical routes) that appear in the reports; the default row applies
# the same formulas to the max over every other exercised route.
#
# FLOOR: no named row is rendered below the default row in any counter. Without it
# a route that reads almost nothing renders a budget of 0 rows and 3 buffers, and
# the meter window (which sums the whole database, not one route) turns a single
# stray statement into a blocking breach. The default row is already the formulas
# over the largest reading of the routes that read almost nothing (47 routes at
# most 8 buffers; across the three GREEN artifacts and the RED artifact their maximum
# is 6 calls, 7 buffers, 7 rows, identical in every report), so it is the measured
# noise scale with the same 1.25x+5 / 3x / 2x margins, not a chosen number. The floor
# never lifts a named row that reads real work: every changed status route's budget is
# over three orders of magnitude above it. Numbers are
# never edited by hand: re-run this script and commit the result.
#
# RATCHET: a GREEN-derived budget regenerated from a regressed run is not a
# regression guard. #6843 raised these two rows from 74739 to 663066 blks in the
# same PR that caused the 8.9x increase, and nothing objected, so the work gate
# kept passing while the route went from 145ms to 2.1s. This script now refuses
# to render any per-route counter above 1.5x the committed one unless the caller
# names the route and an issue with --accept-regression. All three are guarded:
# blks is where #6843 showed up, calls is where an N+1 would, rows is where a
# result explosion would. The threshold is 1.5x because the formula already
# carries a 3x margin over the measured GREEN value: a rendered row only moves at
# all when real work moved, and 1.5x of an already-3x-padded budget is well
# outside plan drift. Counters below 100 are skipped, because at that scale a
# ratio is noise (the default row's rows can go 2 -> 0 legitimately).
#
# The guard is two-directional. A COLLAPSE past 20x below the committed value is
# refused too, with --accept-drop: a budget that falls off a cliff disarms that
# route's guard silently, and unlike a regression nothing fails when it lands.
# Found on #6912, where reverting #6843 deleted the seed /cloud/inventory reads
# and dropped it 145851 -> 21 blks in a PR about two unrelated routes. 20x
# separates that from a real optimisation: #6912's own read-model fix drops
# these routes 8.2x and must pass.
#
# The 1.5x growth threshold is also
# calibrated to admit the fix for the regression it blocks: #6912's post-fix
# value for those two routes renders 81015, which is 1.08x the pre-#6843 74739
# and passes without a flag, while #6843's own 8.9x is refused.
#
# Usage:
#   scripts/refresh-read-api-work-budgets.sh [--named-from FILE] [--out FILE]
#                                            [--baseline FILE]
#                                            [--accept-regression 'ROUTE=#ISSUE']...
#                                            [--accept-drop 'ROUTE=#ISSUE']...
#                                            REPORT.json...
#     --named-from FILE  latency budget table naming the routes that get their own
#                        row (default: testdata/benchmarks/read-api-route-budgets.txt)
#     --out FILE         write the table to FILE (default: stdout)
#     --baseline FILE    committed table to ratchet against (default: --out when it
#                        exists, else testdata/benchmarks/read-api-route-work-budgets.txt).
#                        Pass /dev/null to render the first version of a new table.
#     --accept-regression 'ROUTE=#ISSUE'
#                        allow ROUTE's blks to exceed the 1.5x growth ratchet,
#                        recording why. Repeatable. The reason must name an issue.
#     --accept-drop 'ROUTE=#ISSUE'
#                        allow ROUTE's blks to fall past the 20x collapse ratchet.
#                        Repeatable. The reason must name an issue. A collapse is
#                        usually corpus loss, so check the seed before reaching
#                        for this.
#
# Output is byte-identical for identical inputs (LC_ALL=C, sorted by route, no
# timestamps).
set -euo pipefail
export LC_ALL=C

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
named_from="${repo_root}/testdata/benchmarks/read-api-route-budgets.txt"
out=""
baseline=""
baseline_set=0
# Newline-delimited "route<TAB>reason". A plain list, not an associative array:
# /bin/bash on macOS is 3.2 and has none.
accepted_regressions=""
accepted_drops=""
ratchet_numerator=3
ratchet_denominator=2
# A collapse this large is corpus loss, not an optimisation. 20x separates the
# two cases measured so far: #6912's read-model fix drops these routes 8.2x
# (663066 -> 81015) and must pass, while deleting the seed that feeds
# /cloud/inventory dropped it 6945x (145851 -> 21) and must not.
ratchet_drop_factor=20
# Below this, a ratio is noise rather than signal: the default row's rows can go
# 2 -> 0 legitimately, which is a "100% collapse" that means nothing. Every real
# signal is orders of magnitude above it (74739, 145851, 663066).
ratchet_floor=100

die() {
	printf 'refresh-read-api-work-budgets: %s\n' "$*" >&2
	exit "${2:-2}"
}

reports=()
while [[ $# -gt 0 ]]; do
	case "$1" in
		--named-from) named_from="${2:?--named-from needs a file}"; shift 2 ;;
		--out) out="${2:?--out needs a file}"; shift 2 ;;
		--baseline) baseline="${2:?--baseline needs a file}"; baseline_set=1; shift 2 ;;
		--accept-regression)
			entry="${2:?--accept-regression needs 'ROUTE=#ISSUE'}"
			case "${entry}" in
				*=*) ;;
				*) die "--accept-regression wants 'ROUTE=#ISSUE', got: ${entry}" ;;
			esac
			acc_route="${entry%%=*}"
			acc_reason="${entry#*=}"
			[[ -n "${acc_route}" ]] || die "--accept-regression has an empty route: ${entry}"
			[[ "${acc_reason}" =~ \#[0-9]+ ]] || die "--accept-regression needs a reason naming an issue (e.g. '#6912'), got: ${entry}"
			accepted_regressions="${accepted_regressions}${acc_route}	${acc_reason}
"
			shift 2 ;;
		--accept-drop)
			entry="${2:?--accept-drop needs 'ROUTE=#ISSUE'}"
			case "${entry}" in *=*) ;; *) die "--accept-drop wants 'ROUTE=#ISSUE', got: ${entry}" ;; esac
			drop_route="${entry%%=*}"
			drop_reason="${entry#*=}"
			[[ -n "${drop_route}" ]] || die "--accept-drop has an empty route: ${entry}"
			[[ "${drop_reason}" =~ \#[0-9]+ ]] || die "--accept-drop needs a reason naming an issue (e.g. '#6912'), got: ${entry}"
			accepted_drops="${accepted_drops}${drop_route}\t${drop_reason}\n"
			shift 2 ;;
		-h | --help) sed -n '2,75p' "${BASH_SOURCE[0]}"; exit 0 ;;
		-*) die "unknown flag: $1" ;;
		*) reports+=("$1"); shift ;;
	esac
done

command -v jq >/dev/null 2>&1 || die "missing required tool: jq"
[[ ${#reports[@]} -gt 0 ]] || die "at least one GREEN work report is required"
[[ -f "${named_from}" ]] || die "named-from file not found: ${named_from}"

for report in "${reports[@]}"; do
	[[ -f "${report}" ]] || die "report not found: ${report}"
	jq -e '
		(.routes | type == "array") and
		all(.routes[]; (.route | type == "string")
			and (.work.calls | type == "number")
			and (.work.blks | type == "number")
			and (.work.rows | type == "number"))
	' "${report}" >/dev/null 2>&1 || die "malformed work report (want {\"routes\":[{\"route\",\"work\":{calls,blks,rows}}]}): ${report}"
done

# column_for ROUTE FILE COL -- the committed counter in column COL (2=calls,
# 3=blks, 4=rows) for ROUTE, empty when absent.
column_for() {
	awk -F'\t' -v want="$1" -v col="$3" '
		/^[[:space:]]*#/ { next }
		$1 == want { print $col; exit }
	' "$2"
}

# reason_for LIST ROUTE -- the recorded reason for ROUTE in LIST, empty when none.
reason_for() {
	printf '%b' "$1" | awk -F'\t' -v want="$2" '
		$1 == want { print $2; exit }
	'
}

# ratchet BASELINE RENDERED -- refuse any per-route blks above the threshold over
# the committed value. Compares as integers (num*new > den*old) so no bc or
# floating point is involved. A route absent from the baseline is new and has
# nothing to ratchet against.
ratchet() {
	local baseline_file="$1" rendered="$2"
	# A first-ever render has no baseline, which is legitimate, and --baseline
	# /dev/null is the documented way to say so. A path that does not EXIST is a
	# typo, and silently skipping the ratchet on a typo is exactly the silent
	# disarming this guard exists to prevent -- so that case fails closed.
	if [[ "${baseline_set}" -eq 1 && ! -e "${baseline_file}" ]]; then
		die "--baseline not found: ${baseline_file}" 2
	fi
	[[ -s "${baseline_file}" ]] || return 0
	local violations="" collapses="" accepted_note="" route calls blks rows old reason
	local counter name rest col new
	while IFS=$'\t' read -r route calls blks rows _; do
		# The default row is the fallback budget for every unnamed route, so
		# inflating it disarms all of them at once -- the same class this guard
		# closes for named rows. It is ratcheted on the same thresholds.
		case "${route}" in "" | \#*) continue ;; esac
		# All three counters share the GREEN-regenerated failure class: blks is
		# where #6843 showed up, calls is where an N+1 would, rows is where a
		# result explosion would. Guarding one and not the others would leave the
		# same hole in two places.
		for counter in calls:2:"${calls}" blks:3:"${blks}" rows:4:"${rows}"; do
			name="${counter%%:*}"; rest="${counter#*:}"
			col="${rest%%:*}"; new="${rest#*:}"
			[[ "${new}" =~ ^[0-9]+$ ]] || continue
			old="$(column_for "${route}" "${baseline_file}" "${col}")"
			[[ "${old}" =~ ^[0-9]+$ ]] || continue
			(( old < ratchet_floor )) && continue
			if (( ratchet_denominator * new > ratchet_numerator * old )); then
				reason="$(reason_for "${accepted_regressions}" "${route}")"
				if [[ -n "${reason}" ]]; then
					accepted_note="${accepted_note}  ACCEPTED ${route}: ${old} -> ${new} ${name} (${reason})\n"
				else
					violations="${violations}  ${route}: ${old} -> ${new} ${name}\n"
				fi
			elif (( ratchet_drop_factor * new < old )); then
				reason="$(reason_for "${accepted_drops}" "${route}")"
				if [[ -n "${reason}" ]]; then
					accepted_note="${accepted_note}  ACCEPTED DROP ${route}: ${old} -> ${new} ${name} (${reason})\n"
				else
					collapses="${collapses}  ${route}: ${old} -> ${new} ${name}\n"
				fi
			fi
		done
	done < <(printf '%s\n' "${rendered}")
	[[ -n "${accepted_note}" ]] && printf 'refresh-read-api-work-budgets: accepted regression(s):\n%b' "${accepted_note}" >&2
	if [[ -n "${collapses}" ]]; then
		printf 'refresh-read-api-work-budgets: RATCHET: per-route work COLLAPSED past %sx below the committed budget:\n%b' \
			"${ratchet_drop_factor}" "${collapses}" >&2
		printf '%s\n' \
			"A budget this much smaller usually means the corpus shrank, not that the route" \
			"got faster -- deleting a seed that another route reads is the way this happens." \
			"It disarms that route's guard silently: nothing fails now, and the next PR to" \
			"touch the route breaches for no visible reason." \
			"" \
			"Check the seed before the budget. If the drop is a real optimisation, re-run with" \
			"  --accept-drop 'ROUTE=#ISSUE'" >&2
		exit 3
	fi
	if [[ -n "${violations}" ]]; then
		printf 'refresh-read-api-work-budgets: RATCHET: per-route work grew past %s/%sx the committed budget:\n%b' \
			"${ratchet_numerator}" "${ratchet_denominator}" "${violations}" >&2
		printf '%s\n' \
			"The work budget is the gate that catches this. Regenerating it from a run that" \
			"already regressed is how #6843 took /infra/resources/{count,inventory} from 145ms" \
			"to 2.1s with every check green." \
			"" \
			"Fix the regression, or, if the new cost is genuinely intended, re-run with" \
			"  --accept-regression 'ROUTE=#ISSUE'" \
			"for each route above and say in the PR why the work grew." >&2
		exit 3
	fi
	return 0
}

named_json="$(awk -F'\t' '!/^[[:space:]]*#/ && NF >= 2 && $1 != "default" && $1 != "" {print $1}' "${named_from}" | jq -R . | jq -s .)"

table="$(jq -s -r --argjson named "${named_json}" --arg n "${#reports[@]}" '
	def ceil5: (. * 1.25 | ceil) + 5;
	def per_route: [.[] | .routes[]] | group_by(.route)
		| map({
			route: .[0].route,
			calls: (map(.work.calls) | max),
			blks: (map(.work.blks) | max),
			rows: (map(.work.rows) | max)
		});
	def row($r): "\($r.calls | ceil5)\t\($r.blks * 3 | ceil)\t\($r.rows * 2 | ceil)";
	def floored($r; $f):
		"\([($r.calls | ceil5), $f.calls] | max)\t\([($r.blks * 3 | ceil), $f.blks] | max)\t\([($r.rows * 2 | ceil), $f.rows] | max)";
	def max_of($k): map(.[$k]) | max;

	per_route as $all
	| ($all | map(select(.route as $x | $named | index($x)))) as $named_rows
	| ($all | map(select(.route as $x | ($named | index($x)) | not))) as $unnamed
	| if ($unnamed | length) == 0 then error("no unnamed exercised route to derive the default row from") else . end
	| ({calls: ($unnamed | max_of("calls")), blks: ($unnamed | max_of("blks")), rows: ($unnamed | max_of("rows"))}) as $d
	| ({calls: ($d.calls | ceil5), blks: ($d.blks * 3 | ceil), rows: ($d.rows * 2 | ceil)}) as $floor
	| "# read-api-route-work-budgets.txt -- GENERATED by scripts/refresh-read-api-work-budgets.sh",
	  "# from \($n) GREEN work report(s). Do not edit by hand: re-run the script and commit the result.",
	  "#",
	  "# Per-request Postgres work budgets for the read-API latency gate. A route breaches when ANY",
	  "# counter exceeds its budget. Formulas over the max GREEN value: calls = ceil(max*1.25)+5,",
	  "# blks = ceil(max*3.0), rows = ceil(max*2.0). No named row is below the default row.",
	  "",
	  "default\t\(row($d))",
	  "",
	  ($named_rows | sort_by(.route)[] | "\(.route)\t\(floored(.; $floor))\tGREEN-derived work guard")
' "${reports[@]}")" || die "could not render the budget table from the reports" 3

# Default baseline: the file being overwritten when --out names one, else the
# committed table. --baseline /dev/null renders a brand-new table.
if [[ "${baseline_set}" -eq 0 ]]; then
	if [[ -n "${out}" && -f "${out}" ]]; then
		baseline="${out}"
	else
		baseline="${repo_root}/testdata/benchmarks/read-api-route-work-budgets.txt"
	fi
fi

ratchet "${baseline}" "${table}"

if [[ -n "${out}" ]]; then
	tmp="$(mktemp "${out}.XXXXXX")"
	printf '%s\n' "${table}" >"${tmp}"
	mv "${tmp}" "${out}"
else
	printf '%s\n' "${table}"
fi
