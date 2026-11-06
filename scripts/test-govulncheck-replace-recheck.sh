#!/usr/bin/env bash
#
# Executable regression test for scripts/ci/govulncheck-replace-recheck.sh
# (#10212). It runs the REAL script against stub `go` and `govulncheck`
# binaries and a throwaway git repo, with canned `govulncheck -mode query`
# output, and asserts the exit code of every planted case:
#
#   green: a known advisory whose regression tests pass with the replacement
#          and fail on upstream                                  -> exit 0
#   green: a replaced module with no advisory                    -> exit 0
#   red:   an advisory ID no test is named for (a NEW finding)   -> exit 1
#   red:   an advisory that now has a fixed version              -> exit 1
#   red:   tests that also pass on upstream (no differential)    -> exit 1
#   red:   dropping the replace breaks the build, not the tests  -> exit 1
#   red:   tests that fail even with the replacement             -> exit 1
#   red:   a local-path replace                                  -> exit 1
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/ci/govulncheck-replace-recheck.sh"
[ -x "${script}" ] || { printf 'test-govulncheck-replace-recheck: missing executable %s\n' "${script}" >&2; exit 1; }
command -v jq >/dev/null || { printf 'test-govulncheck-replace-recheck: jq is required\n' >&2; exit 1; }

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT
failures=0
fail() { printf 'test-govulncheck-replace-recheck: FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }
pass() { printf 'test-govulncheck-replace-recheck: ok: %s\n' "$*"; }

# Stub go: `list -m -json all` prints $STUB_MODULES; `mod edit` is a no-op;
# `test` consults STUB_REPLACED_TEST for the real go.mod and STUB_UPSTREAM_TEST
# when -modfile= is passed (pass | fail | build).
bin="${tmp_root}/bin"
mkdir -p "${bin}"
cat >"${bin}/go" <<'STUB'
#!/usr/bin/env bash
case "$1" in
list) cat "${STUB_MODULES}"; exit 0 ;;
mod) exit 0 ;;
test)
	mode="${STUB_REPLACED_TEST}"
	for arg in "$@"; do
		case "${arg}" in -modfile=*) mode="${STUB_UPSTREAM_TEST}" ;; esac
	done
	case "${mode}" in
	pass) echo "ok  	example/pkg	0.01s"; exit 0 ;;
	fail) echo "--- FAIL: TestPlantedGO20266909 (0.00s)"; echo "FAIL"; exit 1 ;;
	build) echo "FAIL	example/pkg [build failed]"; exit 1 ;;
	esac ;;
esac
exit 2
STUB
cat >"${bin}/govulncheck" <<'STUB'
#!/usr/bin/env bash
cat "${STUB_QUERY}"
STUB
chmod +x "${bin}/go" "${bin}/govulncheck"

# Throwaway module dir inside a git repo, with one test named for GO-2026-6909.
module="${tmp_root}/repo/mod"
mkdir -p "${module}/pkg"
git -C "${tmp_root}/repo" init -q
printf 'module example\n' >"${module}/go.mod"
: >"${module}/go.sum"
cat >"${module}/pkg/x_test.go" <<'EOF'
package pkg

import "testing"

func TestPlantedGO20266909(t *testing.T) {}
EOF

replaced_module='{"Path":"example.com/up","Version":"v1.0.0","Replace":{"Path":"example.com/fork","Version":"v1.0.0-fork.1"}}'
local_module='{"Path":"example.com/up","Version":"v1.0.0","Replace":{"Path":"../fork"}}'
osv_open='{"osv":{"id":"GO-2026-6909","affected":[{"package":{"name":"example.com/up"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"}]}]}]}}'
osv_fixed='{"osv":{"id":"GO-2026-6909","affected":[{"package":{"name":"example.com/up"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"1.1.0"}]}]}]}}'
osv_unknown='{"osv":{"id":"GO-2026-7777","affected":[{"package":{"name":"example.com/up"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"}]}]}]}}'
config='{"config":{"scan_mode":"query"}}'

# run_case <name> <want-exit> <modules-json> <query-json-or-empty> <replaced-test> <upstream-test> <want-message>
# want-message must appear in the output, so a red case cannot pass for the
# wrong reason.
run_case() {
	local name="$1" want="$2" modules="$3" osv="$4" replaced="$5" upstream="$6" msg="$7" got out
	printf '%s\n' "${modules}" >"${tmp_root}/modules.json"
	printf '%s\n%s\n' "${config}" "${osv}" >"${tmp_root}/query.json"
	out="${tmp_root}/out.txt"
	set +e
	PATH="${bin}:${PATH}" STUB_MODULES="${tmp_root}/modules.json" STUB_QUERY="${tmp_root}/query.json" \
		STUB_REPLACED_TEST="${replaced}" STUB_UPSTREAM_TEST="${upstream}" \
		bash "${script}" "${module}" >"${out}" 2>&1
	got=$?
	set -e
	if [ "${got}" -ne "${want}" ]; then
		cat "${out}" >&2
		fail "${name}: exit ${got}, want ${want}"
		return
	fi
	if ! grep -q -F -e "${msg}" "${out}"; then
		cat "${out}" >&2
		fail "${name}: output missing ${msg}"
		return
	fi
	pass "${name} (exit ${got})"
}

run_case "known advisory with a holding differential is green" 0 "${replaced_module}" "${osv_open}" pass fail "tests pass with the replacement and fail on upstream"
run_case "replaced module with no advisory is green" 0 "${replaced_module}" "" pass pass "no advisory against the original module"
run_case "unknown advisory ID with no named test is red" 1 "${replaced_module}" "${osv_unknown}" pass fail "no Go test is named for it"
run_case "advisory with a fixed version is red" 1 "${replaced_module}" "${osv_fixed}" pass fail "has a fixed version"
run_case "tests that pass on upstream are red" 1 "${replaced_module}" "${osv_open}" pass pass "PASS on upstream"
run_case "build break instead of test failure is red" 1 "${replaced_module}" "${osv_open}" pass build "broke the build instead of failing the tests"
run_case "tests that fail with the replacement are red" 1 "${replaced_module}" "${osv_open}" fail fail "regression tests fail with the replacement in place"
run_case "local-path replace is red" 1 "${local_module}" "" pass pass "replaced by a local path"

if [ "${failures}" -gt 0 ]; then
	printf 'test-govulncheck-replace-recheck: %d failure(s)\n' "${failures}" >&2
	exit 1
fi
printf 'test-govulncheck-replace-recheck: all checks passed\n'
