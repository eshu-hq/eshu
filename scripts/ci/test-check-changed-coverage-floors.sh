#!/usr/bin/env bash
# Self-test for scripts/ci/check-changed-coverage-floors.sh
# (Refs eshu-hq/eshu#10983). Hermetic: temp git repo plus a stub `go` that
# writes canned profiles; no network, no real test run.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
glue="${repo_root}/scripts/ci/check-changed-coverage-floors.sh"
verifier="${repo_root}/scripts/verify-coverage-floors.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

if [[ -f "${glue}" ]]; then
	record_pass "glue script exists"
else
	record_fail "glue script must exist"
fi
bash -n "${glue}" 2>/dev/null && record_pass "glue script has valid shell syntax" || record_fail "glue script shell syntax is invalid"

# Stub `go`: records its test args, writes a canned profile to -coverprofile.
stub_bin="${tmp_root}/stub-bin"
mkdir -p "${stub_bin}"
cat >"${stub_bin}/go" <<'STUB'
#!/usr/bin/env bash
echo "$@" >>"${GO_STUB_ARGS}"
[ "${GO_STUB_MODE:-pass}" = fail ] && { printf 'FAIL\texample.com/go/alpha\n' >&2; exit 1; }
for arg in "$@"; do case "$arg" in -coverprofile=*) printf 'mode: count\nexample.com/go/alpha/a.go:1.1,2.1 8 1\nexample.com/go/alpha/a.go:3.1,4.1 2 0\n' >"${arg#-coverprofile=}";; esac; done
STUB
chmod +x "${stub_bin}/go"

# Fixture repo: module example.com/go, packages alpha (floor 80) and gated.
make_repo() {
	local dir="$1"
	mkdir -p "${dir}/go/alpha" "${dir}/go/gated" "${dir}/specs" "${dir}/scripts"
	printf 'module example.com/go\n\ngo 1.24\n' >"${dir}/go/go.mod"
	printf 'package alpha\n' >"${dir}/go/alpha/a.go"
	printf 'package gated\n' >"${dir}/go/gated/g.go"
	cat >"${dir}/specs/coverage-floors.v1.yaml" <<'YAML'
schema_version: 1
whole_module_floor: 74
env_gated:
  - example.com/go/gated
packages:
  example.com/go/alpha: 80
YAML
	cp "${verifier}" "${dir}/scripts/verify-coverage-floors.sh"
	chmod +x "${dir}/scripts/verify-coverage-floors.sh"
	git -C "${dir}" init -b main >/dev/null 2>&1
	git -C "${dir}" -c user.email=t@t -c user.name=t add -A
	git -C "${dir}" -c user.email=t@t -c user.name=t commit -qm base
	git -C "${dir}" rev-parse HEAD
}

run_glue() {
	local repo="$1"
	shift
	(
		cd "${repo}" || exit 2
		PATH="${stub_bin}:${PATH}" \
			GO_STUB_ARGS="${tmp_root}/go-args.txt" \
			GO_STUB_MODE="${GO_STUB_MODE:-pass}" \
			ESHU_FLOORS_VERIFIER="${repo}/scripts/verify-coverage-floors.sh" \
			"$@" >"${tmp_root}/glue.log" 2>&1
	)
}

# Case 1: alpha changed, profile measures 80% at floor 80 -> pass, and the
# stub saw exactly the alpha package.
repo1="${tmp_root}/repo1"
base1="$(make_repo "${repo1}")"
: >"${tmp_root}/go-args.txt"
printf 'package alpha // v2\n' >"${repo1}/go/alpha/a.go"
git -C "${repo1}" -c user.email=t@t -c user.name=t commit -qam change
status=0
run_glue "${repo1}" "${glue}" --base-rev "${base1}" || status=$?
if [[ "${status}" -eq 0 ]]; then
	record_pass "changed package at floor passes"
else
	record_fail "changed package at floor exits ${status}, want 0"
