#!/usr/bin/env bash
# test-verify-workflow-script-checkout.sh - hermetic tests for
# scripts/verify-workflow-script-checkout.sh (#8008). No network and no
# Docker: RED/GREEN cases run the verifier against scratch workflow trees via
# ESHU_WORKFLOW_SCRIPT_CHECKOUT_ROOT, so the committed files are never
# touched.
#
# The seeded RED/GREEN pair (acceptance criterion 4):
#   RED   scripts/fixtures/workflow-script-checkout/red-no-checkout.yml, the
#         pre-fix trivy-image shape (a step running the resolve script with no
#         actions/checkout in the job): the verifier must exit 1 and name it.
#   GREEN the committed tree after the fix, plus the green-with-checkout
#         fixture: the verifier must exit 0.
# Further cases: a checkout AFTER the script step still fails (order
# matters); reusable-call jobs and script-less jobs without a checkout pass;
# commented-out invocations pass; ../scripts, ./scripts, and source forms
# are recognised; unparseable YAML and bad usage fail closed (exit 2, never 0).
# The #8057 pair: a $GITHUB_WORKSPACE/scripts/... call without a checkout
# fails (and passes with one), while an echo of a quoted scripts/ path
# without a checkout passes (it executes no repository script). The
# follow-up pairs: sudo with flags, VAR=value stacked before sudo, and
# quoted VAR values each fail without a checkout, while `bash` as an echo
# argument passes. The #8107 triple: `sudo --`, `sudo -u$USER` (joined
# short-option argument), and `sudo env A=1` each fail without a checkout.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-workflow-script-checkout.sh"
fixtures="${repo_root}/scripts/fixtures/workflow-script-checkout"

command -v yq >/dev/null 2>&1 || {
	echo "test-verify-workflow-script-checkout: required tool not found: yq" >&2
	exit 1
}

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

