#!/usr/bin/env bash
# compare-backend-latency.sh -- #6965 phase 6: render a markdown table
# comparing two read-api-latency-gate `-latency-report` JSON legs (schema
# version 1; see go/cmd/read-api-latency-gate/latency_report.go), each swept
# against a DIFFERENT backend (identity.backend) but otherwise the same
# corpus, binary, and run shape.
#
# Usage:
#   scripts/compare-backend-latency.sh [--out FILE] LEFT.json RIGHT.json
#     --out FILE  write the table to FILE (default: stdout)
#
# Refuses (exit 2, naming the field) to compare two reports whose identity
# disagrees in a field that must match for the comparison to mean anything:
# identity.seed_options (total_scopes/nodes_per_label/iac_fact_count/
# shared_intent_count), identity.iterations, identity.warmups,
# identity.eshu_commit, identity.api_binary_sha256. identity.backend is
# deliberately NOT in this list -- the two legs are expected to differ there,
# that is the whole point of the comparison.
#
# Also refuses (exit 2) when either leg's identity.runs < 3: the table
# compares WARM samples (runs 2..Runs; see RouteLatency.WarmSamples), and
# fewer than 2 warm runs cannot show the run-to-run p95 spread the report
# carries (LatencyReportWarmStats.run_p95_min_ms/run_p95_max_ms).
#
# A route whose measured HTTP status or per-request Postgres work
# (calls/blks/rows) differs between the two legs did different work on the
# two backends (e.g. a graph->Postgres fallback that only fires on one leg)
# and is listed as NON-COMPARABLE instead of having its latency compared -- a
# latency ratio between two different queries is not a backend comparison.
# Work counters are compared rounded to 2 decimal places: they are
# per-request averages (raw counter / (iterations*runs)) and are invariant to
# runs by construction, but binary floating point can still differ in the
# last bit between two independently computed averages of the same integer
# counters, and that is not the kind of "different work" this check exists
# to catch.
#
# No --benchstat mode: the schema (n/p50/p95/min/max/stddev per route) is
# already exactly what this script needs, and adding a benchfmt emitter plus
# a benchstat dependency was not cheap enough to justify for the first cut.
# A future PR can add one without changing this script's own output.
set -euo pipefail
export LC_ALL=C

die() {
	printf 'compare-backend-latency: %s\n' "$*" >&2
	exit "${2:-2}"
}

out=""
paths=()
while [[ $# -gt 0 ]]; do
	case "$1" in
		--out) out="${2:?--out needs a file}"; shift 2 ;;
		# Print the whole comment header, terminated by the first line of code.
		-h | --help) awk 'NR > 1 && /^#/ { print; next } NR > 1 { exit }' "${BASH_SOURCE[0]}"; exit 0 ;;
		-*) die "unknown flag: $1" ;;
		*) paths+=("$1"); shift ;;
	esac
done

