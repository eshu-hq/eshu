#!/usr/bin/env bash
# Usage cases for scripts/collector-cutover-rollback.sh (#8895): input
# validation, the environment knobs, and the help text.
# Sourced by scripts/test-collector-cutover-rollback.sh after the harness.

# The harness defines these variables and the entry script sources this file.
# shellcheck disable=SC2154,SC2034

# --- identifier lists never glob or overlap (F8) -------------------------------------

# A '*' must be rejected, not expanded against the working directory into the
# names of files that happen to pass the token check.
globdir="${tmp_root}/globdir"
mkdir -p "${globdir}"
: >"${globdir}/ext-1"
: >"${globdir}/tree-1"
new_state glob
rc=0
out="$(cd "${globdir}" && PATH="${bin_dir}:${PATH}" "${script}" plan "${ARGS[@]}" --external-instance '*' 2>&1)" || rc=$?
expect_rc "a glob in --external-instance is rejected, not expanded" 2
rc=0
out="$(cd "${globdir}" && PATH="${bin_dir}:${PATH}" "${script}" plan "${ARGS[@]}" --in-tree-instance '*' 2>&1)" || rc=$?
expect_rc "a glob in --in-tree-instance is rejected, not expanded" 2
run_script plan "${ARGS[@]}" --external-instance 'ext-1,'
expect_rc "a trailing comma is rejected" 2
run_script plan "${ARGS[@]}" --external-instance 'ext-1,,ext-2'
expect_rc "an empty list entry is rejected" 2
run_script plan "${ARGS[@]}" --external-instance 'ext-1,tree-1'
expect_rc "an instance named as both external and in-tree is rejected" 2
expect_out "the overlap names the instance" "both external and in-tree"
run_script plan "${ARGS[@]}" --external-instance 'ext-1,ext-2'
expect_rc "several external instances are accepted" 0

# --- environment knobs (F9) --------------------------------------------------------------

new_state env-defaults
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a default run succeeds" 0
expect_call "kubectl calls carry the default request timeout" "--request-timeout=30s"
expect_call "psql runs with the default connect timeout" "connect_timeout=10"
expect_call "the replay window carries the default clock-skew margin" "skew=30"

new_state env-custom
rc=0
out="$(PATH="${bin_dir}:${PATH}" ROLLBACK_POLL_SECONDS=0.05 ROLLBACK_KUBECTL_REQUEST_TIMEOUT=7s \
	ROLLBACK_CLOCK_SKEW_SECONDS=45 PGCONNECT_TIMEOUT=3 \
	"${script}" run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}" 2>&1)" || rc=$?
expect_rc "a run with custom timeouts succeeds" 0
expect_call "the kubectl request timeout is configurable" "--request-timeout=7s"
expect_call "an operator PGCONNECT_TIMEOUT is kept" "connect_timeout=3"
expect_call "the clock-skew margin is configurable" "skew=45"
rc=0
out="$(PATH="${bin_dir}:${PATH}" ROLLBACK_KUBECTL_REQUEST_TIMEOUT='30; rm -rf /' "${script}" plan "${ARGS[@]}" 2>&1)" || rc=$?
expect_rc "an unsafe request timeout is rejected" 2
rc=0
out="$(PATH="${bin_dir}:${PATH}" ROLLBACK_CLOCK_SKEW_SECONDS=soon "${script}" plan "${ARGS[@]}" 2>&1)" || rc=$?
expect_rc "a non-numeric clock-skew margin is rejected" 2

# --- help text matches the code (F10) --------------------------------------------------------

run_script --help
expect_rc "--help exits 0" 0
expect_out "help says --since is required only with --from-step 5" "required with --from-step 5"
expect_no_out "help does not claim --since is needed for step 6" "5 or 6"
for flag in --report-drift --capture-keys --drift-keys; do
	expect_out "help documents ${flag}" "${flag}"
done
new_state since-5
run_script run --yes --from-step 5 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "--from-step 5 without --since is a usage error" 2
