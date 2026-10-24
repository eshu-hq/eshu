#!/usr/bin/env bash
# Authority and truth checks for scripts/collector-cutover-rollback.sh: the
# dual-authority check, the baseline capture, the exact comparison, and the
# opt-in drift report. Sourced by the step library (see its header for the
# helpers it supplies). Everything here is read-only.

# The option variables and exit codes come from the main script.
# shellcheck disable=SC2154

# TRUTH_HEADER versions the baseline file. A baseline written by another format
# is refused rather than compared, because the digests would not be comparable.
readonly TRUTH_HEADER="# eshu-rollback-truth format=1"

# read_truth fills XOUT with the per-kind truth rows or aborts.
read_truth() { # step
	psql_run truth -v "kind=${collector_kind}"
	if [[ "$XRC" -ne 0 ]]; then
		step_failed "$1" "could not read fact truth for ${collector_kind}: ${XOUT}"
	fi
}

# psql_keys_to writes the per-fact key lines, sorted, to a file. They stream to
# disk because a large scope has one line per fact.
psql_keys_to() { # step file
	local raw err_rc=0
	mk_tmp raw
	"$ROLLBACK_PSQL_BIN" -X -A -t -v ON_ERROR_STOP=1 -v "kind=${collector_kind}" -f "$(sql_file keys)" >"$raw" 2>"${raw}.err" || err_rc=$?
	if [[ "$err_rc" -ne 0 ]]; then
		local detail
		detail="$(head -c 400 "${raw}.err")"
		rm -f "$raw" "${raw}.err"
		step_failed "$1" "could not read the fact keys for ${collector_kind} (exit ${err_rc}): ${detail}"
	fi
	awk 'NF' "$raw" | LC_ALL=C sort >"$2"
	rm -f "$raw" "${raw}.err"
}

# truth_view prints the comparable rows of a truth file: no header, no blanks,
# and with --keys-only only kind, count, and key digest.
truth_view() { # file
	if [[ "$keys_only" == "true" ]]; then
		awk -F'|' 'NF && !/^#/ { print $1 "|" $2 "|" $3 }' "$1"
	else
		awk 'NF && !/^#/' "$1"
	fi
}

check_authority() { # step
	psql_run dual -v "kind=${collector_kind}" -v "in_tree=${in_tree_instances}"
	if [[ "$XRC" -ne 0 || ! "$XOUT" =~ ^[0-9]+\|[0-9]+\|[0-9]+$ ]]; then
		step_failed "$1" "could not read the claim table for the authority check (exit ${XRC})"
	fi
	local ext tree overlaps
	IFS='|' read -r ext tree overlaps <<<"$XOUT"
	printf '  external_claimed=%s in_tree_claimed=%s overlapping_claim_pairs=%s\n' "$ext" "$tree" "$overlaps"
	if ((ext > 0 && tree > 0)) || ((overlaps > 0)); then
		printf 'DUAL AUTHORITY[step %s]: both producers hold or held authority for one scope.\n' "$1" >&2
		exit "$EX_DUAL_AUTHORITY"
	fi
	if ((ext > 0)); then
		printf 'REFUSED[step %s]: an external instance still holds %s claimed work item(s).\n' "$1" "$ext" >&2
		exit "$EX_REFUSED"
	fi
	evidence "$1" "no dual authority: external_claimed=0 overlapping_claim_pairs=0"
}

# report_drift prints, per fact kind, how many stable keys were added, removed,
# or changed since the baseline, and up to --drift-keys keys of each. It needs
# the FILE.keys sidecar written by --capture-keys. It only describes the
# difference; the exit code stays 6 and nothing is accepted.
report_drift() { # step
	local keys_file="${baseline_file}.keys" current
	if [[ ! -s "$keys_file" ]]; then
		printf 'DRIFT REPORT: %s does not exist, so the keys that moved cannot be named.\n' "$keys_file"
		printf 'Recapture the baseline with --capture-keys before the next cutover. The per-kind\n'
		printf 'counts above still show which kinds differ.\n'
		return 0
	fi
	mk_tmp current
	psql_keys_to "$1" "$current"
	printf 'DRIFT REPORT (baseline keys: %s, up to %s key(s) per kind and class):\n' "$keys_file" "$drift_keys"
	# pass=N operands switch the awk phase when awk reaches them, so an empty
	# current-keys file cannot shift the phase numbering.
	awk -F'\t' -v max="$drift_keys" '
		function key(line) { return substr(line, length($1) + length($2) + 3) }
		function sample(kind, class, text) {
			if (shown[class SUBSEP kind]++ < max) print kind "\t1\t" class ": " text
		}
		pass == 1 { base[$1 SUBSEP key($0)] = $2; next }
		pass == 2 {
			k = $1 SUBSEP key($0)
			kinds[$1] = 1
			if (!(k in base)) { added[$1]++; sample($1, "added", key($0)) }
			else {
				seen[k] = 1
				if (base[k] != $2) { changed[$1]++; sample($1, "changed", key($0)) }
			}
			next
		}
		pass == 3 {
			k = $1 SUBSEP key($0)
			kinds[$1] = 1
			if (!(k in seen)) { removed[$1]++; sample($1, "removed", key($0)) }
		}
		END {
			for (kind in kinds) {
				print kind "\t0\tadded=" (added[kind] + 0) " removed=" (removed[kind] + 0) " changed=" (changed[kind] + 0)
			}
		}' pass=1 "$keys_file" pass=2 "$current" pass=3 "$keys_file" | LC_ALL=C sort |
		awk -F'\t' '{ print "  " $1 "  " substr($0, length($1) + length($2) + 3) }'
	rm -f "$current"
}

