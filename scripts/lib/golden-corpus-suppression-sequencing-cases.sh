#!/usr/bin/env bash
# #7740: committed sequencing cases for the suppression producer proof
# (scripts/lib/golden-corpus-vulnerability-suppression.sh), sourced by
# test-verify-golden-corpus-gate.sh. Runs in the caller's shell and uses the
# caller's fail(), repo_root, and suppression_lib. Extracted to its own lib
# chunk to keep both files near the repo's 500-line cap.
#
# Why this file exists
# --------------------
# Run 37773153037 proved the old single-suppression order is a harness race:
# one 62s ignored drain consumed the 20s window armed at phase start, and the
# hidden assertion failed on a genuinely-expired suppression. The D4 fix
# reorders the phase (expiry leg first on a freshly-armed 20s window, hidden
# leg second on a 600s window) so drain duration cannot trip either leg. These
# cases simulate both drain regimes against the CURRENT lib with a fake clock
# and a findings API that mirrors the real read-time semantics (single
# persisted winner, active-beats-expired, expiry re-checked at read time):
# the 62s case fails on the old order (dies at ignored_hidden) and passes on
# the new one, and the 2s case passes on both (matching the 12/12 ~2.1s CI
# drains). A future reorder back to hidden-before-expiry turns CI red here
# instead of flaking B-7 under load.
#
# Everything timing-shaped runs inside one subshell per case; the date/sleep/
# python3/curl/pg/start_bg/die shadows never leak into the caller.

