#!/usr/bin/env bash
#
# golden-corpus-failure-diagnostics.sh — stage the #11382 failure-diagnostics
# bundle for the golden corpus gate orchestrator
# (scripts/verify-golden-corpus-gate.sh).
#
# stage_failure_diagnostics() runs on the failure path only, called from
# cleanup() in scripts/lib/golden-corpus-cleanup.sh BEFORE the compose
# teardown and the `rm -rf "${work_dir}"`, so the Postgres queries below
# still have a database to read and the work_dir files still exist. It stages:
#   (i)   the leg's ESHU_DIFFERENTIAL_CAPTURE_DIR *.jsonl recordings as-is
#         (skipped when unset: the corpus-gate job runs no capture leg);
#   (ii)  from work_dir: logs/e2e-20repo-repository-runtime-snapshot.json,
#         logs/persisted-aggregate-counts.json (the aggregate-counts oracle),
#         logs/phase-timings.json, plus last-200-line tails of every
#         host-binary *.log passed through sed redaction;
#   (iii) reducer-domain.json from three bounded read-only SELECTs against
#         the gate Postgres: terraform-state scope/generation commit counts,
#         drift findings by finding_kind, and the IaC management finding
#         count. Counts and small group-bys only: no full dumps, no DSNs, no
#         key material. A failed query degrades its own section, never the
#         bundle: the section records query_failed with the exit status, and
#         pg's own error text is kept off the bundle (psql connection errors
#         can echo connection context).
#
# The bundle lands in ${RUNNER_TEMP:-/tmp}/golden-corpus-failure/<job> (one
# bundle per failing leg; the workflow uploads that exact path on failure
# only, retention 7 days) and the function prints the directory. The staged
# bundle is capped at 100MB (GOLDEN_CORPUS_DIAG_MAX_KB, default 102400):
# tails truncate to 50 lines first, and a bundle still over the cap fails
# loud (::error, non-zero, nothing left behind to upload). Missing or empty
# input also fails closed. Staging never calls die(): it returns non-zero and
# lets cleanup() keep the original exit status through teardown.
#
# Requires (set by the orchestrator before the call): work_dir, log_dir, the
# pg() function (golden-corpus-host-helpers.sh; when absent, section (iii)
# records unavailable and staging continues with files). Bodies resolve these
# lazily at call time, so sourcing this file early is safe.

# redact_gate_log redacts credential-shaped content from a host-binary log
# tail: Authorization headers (Authorization:, authorization:,
# AUTHORIZATION:), password=/PASSWORD= assignments, whole PEM private-key
# blocks, and URI userinfo (the no-DSN rule: any userinfo exists to carry
# credentials). A 200-line tail can cut below a PEM BEGIN line, leaving a
# key body whose END line the sed range below would never open on, so the
# POSIX awk pre-pass fails closed first: an END line with no BEGIN above it
# in the tail redacts back to the previous END (or the tail start). Long
# base64 runs (48 or more characters, the PEM body shape; 40-char SHA1s pass
# through) redact inline as a second net. Whole-line replacement for key
# blocks, value replacement elsewhere; portable BRE so it runs on GNU and
# BSD sed.
redact_gate_log() {
	awk '
		{ buf[NR] = $0 }
		/BEGIN .* KEY/ { depth++ }
		/END .* KEY/ {
			if (depth > 0) { depth-- }
			else { for (i = red + 1; i <= NR; i++) buf[i] = "[redacted private key material]" }
			red = NR
		}
		END { for (i = 1; i <= NR; i++) print buf[i] }
	' | sed -e 's/Authorization:.*/Authorization: [redacted]/' \
		-e 's/authorization:.*/authorization: [redacted]/' \
		-e 's/AUTHORIZATION:.*/AUTHORIZATION: [redacted]/' \
		-e 's/password=[^[:space:]&;]*/password=[redacted]/g' \
		-e 's/PASSWORD=[^[:space:]&;]*/PASSWORD=[redacted]/g' \
		-e 's|://[^/@ ]*@|://[redacted]@|g' \
		-e '/BEGIN .* KEY/,/END .* KEY/s/.*/[redacted private key material]/' \
		-e '/PRIVATE KEY/s/.*/[redacted private key material]/' \
		-e '/BEGIN .* KEY/s/.*/[redacted private key material]/' \
		-e 's/[A-Za-z0-9+\/=]\{48,\}/[redacted]/g'
}