compare_truth() { # step
	[[ -s "$baseline_file" ]] || step_failed "$1" "baseline file '${baseline_file}' is missing or empty"
	check_baseline_format "$1"
	read_truth "$1"
	local current baseline
	mk_tmp current
	mk_tmp baseline
	printf '%s\n' "$XOUT" >"$current"
	truth_view "$baseline_file" >"$baseline"
	truth_view "$current" >"${current}.view"
	local diff_out diff_rc=0
	diff_out="$(diff -u --label baseline --label current "$baseline" "${current}.view")" || diff_rc=$?
	rm -f "$current" "${current}.view" "$baseline"
	if [[ "$diff_rc" -ne 0 ]]; then
		printf '%s\n' "$diff_out" | sed 's/^/    /'
		[[ "$report_drift" != "true" ]] || report_drift "$1"
		printf 'TRUTH MISMATCH[step %s]: fact truth for %s differs from the baseline.\n' "$1" "$collector_kind" >&2
		exit "$EX_TRUTH_MISMATCH"
	fi
	evidence "$1" "fact truth equals baseline ${baseline_file} (keys_only=${keys_only})"
}

# baseline_format_problem prints why the baseline file cannot be compared and
# returns 0, or returns 1 when its format header is the one this script writes.
baseline_format_problem() {
	local first
	first="$(head -n 1 "$baseline_file")"
	[[ "$first" != "$TRUTH_HEADER" ]] || return 1
	printf "baseline file '%s' is not '%s' (first line: '%s'); recapture it with verify --capture" \
		"$baseline_file" "$TRUTH_HEADER" "$first"
}

# check_baseline_format fails the step for a baseline this script cannot compare.
check_baseline_format() { # step
	local problem
	if problem="$(baseline_format_problem)"; then
		step_failed "$1" "$problem"
	fi
}

# do_verify runs the read-only authority and truth checks. `run` calls it as
# step 6; the verify subcommand calls it on its own.
do_verify() { # step
	local step="$1"
	check_authority "$step"
	if [[ -n "$namespace" && -n "$worker_deployment" ]]; then
		if worker_pods_gone; then
			evidence "$step" "no extension worker pod remains and deployment/${worker_deployment} is at 0 replicas or absent"
		else
			refuse "$step" "the extension worker is not stopped: ${POD_NOTE}"
		fi
	fi
	if [[ -n "$component_home" && -n "$component_id" ]]; then
		if registry_clear; then
			evidence "$step" "component ${component_id} has no enabled activation"
		else
			refuse "$step" "$REGISTRY_NOTE"
		fi
	fi
	if [[ -n "$component_home" && -n "$component_id" && ${#grant_kinds[@]} -gt 0 ]]; then
		[[ -n "$producer_id" ]] || producer_id="$component_id"
		[[ -n "$grant_scope" ]] || grant_scope="$collector_kind"
		if grants_revoked; then
			evidence "$step" "external producer grants are revoked"
		else
			refuse "$step" "$GRANT_NOTE"
		fi
	fi
	compare_truth "$step"
}

capture_baseline() {
	read_truth 6
	[[ -n "$XOUT" ]] || step_failed 6 "no active facts exist for collector kind ${collector_kind}; an empty baseline would prove nothing"
	# A key sidecar belongs to the capture that wrote it. Drop any older one first, so
	# a recapture without --capture-keys, or one whose keys query fails, can never
	# leave keys from a previous baseline for --report-drift to trust. The baseline
	# goes too: a shell redirect keeps the mode of a file that already exists.
	rm -f "${capture_file}" "${capture_file}.keys"
	{
		printf '%s\n' "$TRUTH_HEADER"
		printf '%s\n' "$XOUT"
	} >"$capture_file"
	printf 'Captured %s fact kind(s) for %s into %s\n' "$(printf '%s\n' "$XOUT" | awk 'END { print NR }')" "$collector_kind" "$capture_file"
	if [[ "$capture_keys" == "true" ]]; then
		psql_keys_to 6 "${capture_file}.keys"
		printf 'Captured %s fact key(s) into %s.keys\n' "$(awk 'END { print NR }' "${capture_file}.keys")" "$capture_file"
	fi
	evidence 6 "baseline written to ${capture_file}; keep it with the cutover record"
}
