#!/usr/bin/env bash
# Cases T1-T2: the timing tee must never change a test step's result.
#
# Sourced by scripts/test-pre-push.sh, which provides fail, build_fixture,
# run_fixture and temp_root. pre-push.sh pipes `go test` through
# `tee -a "${ESHU_TIMING_TEST_LOG}"` so the recorder can read per-package
# seconds. A pipeline reports its last command, so the step has to return
# `${PIPESTATUS[0]}` (go's own status) or the tee decides the result:
#   T1  go fails        -> the step is FAIL and the driver exits non-zero.
#   T2  go passes but the timing log cannot be written (tee exits 1)
#                       -> the step is still PASS and the driver exits 0.
# Each case has a planted bug it catches: T1 fails if the step returns 0, T2
# fails if the step returns the pipeline's status instead of go's.

# shellcheck disable=SC2154  # fail, build_fixture, run_fixture, temp_root: the caller's.

timing_case_fixture() {
	local name="$1" fixture
	fixture="$(build_fixture "${name}")"
	mkdir -p "${fixture}/go/internal/alpha"
	printf 'package alpha\n' > "${fixture}/go/internal/alpha/alpha.go"
	# A //go:build !race file keeps the plain `go test` step running for this
	# package: pre-push skips the plain step for a changed package that the race
	# step already covers (scripts/lib/pre-push-test-dedupe.sh), and these cases
	# need both tee paths exercised.
	printf '//go:build !race\n\npackage alpha\n' > "${fixture}/go/internal/alpha/race_off_test.go"
	git -C "${fixture}" -c core.hooksPath=/dev/null add go
	# Only `go test` takes the status from FAKE_GO_RC; build and vet succeed.
	cat > "${fixture}/bin/go" <<'GOSTUB'
#!/usr/bin/env bash
[[ "${1:-}" == test ]] && exit "${FAKE_GO_RC:-0}"
exit 0
GOSTUB
	chmod +x "${fixture}/bin/go"
	printf '%s\n' "${fixture}"
}

# T1: a failing go test fails both test steps, through the tee.
fixture="$(timing_case_fixture case-t1)"
status="$(FAKE_GO_RC=1 run_fixture "${fixture}" 0)"
[[ "${status}" != "0" ]] || { cat "${fixture}.log" >&2; fail "case T1: a failing go test was reported as success"; }
rg -q '^FAIL +go test \(changed packages\)' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case T1: the plain test step did not fail"; }
rg -q '^FAIL +go test -race \(changed packages\)' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case T1: the race step did not fail"; }

# T2: go passes, but the timing logs cannot be written. The stand-in lib runs
# the real init, then points both logs at a directory that does not exist.
fixture="$(timing_case_fixture case-t2)"
cp "${fixture}/scripts/lib/gate-timing.sh" "${fixture}/scripts/lib/gate-timing-real.sh"
cat > "${fixture}/scripts/lib/gate-timing.sh" <<'UNWRITABLE'
#!/usr/bin/env bash
source "$(dirname "${BASH_SOURCE[0]}")/gate-timing-real.sh"
eval "$(declare -f eshu_timing_init | sed '1s/eshu_timing_init/eshu_timing_init_real/')"
eshu_timing_init() {
	eshu_timing_init_real "$@"
	ESHU_TIMING_TEST_LOG="/nonexistent-timing-dir/test.log"
	ESHU_TIMING_RACE_LOG="/nonexistent-timing-dir/race.log"
}
UNWRITABLE
status="$(FAKE_GO_RC=0 run_fixture "${fixture}" 0)"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case T2: an unwritable timing log turned a passing run red (exit ${status})"; }
rg -q '^PASS +go test \(changed packages\)' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case T2: the plain test step did not pass"; }
rg -q '^PASS +go test -race \(changed packages\)' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case T2: the race step did not pass"; }
