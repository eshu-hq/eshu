#!/usr/bin/env bash
# shellcheck disable=SC2154,SC2034,SC2329  # Sourced; the parent owns these names, and pg() is called by the lib.
# Collector settle Case G for test-verify-golden-corpus-gate.sh (#6965).
#
# Sourced, never executed, from golden-corpus-collector-settle-cases.sh, whose
# collector_settle_run_case(), collector_settle_case_log_dir, die() and fail()
# it uses. Kept separate so that cases file stays well under the 500-line cap.
#
# The population the settle poll reads is not the launched set. Bootstrap
# migration 115 seeds an eshu:global scope and generation on every run, so a
# count threshold derived from the launched set can be met with one launched
# collector still committing. #6965 observed exactly this: the gcp cassette
# (one source, one generation) was in flight, eshu:global filled the slack, the
# poll broke on 19/39, and the kill rolled back 234 facts.
#
# This case plants that population: 39 launched pairs over 19 sources, all but
# the single-scope source-19 pair present, plus eshu:global. It first proves the
# plant is a real counterexample to the count predicate (today's >= 19 sources
# and >= 39 generations both hold), then that the subset assertion refuses it
# and names the missing pair, then that it settles once that pair commits.
#
# The mock pg() reads the (scope_id, generation_id) rows out of the VALUES list
# the lib actually built and joins them to the planted table, so a lib that
# dropped, duplicated or misquoted a pair fails here rather than in CI.

collector_settle_population="${collector_settle_case_log_dir}/population.tsv"
collector_settle_launched=()
: >"${collector_settle_population}"
for collector_settle_source in $(seq -w 1 19); do
	collector_settle_gens=2
	case "${collector_settle_source}" in
	01 | 02) collector_settle_gens=3 ;;
	19) collector_settle_gens=1 ;;
	esac
	for collector_settle_gen in $(seq 1 "${collector_settle_gens}"); do
		collector_settle_scope="source-${collector_settle_source}:scope"
		collector_settle_generation="source-${collector_settle_source}-gen${collector_settle_gen}"
		collector_settle_launched+=("${collector_settle_scope}"$'\t'"${collector_settle_generation}")
		if [[ "${collector_settle_source}" != "19" ]]; then
			printf 'source-%s\t%s\t%s\tactive\n' "${collector_settle_source}" \
				"${collector_settle_scope}" "${collector_settle_generation}" >>"${collector_settle_population}"
		fi
	done
done
printf 'eshu\teshu:global\teshu:global:genesis\tactive\n' >>"${collector_settle_population}"

[[ "${#collector_settle_launched[@]}" -eq 39 ]] ||
	fail "test harness: case G must launch 39 pairs, built ${#collector_settle_launched[@]}"

# Today's thresholds, derived the way golden-corpus-cassette-replay.sh derived
# them before #6965: launched sources and launched generations.
collector_settle_old_min_sources="$(printf '%s\n' "${collector_settle_launched[@]}" | cut -f1 | sort -u | wc -l | tr -d ' ')"
collector_settle_old_total="${#collector_settle_launched[@]}"
# What today's probe would read from the planted population.
collector_settle_old_sources="$(awk -F'\t' '$1 != "git" { print $1 }' "${collector_settle_population}" | sort -u | wc -l | tr -d ' ')"
collector_settle_old_generations="$(awk -F'\t' '$1 != "git"' "${collector_settle_population}" | wc -l | tr -d ' ')"
if ! ((collector_settle_old_sources >= collector_settle_old_min_sources &&
	collector_settle_old_generations >= collector_settle_old_total)); then
	fail "case G plant no longer defeats the count predicate (${collector_settle_old_sources}/${collector_settle_old_min_sources} sources, ${collector_settle_old_generations}/${collector_settle_old_total} generations), so it proves nothing about #6965"
fi

# collector_settle_population_pg answers the lib's subset probe from the
# planted table. It emits the lib's one-row shape.
collector_settle_population_pg() {
	local sql="$1" line scope_id generation_id present=0 total=0 missing=""
	while IFS= read -r line; do
		[[ "${line}" =~ ^[[:space:]]*\(\'([^\']*)\',\ \'([^\']*)\'\),?$ ]] || continue
		scope_id="${BASH_REMATCH[1]}"
		generation_id="${BASH_REMATCH[2]}"
		total=$((total + 1))
		if awk -F'\t' -v s="${scope_id}" -v g="${generation_id}" \
			'$2 == s && $3 == g && ($4 == "pending" || $4 == "active" || $4 == "completed" || $4 == "superseded") { found = 1 } END { exit !found }' \
			"${collector_settle_population}"; then
			present=$((present + 1))
		else
			missing+="${missing:+,}${scope_id}/${generation_id}"
		fi
	done <<<"${sql}"
	printf '%s %s %s' "${present}" "${total}" "${missing}"
}

collector_settle_case_g_err="${collector_settle_case_log_dir}/case-g.err"
if (
	pg() { collector_settle_population_pg "$1"; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_launched[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	collector_settle_run_case g
) >/dev/null 2>"${collector_settle_case_g_err}"; then
	fail "wait_for_collector_settle settled on 38 of 39 launched pairs plus eshu:global (#6965)"
fi
rg --fixed-strings --quiet -- '38 of 39 launched scope generations landed; missing: source-19:scope/source-19-gen1' \
	"${collector_settle_case_g_err}" ||
	fail "case G must name the one in-flight pair: $(cat "${collector_settle_case_g_err}")"

printf 'source-19\tsource-19:scope\tsource-19-gen1\tpending\n' >>"${collector_settle_population}"
collector_settle_case_g_out="${collector_settle_case_log_dir}/case-g.out"
if ! (
	pg() { collector_settle_population_pg "$1"; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_launched[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=10
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	collector_settle_run_case g-green
) >"${collector_settle_case_g_out}" 2>&1; then
	fail "wait_for_collector_settle must settle once every launched pair is present, extras ignored: $(cat "${collector_settle_case_g_out}")"
fi
rg --fixed-strings --quiet -- 'all 39 launched scope generations landed' "${collector_settle_case_g_out}" ||
	fail "case G green must report all 39 launched pairs"

unset -f collector_settle_population_pg