pass=0
fail=0
record_pass() { pass=$((pass + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { fail=$((fail + 1)); printf 'not ok - %s\n' "$1"; }

# scratch NAME: print a fresh root holding an empty .github/workflows dir.
scratch() {
	local dir="${tmp_root}/$1"
	mkdir -p "${dir}/.github/workflows"
	printf '%s' "${dir}"
}

# run_root DIR: run the verifier against DIR, echoing its combined output;
# the caller reads the exit through $?. (Command substitution strips the
# exit, so cases capture output to a file instead.)
run_root() {
	ESHU_WORKFLOW_SCRIPT_CHECKOUT_ROOT="$1" bash "${verifier}"
}

# 1. The committed tree passes (GREEN). Fails while #8008 is unfixed.
out_file="${tmp_root}/committed.out"
if bash "${verifier}" >"${out_file}" 2>&1; then
	record_pass "committed tree passes"
else
	record_fail "committed tree passes (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 2. Seeded RED: no checkout in the job.
red="$(scratch red)"
cp "${fixtures}/red-no-checkout.yml" "${red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/red.out"
if run_root "${red}" >"${out_file}" 2>&1; then
	record_fail "red-no-checkout fixture fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "red-no-checkout fixture fails naming the job"
	else
		record_fail "red-no-checkout fixture fails naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 3. RED: checkout after the script step still fails.
order="$(scratch order)"
cp "${fixtures}/red-checkout-after-script.yml" "${order}/.github/workflows/fixture.yml"
out_file="${tmp_root}/order.out"
if run_root "${order}" >"${out_file}" 2>&1; then
	record_fail "checkout-after-script fixture fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ]; then
		record_pass "checkout-after-script fixture fails"
	else
		record_fail "checkout-after-script fixture fails (exit ${code}, want 1)"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 4. Seeded GREEN fixture passes.
green="$(scratch green)"
cp "${fixtures}/green-with-checkout.yml" "${green}/.github/workflows/fixture.yml"
out_file="${tmp_root}/green.out"
if run_root "${green}" >"${out_file}" 2>&1; then
	record_pass "green-with-checkout fixture passes"
else
	record_fail "green-with-checkout fixture passes (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 5. Reusable-call jobs and script-less jobs without a checkout pass.
plain="$(scratch plain)"
cat >"${plain}/.github/workflows/fixture.yml" <<'WORKFLOW'
name: Fixture plain
on:
  workflow_dispatch:
jobs:
  caller:
    uses: ./.github/workflows/other.yml
  notice:
    runs-on: ubuntu-latest
    steps:
      - name: Say hello
        run: echo hello
WORKFLOW
out_file="${tmp_root}/plain.out"
if run_root "${plain}" >"${out_file}" 2>&1; then
	record_pass "reusable-call and script-less jobs pass"
else
	record_fail "reusable-call and script-less jobs pass (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 6. Commented-out invocations in a checkout-less job pass.
commented="$(scratch commented)"
cat >"${commented}/.github/workflows/fixture.yml" <<'WORKFLOW'
name: Fixture commented
on:
  workflow_dispatch:
jobs:
  notice:
    runs-on: ubuntu-latest
    steps:
      - name: Say hello
        run: |
          # bash scripts/ci/retired.sh
          echo hi # see scripts/ci/retired.sh
WORKFLOW
out_file="${tmp_root}/commented.out"
if run_root "${commented}" >"${out_file}" 2>&1; then
	record_pass "comment-only mentions pass"
else
	record_fail "comment-only mentions pass (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 7. ../scripts and source forms are recognised: flagged without a checkout,
# passed with one.
rel_red="$(scratch rel-red)"
cat >"${rel_red}/.github/workflows/fixture.yml" <<'WORKFLOW'
name: Fixture relative red
on:
  workflow_dispatch:
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - name: Pre-warm
        working-directory: go
        run: ../scripts/ci/go-mod-download-retry.sh
WORKFLOW
out_file="${tmp_root}/rel-red.out"
if run_root "${rel_red}" >"${out_file}" 2>&1; then
	record_fail "relative ../scripts without checkout fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ]; then
		record_pass "relative ../scripts without checkout fails"
	else
		record_fail "relative ../scripts without checkout fails (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi
# The source form sits inside `bash -c "..."` on the `run:` line on purpose:
# the registry drift check reads a line-start `source scripts/...` as a real
# dependency of this test file and would demand it as a gate trigger.
rel_green="$(scratch rel-green)"
cat >"${rel_green}/.github/workflows/fixture.yml" <<'WORKFLOW'
name: Fixture relative green
on:
  workflow_dispatch:
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - name: Check out repository
        uses: actions/checkout@v5
      - name: Skip dirs
        run: bash -c "source scripts/lib/trivy-skip-dirs.sh"
WORKFLOW
out_file="${tmp_root}/rel-green.out"
if run_root "${rel_green}" >"${out_file}" 2>&1; then
	record_pass "source form with checkout passes"
else
	record_fail "source form with checkout passes (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 8. Fail closed: unparseable YAML exits 2, never 0.
broken="$(scratch broken)"
printf 'jobs:\n  bad: [unclosed\n' >"${broken}/.github/workflows/fixture.yml"
out_file="${tmp_root}/broken.out"
if run_root "${broken}" >"${out_file}" 2>&1; then
	record_fail "unparseable workflow fails closed (exited 0)"
else
	code=$?
	if [ "${code}" -eq 2 ]; then
		record_pass "unparseable workflow fails closed"
	else
		record_fail "unparseable workflow fails closed (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 9. Fail closed: unknown argument exits 2; --help exits 0.
out_file="${tmp_root}/usage.out"
if bash "${verifier}" --bogus >"${out_file}" 2>&1; then
	record_fail "unknown argument fails closed (exited 0)"
else
	code=$?
	if [ "${code}" -eq 2 ]; then
		record_pass "unknown argument fails closed"
	else
		record_fail "unknown argument fails closed (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi
if bash "${verifier}" --help >"${out_file}" 2>&1; then
	record_pass "--help exits 0"
else
	record_fail "--help exits 0 (exit $?)"
fi

# 10. ./scripts form is recognised: flagged without a checkout, passed with one.
dot_red="$(scratch dot-red)"
cp "${fixtures}/red-dot-slash-no-checkout.yml" "${dot_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/dot-red.out"
if run_root "${dot_red}" >"${out_file}" 2>&1; then
	record_fail "dot-slash ./scripts without checkout fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "dot-slash ./scripts without checkout fails naming the job"
	else
		record_fail "dot-slash ./scripts without checkout fails naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi
dot_green="$(scratch dot-green)"
cat >"${dot_green}/.github/workflows/fixture.yml" <<'WORKFLOW'
name: Fixture dot-slash green
on:
  workflow_dispatch:
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - name: Check out repository
        uses: actions/checkout@v5
      - name: Conform
        run: ./scripts/verify_backend_conformance_live.sh
WORKFLOW
out_file="${tmp_root}/dot-green.out"
if run_root "${dot_green}" >"${out_file}" 2>&1; then
	record_pass "dot-slash ./scripts with checkout passes"
else
	record_fail "dot-slash ./scripts with checkout passes (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 11. $GITHUB_WORKSPACE/scripts form is recognised: flagged without a
# checkout, passed with one (#8057).
ws_red="$(scratch ws-red)"
cp "${fixtures}/red-github-workspace-no-checkout.yml" "${ws_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/ws-red.out"
if run_root "${ws_red}" >"${out_file}" 2>&1; then
	record_fail "GITHUB_WORKSPACE scripts without checkout fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "GITHUB_WORKSPACE scripts without checkout fails naming the job"
	else
		record_fail "GITHUB_WORKSPACE scripts without checkout fails naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi
ws_green="$(scratch ws-green)"
cp "${fixtures}/green-github-workspace-with-checkout.yml" "${ws_green}/.github/workflows/fixture.yml"
out_file="${tmp_root}/ws-green.out"
if run_root "${ws_green}" >"${out_file}" 2>&1; then
	record_pass "GITHUB_WORKSPACE scripts with checkout passes"
else
	record_fail "GITHUB_WORKSPACE scripts with checkout passes (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 12. A quoted scripts/ path that is only an echo argument passes (#8057).
echo_arg="$(scratch echo-arg)"
cp "${fixtures}/green-echo-quoted-path.yml" "${echo_arg}/.github/workflows/fixture.yml"
out_file="${tmp_root}/echo-arg.out"
if run_root "${echo_arg}" >"${out_file}" 2>&1; then
	record_pass "echo of a quoted scripts path passes"
else
	record_fail "echo of a quoted scripts path passes (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 13. sudo scripts/ without a checkout fails (previously missed).
sudo_red="$(scratch sudo-red)"
cp "${fixtures}/red-sudo-no-checkout.yml" "${sudo_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/sudo-red.out"
if run_root "${sudo_red}" >"${out_file}" 2>&1; then
	record_fail "sudo scripts without checkout fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "sudo scripts without checkout fails naming the job"
	else
		record_fail "sudo scripts without checkout fails naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 14. Stacked VAR=value prefixes without a checkout fail (previously missed).
stacked_red="$(scratch stacked-red)"
cp "${fixtures}/red-stacked-env-no-checkout.yml" "${stacked_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/stacked-red.out"
if run_root "${stacked_red}" >"${out_file}" 2>&1; then
	record_fail "stacked env scripts without checkout fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "stacked env scripts without checkout fails naming the job"
	else
		record_fail "stacked env scripts without checkout fails naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 15. The ${GITHUB_WORKSPACE} spelling without a checkout fails.
braced_red="$(scratch braced-red)"
cp "${fixtures}/red-braced-workspace-no-checkout.yml" "${braced_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/braced-red.out"
if run_root "${braced_red}" >"${out_file}" 2>&1; then
	record_fail "braced workspace scripts without checkout fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "braced workspace scripts without checkout fails naming the job"
	else
		record_fail "braced workspace scripts without checkout fails naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 16. The ${{ github.workspace }} spelling: fails without a checkout,
# passes with one.
dbl_red="$(scratch dbl-red)"
cp "${fixtures}/red-double-brace-workspace-no-checkout.yml" "${dbl_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/dbl-red.out"
if run_root "${dbl_red}" >"${out_file}" 2>&1; then
	record_fail "double-brace workspace scripts without checkout fails (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "double-brace workspace scripts without checkout fails naming the job"
	else
		record_fail "double-brace workspace scripts without checkout fails naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi
dbl_green="$(scratch dbl-green)"
cp "${fixtures}/green-double-brace-workspace-with-checkout.yml" "${dbl_green}/.github/workflows/fixture.yml"
out_file="${tmp_root}/dbl-green.out"
if run_root "${dbl_green}" >"${out_file}" 2>&1; then
	record_pass "double-brace workspace scripts with checkout passes"
else
	record_fail "double-brace workspace scripts with checkout passes (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 17. sudo with flags (-E, -u <user>) without a checkout fails.
sudo_flags_red="$(scratch sudo-flags-red)"
cp "${fixtures}/red-sudo-flags-no-checkout.yml" "${sudo_flags_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/sudo-flags-red.out"
if run_root "${sudo_flags_red}" >"${out_file}" 2>&1; then
	record_fail "sudo-flag scripts without checkout fail (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "sudo-flag scripts without checkout fail naming the job"
	else
		record_fail "sudo-flag scripts without checkout fail naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 18. A VAR=value prefix stacked before sudo without a checkout fails.
env_sudo_red="$(scratch env-sudo-red)"
cp "${fixtures}/red-env-before-sudo-no-checkout.yml" "${env_sudo_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/env-sudo-red.out"
if run_root "${env_sudo_red}" >"${out_file}" 2>&1; then
	record_fail "env-before-sudo scripts without checkout fail (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "env-before-sudo scripts without checkout fail naming the job"
	else
		record_fail "env-before-sudo scripts without checkout fail naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 19. Quoted VAR=value prefixes (values with spaces) without a checkout fail.
quoted_env_red="$(scratch quoted-env-red)"
cp "${fixtures}/red-quoted-env-no-checkout.yml" "${quoted_env_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/quoted-env-red.out"
if run_root "${quoted_env_red}" >"${out_file}" 2>&1; then
	record_fail "quoted-env scripts without checkout fail (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "quoted-env scripts without checkout fail naming the job"
	else
		record_fail "quoted-env scripts without checkout fail naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 20. `bash` as an echo argument is not an execution: passes (#8057).
echo_bash_arg="$(scratch echo-bash-arg)"
cp "${fixtures}/green-echo-bash-arg.yml" "${echo_bash_arg}/.github/workflows/fixture.yml"
out_file="${tmp_root}/echo-bash-arg.out"
if run_root "${echo_bash_arg}" >"${out_file}" 2>&1; then
	record_pass "echo of a bash scripts path passes"
else
	record_fail "echo of a bash scripts path passes (exit $?)"
	sed 's/^/  /' "${out_file}"
fi

# 21. `sudo --` (end-of-options) without a checkout fails (#8107).
eoo_red="$(scratch eoo-red)"
cp "${fixtures}/red-sudo-end-of-options-no-checkout.yml" "${eoo_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/eoo-red.out"
if run_root "${eoo_red}" >"${out_file}" 2>&1; then
	record_fail "sudo end-of-options scripts without checkout fail (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "sudo end-of-options scripts without checkout fail naming the job"
	else
		record_fail "sudo end-of-options scripts without checkout fail naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 22. `sudo -u$USER` (joined short-option argument) without a checkout
# fails (#8107).
joined_red="$(scratch joined-red)"
cp "${fixtures}/red-sudo-joined-option-no-checkout.yml" "${joined_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/joined-red.out"
if run_root "${joined_red}" >"${out_file}" 2>&1; then
	record_fail "sudo joined-option scripts without checkout fail (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "sudo joined-option scripts without checkout fail naming the job"
	else
		record_fail "sudo joined-option scripts without checkout fail naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

# 23. `sudo env A=1` without a checkout fails (#8107).
sudo_env_red="$(scratch sudo-env-red)"
cp "${fixtures}/red-sudo-env-no-checkout.yml" "${sudo_env_red}/.github/workflows/fixture.yml"
out_file="${tmp_root}/sudo-env-red.out"
if run_root "${sudo_env_red}" >"${out_file}" 2>&1; then
	record_fail "sudo env scripts without checkout fail (exited 0)"
else
	code=$?
	if [ "${code}" -eq 1 ] && grep -q "job <image-scan>" "${out_file}"; then
		record_pass "sudo env scripts without checkout fail naming the job"
	else
		record_fail "sudo env scripts without checkout fail naming the job (exit ${code})"
		sed 's/^/  /' "${out_file}"
	fi
fi

printf 'test-verify-workflow-script-checkout: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" -eq 0 ]
