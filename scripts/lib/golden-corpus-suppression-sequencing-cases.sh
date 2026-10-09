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
# reordered the phase (expiry leg first on a freshly-armed 20s window, hidden
# leg second) — but left A's window active at phase end, and #7862 proved
# that leaks downstream: B-7c query truth runs ~1min later and pins
# (A, expired), so an active A fails it findings-empty. The end-clean fix
# arms A late on a 200s window, then waits A out and asserts the expired
# readback under A, so the phase always ends at the exact B-7c pins.
# These cases simulate drain regimes against the CURRENT libs with a fake
# clock and a findings API that mirrors the real semantics: the reducer
# persists one winner per drain (newest-authored active wins, else
# newest-authored expired), reads return the persisted id with only state
# flipped by expires_at, and identical-body retry is blind to expiry.
# Covered: the 2s normal regime (matching the 12/12 ~2.1s CI drains), the
# observed single-62s stall, a triple-62s extreme (still inside the window),
# and a beyond-ceiling stall that must red honestly at the hidden assert
# with the stall diagnostic. Every pass case ends with a B-7c mirror (+60s,
# full (A, expired) pins). A future reorder, a leg that stops ending clean,
# or a lost stall diagnostic turns CI red here instead of flaking B-7.
#
# Everything timing-shaped runs inside one subshell per case; the date/sleep/
# python3/curl/pg/start_bg/die shadows never leak into the caller.

# suppression_seq_run_case runs golden_suppression_verify_producer_truth from
# the sourced libs with a fake clock, scripted per-drain lengths, and
# stubbed IO. Args: case name, drain seconds for the
# setup/malformed/expiry/ignored drains in call order (space-separated),
# stall-line expectation (want|want-not), outcome expectation (pass, or
# red-hidden for a beyond-ceiling stall that must die at the hidden assert).
suppression_seq_run_case() (
	local case_name="$1" drain_seconds="$2" stall_want="$3" expect="$4"
	local case_dir epoch_file gens_file work_file dur_file state_dir log_dir bin_dir
	case_dir="$(mktemp -d -t golden-suppression-seq.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	epoch_file="${case_dir}/epoch"
	gens_file="${case_dir}/gens"
	work_file="${case_dir}/work"
	dur_file="${case_dir}/durations"
	state_dir="${case_dir}/state"
	log_dir="${case_dir}/logs"
	bin_dir="${case_dir}/bin"
	mkdir -p "${state_dir}" "${log_dir}" "${bin_dir}"
	echo 1000000 >"${epoch_file}"
	echo 0 >"${gens_file}"
	echo 0 >"${work_file}"
	local -a dur_list
	read -ra dur_list <<<"${drain_seconds}"
	printf '%s\n' "${dur_list[@]}" >"${dur_file}"

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
	# Portable RFC3339 -> epoch for the lib's fixed '%Y-%m-%dT%H:%M:%SZ'
	# stamps: BSD date -j first, GNU date -d fallback (mirroring the
	# lib's try-BSD-then-GNU shape), so the sim runs wherever the lib
	# does, macOS included. Uses command date to bypass the fake clock.
	# Exits nonzero on unparseable input so callers keep their fallbacks.
	suppression_seq_epoch_from_rfc3339() {
		local stamp="$1" out
		out="$(command date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "${stamp}" '+%s' 2>/dev/null || true)"
		if [[ -z "${out}" ]]; then
			out="$(command date -u -d "${stamp}" '+%s' 2>/dev/null || true)"
		fi
		[[ -n "${out}" ]] || return 1
		printf '%s\n' "${out}"
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
			authored="$(suppression_seq_epoch_from_rfc3339 "$(jq -r '.authored_at' "${data_file}")")"
			expires="$(suppression_seq_epoch_from_rfc3339 "$(jq -r '.expires_at // empty' "${data_file}")" 2>/dev/null || echo 9999999999)"
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
		# GET findings: the persisted winner id with only state flipped by
		# expires_at, mirroring supplyChainImpactEffectiveDecisionStateSQL
		# over the winners read model. There is deliberately NO
		# re-derivation here: EvaluateSupplyChainSuppression runs at drain
		# time (the stub gate binary below persists its verdict); reads
		# never reorder winners.
		local persist="${state_dir}/persisted.json"
		if [[ ! -f "${persist}" ]]; then
			jq -n '{
				count: 1,
				findings: [{
					cve_id: "CVE-2026-00010",
					repository_id: "repository:r_217415d9",
					suppression: {state: "active"}
				}]
			}' >"${out}"
		else
			local pid pexp
			pid="$(jq -r '.id' "${persist}")"
			pexp="$(jq -r '.expires' "${persist}")"
			if ((now < pexp)) && [[ "${url}" != *"include_suppressed=true"* ]]; then
				jq -n '{count: 0, findings: []}' >"${out}"
			elif ((now < pexp)); then
				jq -n --arg id "${pid}" '{
					count: 1,
					findings: [{
						cve_id: "CVE-2026-00010",
						suppression: {state: "ignored", suppression_id: $id}
					}]
				}' >"${out}"
			else
				jq -n --arg id "${pid}" '{
					count: 1,
					findings: [{
						cve_id: "CVE-2026-00010",
						suppression: {state: "expired", suppression_id: $id}
					}]
				}' >"${out}"
			fi
		fi
		printf '200 0.01\n'
	}

	# Plain assignments: an env-prefix before the source builtin does not
	# persist in non-POSIX bash, and the lib reads these under set -u.
	GATE_API_PORT=1
	GATE_API_KEY="sim"
	GATE_DRAIN_TIMEOUT="1s"
	# shellcheck source=scripts/lib/golden-corpus-vulnerability-suppression.sh
	. "${suppression_lib}"
	# shellcheck source=scripts/lib/golden-corpus-suppression-window-leg.sh
	. "${suppression_window_leg_lib}"

	# The stub gate binary pops one duration per drain call (setup,
	# malformed, expiry, ignored — in call order) and persists the drain
	# verdict like the reducer: newest-authored active suppression wins,
	# else newest-authored expired, same CVE only (the scope-setup
	# suppression never touches this finding). Running dry is a loud
	# failure, not a silent zero. Generated after sourcing: it bakes in
	# the lib's CVE id.
	cat >"${bin_dir}/eshu-golden-corpus-gate" <<-EOF
