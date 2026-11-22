#!/usr/bin/env bash
# Cases S1-S5: pre-push runs each changed package's tests once.
#
# Sourced by scripts/test-pre-push.sh, which provides fail, build_fixture,
# run_fixture, repo_root and temp_root. `go test -race` runs every test the plain
# `go test` runs, so a plain pass over a changed package only repeats the race
# pass, with one exception: a file constrained `//go:build !race` is compiled
# out of the race binary, so its tests run only in the plain pass. The helper
# under test keeps the plain run for exactly those packages (and for any
# selected package the race pass does not cover, such as a fixture consumer).

# shellcheck source=pre-push-test-dedupe.sh
source "${repo_root}/scripts/lib/pre-push-test-dedupe.sh"

# --- S1-S3: the selection helper, driven directly against a scratch tree ---
dedupe_root="${temp_root}/dedupe-unit"
rm -rf "${dedupe_root}"
# The helper reads the driver's go_dir; point it at the scratch tree and put the
# caller's value back afterwards, since later cases share this shell.
saved_go_dir="${go_dir-}"
go_dir="${dedupe_root}/go"
mkdir -p "${go_dir}/internal/plain" "${go_dir}/internal/norace" \
	"${go_dir}/internal/combined" "${go_dir}/internal/raceonly" \
	"${go_dir}/internal/consumer" "${go_dir}/internal/missingdir"
printf 'package plain\n' > "${go_dir}/internal/plain/plain.go"
printf '//go:build !race\n\npackage norace\n' > "${go_dir}/internal/norace/race_off_test.go"
printf '//go:build linux && !race\n\npackage combined\n' > "${go_dir}/internal/combined/x_test.go"
printf '//go:build race\n\npackage raceonly\n' > "${go_dir}/internal/raceonly/race_on_test.go"
printf 'package consumer\n' > "${go_dir}/internal/consumer/consumer.go"
# Two more forms Go still treats as "not in a -race build": a negated group, and
# a constraint line with leading whitespace (go list -race ignores both files).
mkdir -p "${go_dir}/internal/negatedgroup" "${go_dir}/internal/indented"
printf '//go:build !(race || linux)\n\npackage negatedgroup\n' > "${go_dir}/internal/negatedgroup/x_test.go"
printf '  //go:build !race\n\npackage indented\n' > "${go_dir}/internal/indented/x_test.go"
race_list="${dedupe_root}/race-dirs.txt"
printf '%s\n' ./internal/plain ./internal/norace ./internal/combined ./internal/raceonly \
	./internal/negatedgroup ./internal/indented > "${race_list}"

kept="$(printf '%s\n' ./internal/plain ./internal/norace ./internal/combined \
	./internal/raceonly ./internal/consumer ./internal/negatedgroup ./internal/indented \
	| pre_push_plain_test_dirs "${race_list}" | tr '\n' ' ')"
# S1: a changed package with no !race file is covered by the race run -> dropped.
case "${kept}" in *"./internal/plain "*) fail "case S1: a changed package with no !race file kept its plain run (kept: ${kept})" ;; esac
# S2: a !race file (alone, or inside a && constraint) means the race binary
# skips those tests, so the plain run must stay.
case "${kept}" in *"./internal/norace "*) ;; *) fail "case S2: a //go:build !race package lost its plain run (kept: ${kept})" ;; esac
case "${kept}" in *"./internal/combined "*) ;; *) fail "case S2: a '&& !race' package lost its plain run (kept: ${kept})" ;; esac
# S2c: a negated group and an indented constraint line also leave a file out of
# the race binary, so those packages keep their plain run too.
case "${kept}" in *"./internal/negatedgroup "*) ;; *) fail "case S2c: a '!(race || linux)' package lost its plain run (kept: ${kept})" ;; esac
case "${kept}" in *"./internal/indented "*) ;; *) fail "case S2c: an indented '//go:build !race' package lost its plain run (kept: ${kept})" ;; esac
# S3: a race-ONLY file (//go:build race) is not an exclusion from race builds,
# and a selected package the race pass does not cover must stay.
case "${kept}" in *"./internal/raceonly "*) fail "case S3: a //go:build race file wrongly kept the plain run (kept: ${kept})" ;; esac
case "${kept}" in *"./internal/consumer "*) ;; *) fail "case S3: a package outside the race set lost its plain run (kept: ${kept})" ;; esac
# S3b: a package dir that cannot be read is kept, never silently dropped.
printf '%s\n' ./internal/vanished >> "${race_list}"
kept="$(printf '%s\n' ./internal/vanished | pre_push_plain_test_dirs "${race_list}" | tr '\n' ' ')"
case "${kept}" in *"./internal/vanished "*) ;; *) fail "case S3b: an unreadable package dir was dropped instead of kept (kept: ${kept})" ;; esac
go_dir="${saved_go_dir}"

# --- S4-S5: the real driver, with a go stand-in that logs every call ---
go_logging_stub() {
	cat > "$1/bin/go" <<'GOSTUB'
#!/usr/bin/env bash
printf 'go %s\n' "$*" >> "${DRIVER_ARGS_LOG}"
exit 0
GOSTUB
	chmod +x "$1/bin/go"
}

# S4: two changed packages, one with a !race file. Plain `go test` runs only
# the !race one; `go test -race` runs both.
fixture="$(build_fixture case-s4)"
mkdir -p "${fixture}/go/internal/alpha" "${fixture}/go/internal/beta"
printf 'package alpha\n' > "${fixture}/go/internal/alpha/alpha.go"
printf 'package beta\n' > "${fixture}/go/internal/beta/beta.go"
printf '//go:build !race\n\npackage beta\n' > "${fixture}/go/internal/beta/race_off_test.go"
git -C "${fixture}" -c core.hooksPath=/dev/null add go
go_logging_stub "${fixture}"
status="$(run_fixture "${fixture}" 0)"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case S4: expected exit 0, got ${status}"; }
plain_line="$(rg -N '^go test -count=1 ' "${fixture}.args" || true)"
race_line="$(rg -N '^go test -race ' "${fixture}.args" || true)"
[[ "${plain_line}" == *"./internal/beta"* ]] || fail "case S4: plain go test skipped the !race package (${plain_line:-no plain go test line})"
[[ "${plain_line}" != *"./internal/alpha"* ]] || fail "case S4: plain go test still ran alpha, which the race run covers (${plain_line})"
[[ "${race_line}" == *"./internal/alpha"* && "${race_line}" == *"./internal/beta"* ]] || fail "case S4: the race run must cover both changed packages (${race_line:-no race line})"

# S5: every changed package is race-covered -> no plain `go test` call at all,
# the race run still covers it, and the run says why the plain pass was skipped.
fixture="$(build_fixture case-s5)"
mkdir -p "${fixture}/go/internal/alpha"
printf 'package alpha\n' > "${fixture}/go/internal/alpha/alpha.go"
git -C "${fixture}" -c core.hooksPath=/dev/null add go
go_logging_stub "${fixture}"
status="$(run_fixture "${fixture}" 0)"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case S5: expected exit 0, got ${status}"; }
rg -q '^go test -count=1 ' "${fixture}.args" && fail "case S5: plain go test ran although the race run covers every changed package"
rg -q '^go test -race .*\./internal/alpha' "${fixture}.args" || fail "case S5: the race run did not cover the changed package"
rg -q 'race run covers' "${fixture}.log" || fail "case S5: the run did not say why the plain pass was skipped"
