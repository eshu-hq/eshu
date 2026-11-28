#!/usr/bin/env bash
# Cases N1-N3: nested Go modules run from their own module roots
# (eshu-hq/eshu-api#456).
#
# Sourced by scripts/test-pre-push.sh, which provides fail, build_fixture,
# run_fixture, repo_root and temp_root. changed_go_files/changed_go_dirs keep
# their contracts; the drivers partition the selected ./-relative dirs at
# execution time by nearest go.mod (scripts/lib/pre-pr-go-paths.sh) and run
# each module group from its own root, because the parent go tool cannot
# resolve packages that live in a nested module.

# --- N1: unit: the partition groups by nearest go.mod; longest-prefix wins ---
n1_go="${temp_root}/partition-unit/go"
rm -rf "${temp_root}/partition-unit"
mkdir -p "${n1_go}/apilib/admin" "${n1_go}/apilib/sub/deep" \
	"${n1_go}/internal/pkg" "${n1_go}/internal/fix/testdata/m"
printf 'module example.invalid/fixture\n\ngo 1.26.10\n' > "${n1_go}/go.mod"
printf 'module example.invalid/apilib\n\ngo 1.26.10\n' > "${n1_go}/apilib/go.mod"
printf 'module example.invalid/sub\n\ngo 1.26.10\n' > "${n1_go}/apilib/sub/go.mod"
# A go.mod under a testdata segment is an intentionally-uncompilable fixture:
# it must never become a nested root (mirrors precommit-go.sh).
printf 'module example.invalid/tfixture\n\ngo 1.26.10\n' > "${n1_go}/internal/fix/testdata/m/go.mod"
saved_go_dir="${go_dir-}"
go_dir="${n1_go}"
unset pre_pr_nested_roots_init
# shellcheck source=lib/pre-pr-go-paths.sh
source "${repo_root}/scripts/lib/pre-pr-go-paths.sh"
n1_roots="$(pre_pr_nested_module_roots)"
[[ "${n1_roots}" == "$(printf 'apilib\napilib/sub')" ]] ||
	fail "case N1: nested roots = [${n1_roots}], want apilib + apilib/sub only (go/ root excluded, testdata pruned)"
pre_pr_partition_go_dirs <<'DIRS'
./.
./internal/pkg
./internal/parser/...
./apilib
./apilib/admin
./apilib/sub
./apilib/sub/deep
./internal/fix/testdata/m
DIRS
n1_main="$(printf '%s\n' ${main_go_dirs[@]+"${main_go_dirs[@]}"})"
[[ "${n1_main}" == "$(printf './.\n./internal/pkg\n./internal/parser/...\n./internal/fix/testdata/m')" ]] ||
	fail "case N1: main = [${n1_main}], want ./. ./internal/pkg ./internal/parser/... and the testdata dir (never routed)"
n1_nested="$(printf '%s\n' ${nested_go_runs[@]+"${nested_go_runs[@]}"})"
[[ "${n1_nested}" == "$(printf 'go/apilib|.\ngo/apilib|./admin\ngo/apilib/sub|.\ngo/apilib/sub|./deep')" ]] ||
	fail "case N1: nested = [${n1_nested}], want root|subpkg with . at the module root and longest-prefix below"
n1_grouped="$(printf '%s\n' ${nested_go_roots[@]+"${nested_go_roots[@]}"})"
[[ "${n1_grouped}" == "$(printf 'go/apilib\ngo/apilib/sub')" ]] ||
	fail "case N1: nested roots = [${n1_grouped}], want one entry per module root for the per-group runs"
go_dir="${saved_go_dir}"

# build_nested_fixture extends build_fixture with a nested module (go/nest)
# and a changed package in each module, plus !race files so the plain `go
# test` pass is not skipped as race-covered (see cases S1-S5).
build_nested_fixture() {
	local name="$1" fixture
	fixture="$(build_fixture "${name}")"
	mkdir -p "${fixture}/go/nest"
	printf 'module example.invalid/fixture/nest\n\ngo 1.26.10\n' > "${fixture}/go/nest/go.mod"
	printf 'package nest\n' > "${fixture}/go/nest/nest.go"
	printf '//go:build !race\n\npackage nest\n' > "${fixture}/go/nest/race_off_test.go"
	printf 'package rootpkg\n' > "${fixture}/go/root.go"
	printf '//go:build !race\n\npackage rootpkg\n' > "${fixture}/go/race_off_test.go"
	git -C "${fixture}" -c core.hooksPath=/dev/null add go
	printf '%s\n' "${fixture}"
}

# log_cwd_go_stub replaces the fixture's go stand-in with one that logs the
# directory each go invocation ran from, so the assertions below pin which
# module root served each package. $2 is the exit behavior: 0, or
# "fail-in-nest" for the negative control.
log_cwd_go_stub() {
	if [[ "$2" == "fail-in-nest" ]]; then
		cat > "$1/bin/go" <<'GOSTUB'
#!/usr/bin/env bash
printf 'cwd=%s go %s\n' "${PWD}" "$*" >> "${DRIVER_ARGS_LOG}"
case "${PWD}" in */go/nest*) exit 7 ;; esac
exit 0
GOSTUB
	else
		cat > "$1/bin/go" <<'GOSTUB'
#!/usr/bin/env bash
printf 'cwd=%s go %s\n' "${PWD}" "$*" >> "${DRIVER_ARGS_LOG}"
exit 0
GOSTUB
	fi
	chmod +x "$1/bin/go"
}

# --- N2: the driver runs test/race/build/vet once per module root ---
fixture="$(build_nested_fixture case-n2)"
log_cwd_go_stub "${fixture}" ok
status="$(run_fixture "${fixture}" 0)"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case N2: expected exit 0, got ${status}"; }
n2_count() { rg -c -F -- "$1" "${fixture}.args" || true; }
# Portable "exactly once" without relying on rg -c's exit code on no match.
n2_expect_once() {
	[[ "$(n2_count "$1")" == "1" ]] || \
		fail "case N2: expected exactly one '$1', got $(n2_count "$1") (see ${fixture}.args)"
}
n2_expect_once "cwd=${fixture}/go go test -count=1 ./."
n2_expect_once "cwd=${fixture}/go/nest go test -count=1 ."
n2_expect_once "cwd=${fixture}/go go test -race -count=1 -timeout 900s ./."
n2_expect_once "cwd=${fixture}/go/nest go test -race -count=1 -timeout 900s ."
n2_expect_once "cwd=${fixture}/go go build -o /dev/null ./."
n2_expect_once "cwd=${fixture}/go/nest go build -o /dev/null ."
n2_expect_once "cwd=${fixture}/go go vet ./."
n2_expect_once "cwd=${fixture}/go/nest go vet ."
# No nested package may run with go/ as its working directory: the parent go
# tool resolves it to zero files, so the run would pass vacuously.
if rg -F -- "cwd=${fixture}/go go " "${fixture}.args" | rg -Fq -- "./nest"; then
	fail "case N2: a nested-module package ran from go/ instead of its own root"
fi

# --- N3 (negative control): a failing nested-module test fails the run ---
fixture="$(build_nested_fixture case-n3)"
log_cwd_go_stub "${fixture}" fail-in-nest
status="$(run_fixture "${fixture}" 0)"
[[ "${status}" != "0" ]] || fail "case N3: a failing nested-module test run was accepted (exit 0)"
rg -q -- '^FAIL  go test' "${fixture}.log" || fail "case N3: the go test step did not report FAIL for the nested failure"