command -v jq >/dev/null 2>&1 || die "missing required tool: jq"
[[ ${#paths[@]} -eq 2 ]] || die "usage: compare-backend-latency.sh [--out FILE] LEFT.json RIGHT.json"
left="${paths[0]}"
right="${paths[1]}"
[[ -f "${left}" ]] || die "left report not found: ${left}"
[[ -f "${right}" ]] || die "right report not found: ${right}"

for f in "${left}" "${right}"; do
	jq -e '
		(.schema_version == 1) and
		(.identity.backend | type == "string") and
		(.identity.runs | type == "number") and
		(.identity.iterations | type == "number") and
		(.identity.warmups | type == "number") and
		(.identity.seed_options | type == "object") and
		(.routes | type == "array")
	' "${f}" >/dev/null 2>&1 \
		|| die "not a schema-version-1 latency report (want {schema_version:1, identity:{backend,runs,iterations,warmups,seed_options}, routes:[...]}): ${f}"
done

left_runs="$(jq -r '.identity.runs' "${left}")"
right_runs="$(jq -r '.identity.runs' "${right}")"
[[ "${left_runs}" -ge 3 ]] || die "left report has identity.runs=${left_runs}, want >= 3 (need at least 2 warm runs to show run-to-run spread): ${left}"
[[ "${right_runs}" -ge 3 ]] || die "right report has identity.runs=${right_runs}, want >= 3 (need at least 2 warm runs to show run-to-run spread): ${right}"

# path:label pairs for the identity fields that must match exactly.
identity_fields=(
	"identity.seed_options.total_scopes:seed_options.total_scopes"
	"identity.seed_options.nodes_per_label:seed_options.nodes_per_label"
	"identity.seed_options.iac_fact_count:seed_options.iac_fact_count"
	"identity.seed_options.shared_intent_count:seed_options.shared_intent_count"
	"identity.iterations:iterations"
	"identity.warmups:warmups"
	"identity.eshu_commit:eshu_commit"
	"identity.api_binary_sha256:api_binary_sha256"
)
for entry in "${identity_fields[@]}"; do
	path="${entry%%:*}"
	label="${entry#*:}"
	lval="$(jq -r ".${path} // \"\"" "${left}")"
	rval="$(jq -r ".${path} // \"\"" "${right}")"
	if [[ "${lval}" != "${rval}" ]]; then
		die "identity mismatch on ${label}: left=${lval:-<empty>} right=${rval:-<empty>} -- two legs must share a corpus, run shape, and binary to be comparable"
	fi
done

left_backend="$(jq -r '.identity.backend' "${left}")"
right_backend="$(jq -r '.identity.backend' "${right}")"
iterations="$(jq -r '.identity.iterations' "${left}")"

table="$(jq -n -r --slurpfile L "${left}" --slurpfile R "${right}" '
	def route_map: map({key: .route, value: .}) | from_entries;
	def round2($x): ($x * 100 | round) / 100;
	def work_key($w): [round2($w.calls // 0), round2($w.blks // 0), round2($w.rows // 0)];

	($L[0].routes | route_map) as $lm
	| ($R[0].routes | route_map) as $rm
	| (($lm | keys) + ($rm | keys) | unique | sort) as $routes
	| $routes[]
	| . as $route
	| $lm[$route] as $lr
	| $rm[$route] as $rr
	| if ($lr == null) then
		"| \($route) | - | - | - | - | - | NON-COMPARABLE (missing on left) |"
	elif ($rr == null) then
		"| \($route) | - | - | - | - | - | NON-COMPARABLE (missing on right) |"
	elif ($lr.status != $rr.status) then
		"| \($route) | - | - | - | - | - | NON-COMPARABLE (status \($lr.status) vs \($rr.status)) |"
	elif (work_key($lr.work) != work_key($rr.work)) then
		"| \($route) | - | - | - | - | - | NON-COMPARABLE (Postgres work differs) |"
	elif ($lr.warm == null or $rr.warm == null) then
		"| \($route) | - | - | - | - | - | NON-COMPARABLE (no warm samples on one or both legs) |"
	else
		(round2($rr.warm.p95_ms / $lr.warm.p95_ms)) as $ratio
		| "| \($route) | \($lr.warm.p50_ms | round) | \($lr.warm.p95_ms | round) | \($rr.warm.p50_ms | round) | \($rr.warm.p95_ms | round) | \($ratio)x | \($lr.warm.n)/\($rr.warm.n) |"
	end
')" || die "could not render the comparison table"

rendered="$(
	printf '# Cross-backend read-latency comparison\n\n'
	printf -- '- left: `%s` (identity.runs=%s)\n' "${left_backend}" "${left_runs}"
	printf -- '- right: `%s` (identity.runs=%s)\n' "${right_backend}" "${right_runs}"
	printf -- '- iterations/route/run: %s\n\n' "${iterations}"
	printf '| route | left p50 (ms) | left p95 (ms) | right p50 (ms) | right p95 (ms) | ratio (right/left p95) | n (left/right) |\n'
	printf '| --- | --- | --- | --- | --- | --- | --- |\n'
	printf '%s' "${table}"
)"

if [[ -n "${out}" ]]; then
	tmp="$(mktemp "${out}.XXXXXX")"
	printf '%s\n' "${rendered}" >"${tmp}"
	mv "${tmp}" "${out}"
else
	printf '%s\n' "${rendered}"
fi
