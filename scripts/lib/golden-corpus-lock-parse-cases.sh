#!/usr/bin/env bash
# Payload-parsing edge cases for the live-gate mutex, split out of
# golden-corpus-lock-cases.sh to keep both chunks under the 500-line cap.
#
# Sourced, never executed: runs in the caller's shell and uses its
# ${lock_lib}, ${lock_file}, ${lock_home}, try_acquire(), fail() and
# require_lock() (all defined earlier in golden-corpus-lock-cases.sh, which
# sources this file). The `golden-corpus-*` name is deliberate: all three
# trigger lists already glob scripts/lib/golden-corpus-*.sh.
#
# Both cases below are payload-PARSING defects: the fingerprint the lock
# depends on was computed or read incorrectly, not a race in the mutex
# machinery itself (that class lives in golden-corpus-lock-race-cases.sh).
# Both were found in the #5826 review reading a live lock as stale and
# reclaiming it - the mutex failing OPEN, which is worse than the pid-reuse
# bug the start-id fingerprint exists to close.

# TZ/locale dependence (#5826 review, P1): `lstart` renders in the CALLER's
# ambient TZ/locale, so the SAME live pid can fingerprint differently
# depending on which TZ happened to be set when each side computed it. A
# holder recorded under one TZ must still be recognized as the SAME live pid
# by an acquirer running under a completely different TZ - normalization has
# to live inside start_id_for_pid, not depend on whatever the caller's
# environment happens to be. Reproduced against the unfixed helper: a holder
# fingerprinted under TZ=UTC read back under TZ=Asia/Tokyo mismatched and the
# stale-reclaim path fired on a demonstrably live pid, destroying its lock.
utc_startid="$(TZ=UTC ps -o lstart= -p "$$" | tr -cd '0-9')"
tokyo_startid="$(TZ=Asia/Tokyo ps -o lstart= -p "$$" | tr -cd '0-9')"
[[ "${utc_startid}" != "${tokyo_startid}" ]] \
	|| fail "TZ harness has no power: TZ=UTC and TZ=Asia/Tokyo produced the same raw fingerprint for pid $$, so this case cannot distinguish normalized from unnormalized"
rm -f "${lock_file}"
ln -s "$$:${utc_startid}:/nonexistent/written-under-utc" "${lock_file}"
set +e
crosstz_out="$(
	ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" TZ=Asia/Tokyo bash -c '
		set -euo pipefail
		. "$1"
		acquire_live_gate_lock
		printf "ACQUIRED=%s\n" "${lock_path}"
	' _ "${lock_lib}" 2>&1
)"
crosstz_status=$?
set -e
[[ "${crosstz_status}" -ne 0 ]] \
	|| fail "a live holder fingerprinted under one TZ must still block an acquirer running under a DIFFERENT TZ, got exit 0 (lock reclaimed): ${crosstz_out}"
rg --quiet 'another live gate is already running' <<<"${crosstz_out}" \
	|| fail "cross-TZ live holder must be reported as running, not silently reclaimed; got: ${crosstz_out}"
rm -f "${lock_file}"

# Malformed single-field payload (#5826 review, P3): a bare pid with NO colon
# at all (not even the two-field legacy format) must be treated as malformed,
# not parsed as if it carried a start-id. Without an arity check, bash's
# `${holder#*:}` / `${holder%%:*}` both fall through UNCHANGED when there is
# no colon to match, so holder_startid silently becomes the pid string
# itself - numeric, so it passes the digit-only guard as a "real" start-id
# and gets compared against the pid's ACTUAL start-id fingerprint, which it
# will essentially never equal. A genuinely live pid then misreads as an
# unconfirmed mismatch and its lock gets reclaimed out from under it.
rm -f "${lock_file}"
ln -s "$$" "${lock_file}"
set +e
bare_out="$(try_acquire)"
bare_status=$?
set -e
[[ "${bare_status}" -ne 0 ]] \
	|| fail "a live pid recorded as a bare colon-less payload must still block, got exit 0 (lock reclaimed): ${bare_out}"
rg --quiet 'another live gate is already running' <<<"${bare_out}" \
	|| fail "bare-pid live holder must be reported as running, not silently reclaimed; got: ${bare_out}"
rm -f "${lock_file}"

# Completion sentinel: `bash -n` only catches syntax errors. A chunk gutted to
# comments, or one that returns early, sources cleanly and would otherwise
# skip every case in it while the run still reported pass.
lock_parse_cases_completed=1