# diag_pg runs one bounded diagnostic SELECT through pg(), printing the
# single-line answer on success. Prints nothing and returns non-zero on
# failure; the caller's section marker carries the failure instead of pg's
# stderr.
diag_pg() {
	local sql="$1" answer status
	answer="$(pg "${sql}" 2>/dev/null)" && status=0 || status=$?
	if [[ "${status}" -ne 0 ]]; then
		return "${status}"
	fi
	case "${answer}" in
		*$'\n'*) return 1 ;;
	esac
	printf '%s' "${answer}"
	return 0
}

stage_failure_diagnostics() {
	local job="${1:-${GATE_COMPOSE_PROJECT:-local}}"
	job="${job//\//_}"
	local dest_root="${RUNNER_TEMP:-/tmp}/golden-corpus-failure"
	local dest="${dest_root}/${job}"
	local max_kb="${GOLDEN_CORPUS_DIAG_MAX_KB:-102400}"
	case "${max_kb}" in '' | *[!0-9]*) max_kb=102400 ;; esac
	local staged=0 capture_n=0 snap_n=0 tails_n=0 truncated_tails=false
	local capture_dir="${ESHU_DIFFERENTIAL_CAPTURE_DIR:-}"
	local logf base q1 q2 q3 q1_ok q2_ok q3_ok bundle_kb tailf tmpf
	if [[ -z "${job}" ]]; then
		printf '::error title=Failure diagnostics::refusing to stage: empty job name\n' >&2
		return 1
	fi
	if [[ -z "${work_dir:-}" || -z "${log_dir:-}" || ! -d "${log_dir:-}" ]]; then
		printf '::error title=Failure diagnostics::refusing to stage: work_dir/log_dir missing (work_dir=%s log_dir=%s)\n' \
			"${work_dir:-<unset>}" "${log_dir:-<unset>}" >&2
		return 1
	fi
	rm -rf "${dest}"
	if ! mkdir -p "${dest}/capture" "${dest}/log-tails"; then
		printf '::error title=Failure diagnostics::cannot create bundle dir %s\n' "${dest}" >&2
		return 1
	fi
	# (i) differential capture recordings, as-is, regular files only.
	if [[ -n "${capture_dir}" && -d "${capture_dir}" ]]; then
		for logf in "${capture_dir}"/*.jsonl; do
			[[ -f "${logf}" && ! -L "${logf}" ]] || continue
			if cp -p "${logf}" "${dest}/capture/"; then
				staged=$((staged + 1))
				capture_n=$((capture_n + 1))
			else
				printf 'failure-diagnostics: skipping unreadable capture file %s\n' "${logf}" >&2
			fi
		done
	fi
	# (ii) fixed work_dir JSONs, as-is when the run reached them.
	for base in e2e-20repo-repository-runtime-snapshot.json persisted-aggregate-counts.json phase-timings.json; do
		if [[ -f "${log_dir}/${base}" && ! -L "${log_dir}/${base}" ]]; then
			if cp -p "${log_dir}/${base}" "${dest}/"; then
				staged=$((staged + 1))
				snap_n=$((snap_n + 1))
			else
				printf 'failure-diagnostics: skipping unreadable %s\n' "${base}" >&2
			fi
		fi
	done
	# (ii) redacted 200-line tails of every host-binary log (regular files
	# only, like (i): a symlinked *.log is skipped, never followed).
	for logf in "${log_dir}"/*.log; do
		[[ -f "${logf}" && ! -L "${logf}" ]] || continue
		base="$(basename "${logf}")"
		if tail -n 200 "${logf}" 2>/dev/null | redact_gate_log >"${dest}/log-tails/${base}.tail"; then
			staged=$((staged + 1))
			tails_n=$((tails_n + 1))
		else
			printf 'failure-diagnostics: skipping unreadable log %s\n' "${base}" >&2
			rm -f "${dest}/log-tails/${base}.tail"
		fi
	done
	# (iii) reducer-domain.json from three bounded read-only queries.
	q1='[]' q2='[]' q3='null' q1_ok=false q2_ok=false q3_ok=false
	if command -v pg >/dev/null 2>&1; then
		if q1="$(diag_pg "-- diag-q1-terraform-state-scopes
SELECT COALESCE(json_agg(row_to_json(t)), '[]') FROM (
  SELECT s.scope_id, s.status, s.active_generation_id,
         (SELECT count(*) FROM scope_generations g WHERE g.scope_id = s.scope_id) AS generation_count,
         (SELECT count(*) FROM scope_generations g WHERE g.scope_id = s.scope_id AND g.status = 'active') AS active_generation_count
  FROM ingestion_scopes s
  WHERE s.scope_id LIKE 'state_snapshot:%'
  ORDER BY s.scope_id
  LIMIT 50
) t;")"; then
			q1_ok=true
			staged=$((staged + 1))
		else
			printf 'failure-diagnostics: terraform-state scope query failed; marking its section\n' >&2
			q1='{"query_failed":true}'
		fi
		if q2="$(diag_pg "-- diag-q2-drift-findings
SELECT COALESCE(json_agg(row_to_json(t)), '[]') FROM (
  SELECT COALESCE(payload->>'finding_kind', '<none>') AS finding_kind, count(*) AS finding_count
  FROM fact_records
  WHERE fact_kind = 'reducer_terraform_config_state_drift_finding'
  GROUP BY 1 ORDER BY 1
  LIMIT 50
) t;")"; then
			q2_ok=true
			staged=$((staged + 1))
		else
			printf 'failure-diagnostics: drift-finding query failed; marking its section\n' >&2
			q2='{"query_failed":true}'
		fi
		if q3="$(diag_pg "-- diag-q3-iac-management-count
SELECT count(*) FROM fact_records
WHERE fact_kind IN ('reducer_aws_cloud_runtime_drift_finding', 'reducer_multi_cloud_runtime_drift_finding');")"; then
			case "${q3}" in
				'' | *[!0-9]*)
					printf 'failure-diagnostics: IaC management count answer is not a number; marking its section\n' >&2
					q3='{"query_failed":true}'
					;;
				*)
					q3_ok=true
					staged=$((staged + 1))
					;;
			esac
		else
			printf 'failure-diagnostics: IaC management count query failed; marking its section\n' >&2
			q3='{"query_failed":true}'
		fi
	else
		printf 'failure-diagnostics: pg() unavailable; reducer-domain sections marked\n' >&2
		q1='{"query_failed":true}' q2='{"query_failed":true}' q3='{"query_failed":true}'
	fi
	if ! printf '{"terraform_state_scopes":%s,"drift_findings_by_finding_kind":%s,"iac_management_finding_count":%s}\n' \
		"${q1}" "${q2}" "${q3}" >"${dest}/reducer-domain.json"; then
		printf '::error title=Failure diagnostics::cannot write %s/reducer-domain.json\n' "${dest}" >&2
		rm -rf "${dest}"
		return 1
	fi
	# The 100MB cap: truncate tails first, fail loud when still over.
	bundle_kb="$(du -sk "${dest}" | cut -f1)"
	case "${bundle_kb}" in '' | *[!0-9]*) bundle_kb=0 ;; esac
	if (( bundle_kb > max_kb )); then
		truncated_tails=true
		for tailf in "${dest}"/log-tails/*.tail; do
			[[ -f "${tailf}" ]] || continue
			tmpf="${tailf}.trunc"
			if tail -n 50 "${tailf}" >"${tmpf}"; then
				mv "${tmpf}" "${tailf}"
			else
				rm -f "${tmpf}"
			fi
		done
		bundle_kb="$(du -sk "${dest}" | cut -f1)"
		case "${bundle_kb}" in '' | *[!0-9]*) bundle_kb=0 ;; esac
		if (( bundle_kb > max_kb )); then
			printf '::error title=Failure diagnostics::bundle %s is %sKB, over the %sKB cap after tail truncation; staging nothing\n' \
				"${dest}" "${bundle_kb}" "${max_kb}" >&2
			rm -rf "${dest}"
			return 1
		fi
	fi
	if (( staged == 0 )); then
		printf '::error title=Failure diagnostics::nothing to stage (no capture, no work_dir files, no query answers); staging nothing\n' >&2
		rm -rf "${dest}"
		return 1
	fi
	if ! printf '{"job":"%s","staged_at":"%s","commit":"%s","capture_files":%d,"snapshot_files":%d,"log_tails":%d,"reducer_domain":"%s","truncated_tails":%s,"bundle_kb":%s}\n' \
		"$(printf '%s' "${job}" | sed 's/\\/\\\\/g; s/"/\\"/g')" \
		"$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		"$(git rev-parse HEAD 2>/dev/null || printf 'unknown')" \
		"${capture_n}" \
		"${snap_n}" \
		"${tails_n}" \
		"$("${q1_ok}" && "${q2_ok}" && "${q3_ok}" && printf 'ok' || printf 'partial')" \
		"${truncated_tails}" \
		"${bundle_kb}" >"${dest}/manifest.json"; then
		printf '::error title=Failure diagnostics::cannot write %s/manifest.json\n' "${dest}" >&2
		rm -rf "${dest}"
		return 1
	fi
	printf '%s\n' "${dest}"
	return 0
}