#!/usr/bin/env bash
dur=\$(head -n 1 "${dur_file}")
[ -n "\$dur" ] || { echo "sim: out of drain durations" >&2; exit 3; }
echo \$((\$(cat "${epoch_file}") + \$dur)) > "${epoch_file}"
tail -n +2 "${dur_file}" > "${dur_file}.next" && mv "${dur_file}.next" "${dur_file}"
now=\$(cat "${epoch_file}")
best_active=""; best_active_a=0; best_active_e=0
best_any=""; best_any_a=0; best_any_e=0
for meta in "${state_dir}"/*.meta; do
    [ -e "\$meta" ] || break
    [ "\$(jq -r '.cve' "\$meta")" = "${golden_suppression_cve}" ] || continue
    a=\$(jq -r '.authored' "\$meta"); e=\$(jq -r '.expires' "\$meta"); mid=\$(basename "\$meta" .meta)
    if [ "\$a" -ge "\$best_any_a" ]; then best_any="\$mid"; best_any_a="\$a"; best_any_e="\$e"; fi
    if [ "\$now" -lt "\$e" ] && [ "\$a" -ge "\$best_active_a" ]; then best_active="\$mid"; best_active_a="\$a"; best_active_e="\$e"; fi
done
if [ -n "\$best_active" ]; then
    jq -n --arg id "\$best_active" --argjson expires "\$best_active_e" '{id: \$id, expires: \$expires}' > "${state_dir}/persisted.json"
elif [ -n "\$best_any" ]; then
    jq -n --arg id "\$best_any" --argjson expires "\$best_any_e" '{id: \$id, expires: \$expires}' > "${state_dir}/persisted.json"
fi
	EOF
	chmod +x "${bin_dir}/eshu-golden-corpus-gate"

	local leg_out="" leg_status=0
	leg_out="$(golden_suppression_verify_producer_truth 2>&1)" || leg_status=$?
	# Beyond-ceiling stalls must die exactly at the hidden assert (status
	# 42 is the sim die), with the ignored-drain stall diagnostic. Any
	# other death — or a pass — fails the case.
	if [[ "${expect}" == "red-hidden" ]]; then
		((leg_status == 42)) || {
			printf 'case %s: want death at hidden (42), got status %s: %s\n' \
				"${case_name}" "${leg_status}" "${leg_out}" >&2
			return 1
		}
		printf '%s\n' "${leg_out}" | grep -q 'suppression ignored_hidden query assertion failed' || {
			printf 'case %s: want the hidden-assert die site, got: %s\n' "${case_name}" "${leg_out}" >&2
			return 1
		}
		printf '%s\n' "${leg_out}" | grep -q 'suppression_stall drain_state=ignored ' || {
			printf 'case %s: want the ignored-drain suppression_stall line\n' "${case_name}" >&2
			return 1
		}
		return 0
	fi
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
	# Stall diagnostic fires exactly on the slow drains: stall_want names
	# the one stalled drain state, "any" when several stall, or "want-not".
	if [[ "${stall_want}" == "want-not" ]]; then
		printf '%s\n' "${leg_out}" | grep -q 'suppression_stall ' && {
			printf 'case %s: want no suppression_stall line\n' "${case_name}" >&2
			return 1
		}
	elif [[ "${stall_want}" == "any" ]]; then
		printf '%s\n' "${leg_out}" | grep -q 'suppression_stall ' || {
			printf 'case %s: want suppression_stall lines\n' "${case_name}" >&2
			return 1
		}
	else
		printf '%s\n' "${leg_out}" | grep -q "suppression_stall drain_state=${stall_want} " || {
			printf 'case %s: want the %s suppression_stall line\n' "${case_name}" "${stall_want}" >&2
			return 1
		}
	fi
	# B is a live 20s window, A a 200s window; the final expired readback
	# pins (A, expired) — the persisted winner with state flipped.
	local b_window a_window expired_id expired_state b_expires b_authored a_expires a_authored
	b_expires="$(suppression_seq_epoch_from_rfc3339 "$(jq -r '.expires_at' "${log_dir}/suppression-expiry-request.json")")"
	b_authored="$(suppression_seq_epoch_from_rfc3339 "$(jq -r '.authored_at' "${log_dir}/suppression-expiry-request.json")")"
	a_expires="$(suppression_seq_epoch_from_rfc3339 "$(jq -r '.expires_at' "${log_dir}/suppression-active-request.json")")"
	a_authored="$(suppression_seq_epoch_from_rfc3339 "$(jq -r '.authored_at' "${log_dir}/suppression-active-request.json")")"
	b_window=$((b_expires - b_authored))
	a_window=$((a_expires - a_authored))
	expired_id="$(jq -r '.findings[0].suppression.suppression_id' "${log_dir}/suppression-expired_visible-query.json")"
	expired_state="$(jq -r '.findings[0].suppression.state' "${log_dir}/suppression-expired_visible-query.json")"
	((b_window == 20)) || {
		printf 'case %s: B window %s, want 20\n' "${case_name}" "${b_window}" >&2
		return 1
	}
	((a_window == 200)) || {
		printf 'case %s: A window %s, want 200\n' "${case_name}" "${a_window}" >&2
		return 1
	}
	[[ "${expired_id}" == "golden-CVE-2026-00010" && "${expired_state}" == "expired" ]] || {
		printf 'case %s: expired readback (%s, %s), want (A, expired)\n' \
			"${case_name}" "${expired_id}" "${expired_state}" >&2
		return 1
	}
	# End-clean: the B-7c mirror. ~1min after the phase (the B-7c query-truth
	# gap that #7862 failed), the finding must read the full committed pins:
	# visible with (A, expired). Count alone would miss an id/state drift.
	echo $(($(cat "${epoch_file}") + 60)) >"${epoch_file}"
	local b7c_file="${case_dir}/b7c-findings.json"
	curl -sS \
		-o "${b7c_file}" \
		-w '%{http_code} %{time_total}' \
		-H "Authorization: Bearer sim" \
		"http://localhost:1/api/v0/supply-chain/impact/findings?limit=10&cve_id=CVE-2026-00010&profile=comprehensive" >/dev/null
	local b7c_count b7c_id b7c_state
	b7c_count="$(jq -r '.count' "${b7c_file}")"
	b7c_id="$(jq -r '.findings[0].suppression.suppression_id' "${b7c_file}")"
	b7c_state="$(jq -r '.findings[0].suppression.state' "${b7c_file}")"
	((b7c_count >= 1)) || {
		printf 'case %s: B-7c mirror count %s, want >= 1 (active leak)\n' "${case_name}" "${b7c_count}" >&2
		return 1
	}
	[[ "${b7c_id}" == "golden-CVE-2026-00010" && "${b7c_state}" == "expired" ]] || {
		printf 'case %s: B-7c mirror (%s, %s), want (A, expired)\n' \
			"${case_name}" "${b7c_id}" "${b7c_state}" >&2
		return 1
	}
	return 0
)

# Case 1: the normal regime. Every drain takes 2s, matching the 12/12
# ~2.1s drains in the passing attempt of run 37773153037: no stall line,
# and the B-7c mirror reads the full (A, expired) pins.
suppression_seq_run_case "fast-drain-2s" "2 2 2 2" "want-not" "pass" ||
	fail "#7740 sequencing: 2s drains must pass with no stall line"

# Case 2: the observed #7740 stall shape. Only the ignored drain stalls
# (62s); the hidden asserts land at ~84s, inside the window, with exactly
# the ignored-drain stall line.
suppression_seq_run_case "single-stall-62s" "2 2 2 62" "ignored" "pass" ||
	fail "#7740 sequencing: single 62s stall must pass with its stall line"

# Case 3: the extreme. Every drain stalls 62s at once; the hidden asserts
# land at ~186s, still inside the 200s window.
suppression_seq_run_case "triple-stall-62s" "62 62 62 62" "any" "pass" ||
	fail "#7740 sequencing: triple 62s stall must still pass"

# Case 4: beyond the ceiling. A 200s ignored drain consumes the window, so
# the proof must die exactly at the hidden assert with the ignored-drain
# stall diagnostic — an honest red, not a silent wrong answer.
suppression_seq_run_case "beyond-ceiling-200s" "2 2 2 200" "ignored" "red-hidden" ||
	fail "#7740 sequencing: 200s stall must red honestly at hidden"

suppression_sequencing_cases_completed=1
