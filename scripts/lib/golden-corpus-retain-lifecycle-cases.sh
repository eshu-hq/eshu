#!/usr/bin/env bash
# Production retain -> dead holder -> running Compose project lifecycle case.
# Sourced by golden-corpus-lock-cases.sh; not intended to run standalone.

rm -f "${lock_file}" "${lock_file}.keep"
retain_probe_home="${lock_home}/retain-probe"
retain_probe_bin="${retain_probe_home}/bin"
retain_probe_calls="${retain_probe_home}/docker.calls"
mkdir -p "${retain_probe_bin}"
printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$*" >>%q\nprintf "retained-container-id\\n"\n' \
	"${retain_probe_calls}" >"${retain_probe_bin}/docker"
chmod +x "${retain_probe_bin}/docker"

retain_out="$(
	GATE_COMPOSE_PROJECT=eshu-gate-retained \
		ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" bash -c '
		set -euo pipefail
		. "$1"
		acquire_live_gate_lock
		retain_live_gate_lock
		printf "MARKER=%s LOCK=%s RAW=%s\n" \
			"$([[ -e "${lock_path}.keep" ]] && echo yes || echo no)" \
			"$([[ -L "${lock_path}" ]] && echo yes || echo no)" \
			"$(<"${lock_path}.keep")"
	' _ "${lock_lib}" 2>&1
)" || fail "retain probe failed: ${retain_out}"
rg --quiet 'MARKER=yes LOCK=yes' <<<"${retain_out}" \
	|| fail "retain must write the marker and leave the lock in place; got: ${retain_out}"

retain_raw="$(<"${lock_file}.keep")"
retain_holder="${retain_raw#v2:}"
retain_holder="${retain_holder%%:*}"
[[ "${retain_holder}" =~ ^[0-9]+$ ]] \
	|| fail "retained marker must record its holder pid (got: ${retain_raw})"
if ps -p "${retain_holder}" >/dev/null 2>&1; then
	fail "retain lifecycle holder ${retain_holder} must have exited before the later acquire"
fi
[[ "${retain_raw}" == v2:*:*:*:eshu-gate-retained:* ]] \
	|| fail "retain lifecycle must record compose project eshu-gate-retained (got: ${retain_raw})"

set +e
retain_block_out="$(
	PATH="${retain_probe_bin}:${PATH}" ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" bash -c '
		set -euo pipefail
		. "$1"
		acquire_live_gate_lock
	' _ "${lock_lib}" 2>&1
)"
retain_block_status=$?
set -e
[[ "${retain_block_status}" -ne 0 ]] \
	|| fail "a retained running project with a dead holder must block a later run"
rg --fixed-strings --quiet -- \
	'still has compose project eshu-gate-retained running on the fixed host ports' \
	<<<"${retain_block_out}" \
	|| fail "dead-holder refusal must name the running retained project; got: ${retain_block_out}"
[[ "$(wc -l <"${retain_probe_calls}" | tr -d ' ')" == "1" ]] \
	|| fail "retained lifecycle must make exactly one Compose ps query"
rg --fixed-strings --quiet -- 'compose -p eshu-gate-retained ps -q' "${retain_probe_calls}" \
	|| fail "retained lifecycle did not query the recorded Compose project"

rm -rf "${retain_probe_home}"
rm -f "${lock_file}" "${lock_file}.keep" "${lock_file}.reclaim"
retain_lifecycle_cases_completed=1