# suppression_seq_run_case runs golden_suppression_verify_producer_truth from
# the sourced lib with a fake clock, a scripted drain length, and stubbed
# IO. Args: case name, drain seconds, stall-line expectation (want|want-not).
suppression_seq_run_case() (
	local case_name="$1" drain_seconds="$2" stall_want="$3"
	local case_dir epoch_file gens_file work_file state_dir log_dir bin_dir
	case_dir="$(mktemp -d -t golden-suppression-seq.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	epoch_file="${case_dir}/epoch"
	gens_file="${case_dir}/gens"
	work_file="${case_dir}/work"
	state_dir="${case_dir}/state"
	log_dir="${case_dir}/logs"
	bin_dir="${case_dir}/bin"
	mkdir -p "${state_dir}" "${log_dir}" "${bin_dir}"
	echo 1000000 >"${epoch_file}"
	echo 0 >"${gens_file}"
	echo 0 >"${work_file}"

	date() {
		if [[ "$*" == "-u +%s" ]]; then
			cat "${epoch_file}"
		else
			command date "$@"
		fi
	}
	sleep() {
		echo $(($(cat "${epoch_file}") + $1)) >"${epoch_file}"
	}
	# The lib uses python3 only for monotonic drain-wall timing.
	python3() {
		printf '%s.0\n' "$(cat "${epoch_file}")"
	}
	start_bg() {
		printf -v "$2" '%s' "99999999"
	}
	die() {
		printf 'sim-die: %s\n' "$*" >&2
		exit 42
	}
	pg() {
		local query="$1"
		case "${query}" in
		*string_agg*)
			printf 'succeededx2/attempt0/none/none\n'
			return
			;;
		*scope_generations*)
			printf '%s %s\n' "$(cat "${gens_file}")" "$(cat "${work_file}")"
			return
			;;
		*active_generation_id*)
			printf 'gen-sim-1\n'
			return
			;;
		*reducer_input_invalid_facts*)
			printf '1\n'
			return
			;;
		*inserted_fact*)
			printf '1 1\n'
			return
			;;
		*count\(\*\)*FROM*removed*)
			printf '1\n'
			return
			;;
		*FILTER*)
			printf '0 0\n'
			return
			;;
		*)
			printf '0 0\n'
			return
			;;
		esac
	}
	curl() {
		local args=("$@") i=0 out="" url="" data_file=""
		while ((i < ${#args[@]})); do
			case "${args[$i]}" in
			-o)
				out="${args[$((i + 1))]}"
				((i += 2))
				;;
			-w | -X | -H | --data-binary)
				if [[ "${args[$i]}" == "--data-binary" ]]; then
					data_file="${args[$((i + 1))]#@}"
				fi
				((i += 2))
				;;
			-sS)
				((i += 1))
				;;
			*)
				url="${args[$i]}"
				((i += 1))
				;;
			esac
		done
		local now
		now="$(cat "${epoch_file}")"
		if [[ -n "${data_file}" ]]; then
			local id authored expires cve
			id="$(jq -r '.suppression_id' "${data_file}")"
			authored="$(command date -u -d "$(jq -r '.authored_at' "${data_file}")" '+%s')"
			expires="$(command date -u -d "$(jq -r '.expires_at // empty' "${data_file}")" '+%s' 2>/dev/null || echo 9999999999)"
			cve="$(jq -r '.scope.cve_id // empty' "${data_file}")"
			if [[ -f "${state_dir}/${id}.payload" ]] && cmp -s "${data_file}" "${state_dir}/${id}.payload"; then
				jq -n --arg id "${id}" \
					'{suppression_id: $id, status: "unchanged", generation_id: "gen-sim"}' >"${out}"
				printf '200 0.01\n'
				return
			fi
			cp "${data_file}" "${state_dir}/${id}.payload"
			jq -n --argjson authored "${authored}" --argjson expires "${expires}" \
				--arg cve "${cve}" \
				'{authored: $authored, expires: $expires, cve: $cve}' >"${state_dir}/${id}.meta"
			echo $(($(cat "${gens_file}") + 1)) >"${gens_file}"
			echo $(($(cat "${work_file}") + 1)) >"${work_file}"
			jq -n --arg id "${id}" \
				'{suppression_id: $id, status: "created", generation_id: "gen-sim"}' >"${out}"
			printf '201 0.01\n'
			return
		fi
		# GET findings: newest-authored active suppression wins (hidden);
		# otherwise the newest wins as expired; none means the active
		# baseline. Mirrors EvaluateSupplyChainSuppression +
		# supplyChainImpactEffectiveDecisionStateSQL, not the script.
		local meta winner="" winner_authored=0 active_id="" active_authored=0
		for meta in "${state_dir}"/*.meta; do
			[[ -e "${meta}" ]] || break
			# Adjacency: only same-CVE suppressions touch this finding. The
			# scope-setup suppression (CVE-2026-99999) never does.
			[[ "$(jq -r '.cve' "${meta}")" == "${golden_suppression_cve}" ]] || continue
			local a e mid
			a="$(jq -r '.authored' "${meta}")"
			e="$(jq -r '.expires' "${meta}")"
			mid="$(basename "${meta}" .meta)"
			if ((a >= winner_authored)); then
				winner_authored="${a}"
				winner="${mid}"
			fi
			if ((now < e)) && ((a >= active_authored)); then
				active_authored="${a}"
				active_id="${mid}"
			fi
		done
		if [[ -z "${winner}" ]]; then
			jq -n '{
				count: 1,
				findings: [{
					cve_id: "CVE-2026-00010",
					repository_id: "repository:r_217415d9",
					suppression: {state: "active"}
				}]
			}' >"${out}"
		elif [[ -n "${active_id}" ]] && [[ "${url}" != *"include_suppressed=true"* ]]; then
			jq -n '{count: 0, findings: []}' >"${out}"
		elif [[ -n "${active_id}" ]]; then
			jq -n --arg id "${active_id}" '{
				count: 1,
				findings: [{
					cve_id: "CVE-2026-00010",
					suppression: {state: "ignored", suppression_id: $id}
				}]
			}' >"${out}"
		else
			jq -n --arg id "${winner}" '{
				count: 1,
				findings: [{
					cve_id: "CVE-2026-00010",
					suppression: {state: "expired", suppression_id: $id}
				}]
			}' >"${out}"
		fi
		printf '200 0.01\n'
	}
	cat >"${bin_dir}/eshu-golden-corpus-gate" <<-EOF
#!/usr/bin/env bash
echo \$((\$(cat "${epoch_file}") + ${drain_seconds})) > "${epoch_file}"
	EOF
	chmod +x "${bin_dir}/eshu-golden-corpus-gate"

	# Plain assignments: an env-prefix before the source builtin does not
	# persist in non-POSIX bash, and the lib reads these under set -u.
	GATE_API_PORT=1
	GATE_API_KEY="sim"
	GATE_DRAIN_TIMEOUT="1s"
	# shellcheck source=scripts/lib/golden-corpus-vulnerability-suppression.sh
	. "${suppression_lib}"

	local leg_out="" leg_status=0
	leg_out="$(golden_suppression_verify_producer_truth 2>&1)" || leg_status=$?
	if ((leg_status != 0)); then
		printf 'case %s: proof died (status %s): %s\n' "${case_name}" "${leg_status}" "${leg_out}" >&2
		return 1
	fi
	# B's expiry leg must run before A's hidden leg: the expiry mutation line
	# precedes the bare ignored-mutation line in the perf log.
	local expiry_line ignored_line
	expiry_line="$(printf '%s\n' "${leg_out}" | grep -n 'mutation=ignored_expiry' | cut -d: -f1)"
	ignored_line="$(printf '%s\n' "${leg_out}" | grep -n 'mutation=ignored ' | cut -d: -f1)"
	if [[ -z "${expiry_line}" || -z "${ignored_line}" || "${expiry_line}" -ge "${ignored_line}" ]]; then
		printf 'case %s: want expiry leg before hidden leg, got lines %s/%s\n' \
			"${case_name}" "${expiry_line}" "${ignored_line}" >&2
		return 1
	fi
	# Stall diagnostic fires only on a slow drain.
	if [[ "${stall_want}" == "want" ]]; then
		printf '%s\n' "${leg_out}" | grep -q 'suppression_stall drain_state=ignored_expiry' || {
			printf 'case %s: want suppression_stall line\n' "${case_name}" >&2
			return 1
		}
	else
		printf '%s\n' "${leg_out}" | grep -q 'suppression_stall ' && {
			printf 'case %s: want no suppression_stall line\n' "${case_name}" >&2
			return 1
		}
	fi
	# B is a live 20s window, A a 600s window; the expired assertion pinned B.
	local b_window a_window expired_id b_expires b_authored a_expires a_authored
	b_expires="$(command date -u -d "$(jq -r '.expires_at' "${log_dir}/suppression-expiry-request.json")" '+%s')"
	b_authored="$(command date -u -d "$(jq -r '.authored_at' "${log_dir}/suppression-expiry-request.json")" '+%s')"
	a_expires="$(command date -u -d "$(jq -r '.expires_at' "${log_dir}/suppression-active-request.json")" '+%s')"
	a_authored="$(command date -u -d "$(jq -r '.authored_at' "${log_dir}/suppression-active-request.json")" '+%s')"
	b_window=$((b_expires - b_authored))
	a_window=$((a_expires - a_authored))
	expired_id="$(jq -r '.findings[0].suppression.suppression_id' "${log_dir}/suppression-expired_visible-query.json")"
	((b_window == 20)) || {
		printf 'case %s: B window %s, want 20\n' "${case_name}" "${b_window}" >&2
		return 1
	}
	((a_window == 600)) || {
		printf 'case %s: A window %s, want 600\n' "${case_name}" "${a_window}" >&2
		return 1
	}
	[[ "${expired_id}" == "golden-CVE-2026-00010-expiry" ]] || {
		printf 'case %s: expired id %s, want the B id\n' "${case_name}" "${expired_id}" >&2
		return 1
	}
	return 0
)

# Case 1: the #7740 stall regime. A 62s ignored drain must still pass: B is
# already expired when its wait runs (no-op), and A holds 600s of headroom.
suppression_seq_run_case "slow-drain-62s" 62 "want" ||
	fail "#7740 sequencing: 62s drain must pass with a suppression_stall line"

# Case 2: the normal regime. A 2s drain passes with no stall line, matching
# the 12/12 ~2.1s drains in the passing attempt of run 37773153037.
suppression_seq_run_case "fast-drain-2s" 2 "want-not" ||
	fail "#7740 sequencing: 2s drain must pass with no suppression_stall line"

suppression_sequencing_cases_completed=1
