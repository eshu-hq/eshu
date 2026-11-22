#!/usr/bin/env bash
# Cases T3-T4: pre-pr's timing tee never changes the plain test step's result.
#
# Sourced by scripts/test-pre-pr-whole-module-gates.sh, which provides fail,
# temp_root, repo_root and script. Same contract as cases T1-T2 in
# scripts/lib/test-pre-push-timing-cases.sh, for pre-pr.sh's step_test:
#   T3  go fails        -> the step is FAIL and the driver exits non-zero.
#   T4  go passes but the timing log cannot be written -> the step is PASS.
# The scoped race step also tees, but it calls `go run ./cmd/ci-gates` to pick
# its packages, which a stand-in `go` cannot answer, so it is not driven here.

# shellcheck disable=SC2154  # fail, temp_root, repo_root, script: the caller's.

assert_pre_pr_timing_step() {
	local case_name="$1" go_rc="$2" unwritable="$3" fixture status=0
	fixture="${temp_root}/timing-${case_name}"
	rm -rf "${fixture}"
	mkdir -p "${fixture}/scripts/dev" "${fixture}/scripts/lib" "${fixture}/go/internal/alpha" "${fixture}/bin"
	cp "${script}" "${fixture}/scripts/dev/pre-pr.sh"
	cp "${repo_root}"/scripts/lib/pre-pr-*.sh "${fixture}/scripts/lib/"
	cp "${repo_root}/scripts/lib/cgo-cflags.sh" "${fixture}/scripts/lib/"
	cp "${repo_root}/scripts/lib/gate-timing.sh" "${fixture}/scripts/lib/"
	cp "${repo_root}/scripts/dev/gate-timing.py" "${fixture}/scripts/dev/"
	cat > "${fixture}/bin/go" <<'GOSTUB'
#!/usr/bin/env bash
[[ "${1:-}" == test ]] && exit "${FAKE_GO_RC:-0}"
exit 0
GOSTUB
	chmod +x "${fixture}/bin/go"
	cat > "${fixture}/scripts/dev/run-selected-gates.sh" <<'GATE'
#!/usr/bin/env bash
while (( $# )); do
	if [[ "$1" == --report-file ]]; then printf '{}\n' > "$2"; shift; fi
	shift
done
exit 0
GATE
	chmod +x "${fixture}/scripts/dev/run-selected-gates.sh"
	if [[ "${unwritable}" == yes ]]; then
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
	fi
	printf 'baseline\n' > "${fixture}/README.md"
	git -C "${fixture}" init -q
	git -C "${fixture}" -c core.hooksPath=/dev/null add .
	git -C "${fixture}" -c core.hooksPath=/dev/null -c user.name=Test -c user.email=test@example.invalid commit -qm baseline
	git -C "${fixture}" update-ref refs/remotes/origin/main "$(git -C "${fixture}" rev-parse HEAD)"
	printf 'package alpha\n' > "${fixture}/go/internal/alpha/alpha.go"
	git -C "${fixture}" -c core.hooksPath=/dev/null add go
	FAKE_GO_RC="${go_rc}" PATH="${fixture}/bin:${PATH}" bash "${fixture}/scripts/dev/pre-pr.sh" \
		> "${fixture}.log" 2>&1 || status=$?
	if [[ "${go_rc}" != 0 ]]; then
		[[ "${status}" != 0 ]] || { cat "${fixture}.log" >&2; fail "case ${case_name}: a failing go test was reported as success"; }
		rg -q '^FAIL +go test \(changed packages\)' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case ${case_name}: the test step did not fail"; }
	else
		rg -q '^PASS +go test \(changed packages\)' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case ${case_name}: an unwritable timing log failed a passing test step"; }
	fi
}

assert_pre_pr_timing_step t3 1 no
assert_pre_pr_timing_step t4 0 yes
printf 'PASS: pre-pr timing tee leaves the test step result alone\n'