fi
if rg --fixed-strings --quiet -- "./alpha" "${tmp_root}/go-args.txt"; then
	record_pass "glue tests the changed package"
else
	record_fail "glue must pass the changed package to go test"
fi
if rg --fixed-strings --quiet -- "gated" "${tmp_root}/go-args.txt"; then
	record_fail "glue must not test untouched packages"
else
	record_pass "glue tests only changed packages"
fi

# Case 2: gated-only change -> env_gated excluded, go test runs zero packages
# is skipped, verifier passes without a profile.
repo2="${tmp_root}/repo2"
base2="$(make_repo "${repo2}")"
: >"${tmp_root}/go-args.txt"
printf 'package gated // v2\n' >"${repo2}/go/gated/g.go"
git -C "${repo2}" -c user.email=t@t -c user.name=t commit -qam change
status=0
run_glue "${repo2}" "${glue}" --base-rev "${base2}" || status=$?
if [[ "${status}" -eq 0 ]]; then
	record_pass "env_gated-only change passes without testing"
else
	record_fail "env_gated-only change exits ${status}, want 0"
fi
if [[ -s "${tmp_root}/go-args.txt" ]]; then
	record_fail "glue must not invoke go test for env_gated-only changes"
else
	record_pass "glue skips go test for env_gated-only changes"
fi

# Case 3: non-Go change -> pass without invoking go test.
repo3="${tmp_root}/repo3"
base3="$(make_repo "${repo3}")"
: >"${tmp_root}/go-args.txt"
printf '# docs\n' >"${repo3}/README.md"
git -C "${repo3}" -c user.email=t@t -c user.name=t add -A
git -C "${repo3}" -c user.email=t@t -c user.name=t commit -qam docs
status=0
run_glue "${repo3}" "${glue}" --base-rev "${base3}" || status=$?
if [[ "${status}" -eq 0 ]]; then
	record_pass "non-Go change passes without testing"
else
	record_fail "non-Go change exits ${status}, want 0"
fi
if [[ -s "${tmp_root}/go-args.txt" ]]; then
	record_fail "glue must not invoke go test for non-Go changes"
else
	record_pass "glue skips go test for non-Go changes"
fi

# Case 5: no --base-rev falls back to origin/main (push-event shape).
repo5="${tmp_root}/repo5"
base5="$(make_repo "${repo5}")"
git -C "${repo5}" update-ref refs/remotes/origin/main "${base5}"
: >"${tmp_root}/go-args.txt"
printf 'package alpha // v2\n' >"${repo5}/go/alpha/a.go"
git -C "${repo5}" -c user.email=t@t -c user.name=t commit -qam change
status=0
(
	cd "${repo5}" || exit 2
	PATH="${stub_bin}:${PATH}" \
		GO_STUB_ARGS="${tmp_root}/go-args.txt" \
		GO_STUB_MODE=pass \
		ESHU_FLOORS_VERIFIER="${repo5}/scripts/verify-coverage-floors.sh" \
		env -u GITHUB_BASE_REF "${glue}" >"${tmp_root}/glue.log" 2>&1
) || status=$?
if [[ "${status}" -eq 0 ]]; then
	record_pass "missing base rev falls back to origin/main"
else
	record_fail "origin/main fallback exits ${status}, want 0"
fi

# Case 6: go test failure propagates.
repo4="${tmp_root}/repo4"
base4="$(make_repo "${repo4}")"
printf 'package alpha // v2\n' >"${repo4}/go/alpha/a.go"
git -C "${repo4}" -c user.email=t@t -c user.name=t commit -qam change
status=0
GO_STUB_MODE=fail run_glue "${repo4}" "${glue}" --base-rev "${base4}" || status=$?
if [[ "${status}" -ne 0 ]]; then
	record_pass "go test failure fails the check"
else
	record_fail "go test failure must fail the check"
fi

if [[ "${FAIL}" -ne 0 ]]; then
	printf 'check-changed-coverage-floors tests FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
	exit 1
fi

printf 'check-changed-coverage-floors tests passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
