#!/usr/bin/env bash
#
# Executable regression guard for the GOROOT isolation on every `go install` in
# scripts/dev/precommit-go.sh (fallout of the go1.26.6 bump, #6112).
#
# THE BUG THIS EXISTS TO CATCH
#
# go/go.mod pins `go 1.26.6`. On a developer host whose own `go` is older (1.26.5
# at the time of writing), GOTOOLCHAIN=auto re-execs the downloaded 1.26.6
# toolchain, and that switched process exports GOROOT to every child it spawns —
# including go/cmd/ci-gates, which shells each gate command out through /bin/sh
# with the environment it inherited.
#
# precommit-go.sh's tool installs run from the CALLER's working directory, which
# for every gate path is the repo root. No go.mod is visible there, so the host
# `go` never sees the 1.26.6 directive and never switches: it compiles with the
# 1.26.5 driver against the inherited 1.26.6 GOROOT, and every package fails with
#
#     compile: version "go1.26.6" does not match go tool version "go1.26.5"
#
# The gate then died on `failed to install govulncheck` — blocking `make pre-pr`
# on every branch of every such host, on the INSTALL step, before the scanner
# ever ran. CI never saw it because setup-go installs a matching toolchain.
#
# WHAT THIS GUARD ASSERTS, AND WHY NOT JUST THE EXIT CODE
#
# A bare `rc != 0` assertion would be satisfied just as well by govulncheck
# reporting a genuine advisory, so the first real CVE would keep this guard green
# while the install path was broken all over again. This guard asserts the
# observable that actually moves — the environment each `go install` is handed —
# by running the REAL precommit-go.sh with a fake `go` on PATH that records, per
# invocation, whether GOROOT was set, and reproduces the true compile-mismatch
# failure when it was. Every case checks four things:
#
#   1. the install was handed GOROOT=unset (exactly once — a count, not a match),
#   2. the script exited 0,
#   3. the compile-mismatch text never appeared, and
#   4. the installed tool actually ran, so a silently skipped step cannot pass
#      itself off as a clean one.
#
# Case 0 is a self-check on the harness: it drives the fake `go` with GOROOT
# present and REQUIRES the mismatch failure. Without it, a shim that quietly
# became a no-op would make every other case pass for no reason.
#
# The whole run is hermetic: `go` is faked, so no toolchain, network, module
# download, or real scanner is involved, and every path (tool cache, config,
# SARIF) lands inside a throwaway git repo under $TMPDIR rather than in the
# developer's shared precommit tool cache.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/precommit-go.sh"
[ -f "${script}" ] || { printf 'test-precommit-go-toolchain-isolation: missing %s\n' "${script}" >&2; exit 1; }

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/fakebin"
stub_src="${tmp_root}/stubs"
go_log="${tmp_root}/go-invocations.log"
tool_log="${tmp_root}/tool-invocations.log"
# A GOROOT that looks exactly like what a switched toolchain leaks. It never has
# to exist: the shim only cares whether the variable is present.
leaked_goroot="${tmp_root}/toolchain@v0.0.1-go1.26.6.darwin-arm64"

mismatch_text='does not match go tool version'
failures=0

fail() {
	printf 'test-precommit-go-toolchain-isolation: FAIL: %s\n' "$*" >&2
	failures=$((failures + 1))
}

pass() { printf 'test-precommit-go-toolchain-isolation: ok: %s\n' "$*"; }

# ---------------------------------------------------------------------------
# Harness: a fake `go`, and stub tool binaries for it to "install".
# ---------------------------------------------------------------------------

# write_fake_go writes a `go` shim that records the GOROOT state of every
# invocation and models the real toolchain behaviour:
#   - `install`: runs at the caller's cwd (repo root, no go.mod), so it does NOT
#     switch toolchains — with GOROOT set it reproduces the compile mismatch and
#     exits 1; with GOROOT clear it materialises the requested stub binary.
#   - anything else (`list` from nancy-local.sh, `run` for the surface gate):
#     runs inside go/, where the real toolchain switches cleanly, so GOROOT is
#     recorded but never fatal.
# Explanatory comments stay OUT of the heredoc body: the heredoc-budget gate
# caps a body at 512 bytes because Homebrew bash >= 5.1 writes the whole body
# into a 512-byte pipe before forking the reader, so a larger one deadlocks on
# macOS (#5074). Keeping the shim terse is what keeps this harness runnable.
#   `install` models the broken path: it runs at the caller's cwd (repo root,
#   no go.mod), so it never switches toolchains — GOROOT set means the compile
#   mismatch and exit 1; GOROOT clear means it copies in the stub binary named
#   by the module@version argument.
#   `list` models nancy-local.sh's dependency listing, which runs inside go/
#   where the real toolchain switches cleanly, so GOROOT is recorded, not fatal.
write_fake_go() {
	mkdir -p "${fake_bin}"
	# Written as TWO heredocs on purpose — do not merge them back into one. Each
	# body must stay under the 512-byte pipe-buffer budget; a single combined
	# body is ~600 bytes and would deadlock this harness under Homebrew bash.
	cat >"${fake_bin}/go" <<'SHIM_DISPATCH'
#!/usr/bin/env bash
set -uo pipefail
if [ -n "${GOROOT+x}" ]; then st=set; else st=unset; fi
printf '%s GOROOT=%s\n' "${1:-}" "${st}" >>"${FAKE_GO_LOG}"
[ "${1:-}" = list ] && { printf '{"ImportPath":"mini/stub"}\n'; exit 0; }
[ "${1:-}" = install ] || exit 0
SHIM_DISPATCH
	cat >>"${fake_bin}/go" <<'SHIM_INSTALL'
if [ "${st}" = set ]; then
	printf '# internal/asan\n' >&2
	printf 'compile: version "go1.26.6" does not match go tool version "go1.26.5"\n' >&2
	exit 1
fi
spec=""
for a in "$@"; do case "${a}" in */*@*) spec="${a}" ;; esac; done
n="${spec%@*}"
n="${n##*/}"
cp "${FAKE_GO_STUBS}/${n}" "${GOBIN}/${n}" || exit 99
chmod +x "${GOBIN}/${n}"
SHIM_INSTALL
	chmod +x "${fake_bin}/go"
}

# write_stub writes a stub tool binary that logs its own invocation, so a case
# can prove the scanner actually ran rather than being skipped.
write_stub() {
	local name="$1" body="$2"
	mkdir -p "${stub_src}"
	{
		printf '#!/usr/bin/env bash\n'
		printf 'set -uo pipefail\n'
		printf 'printf "%%s\\n" "%s" >>"${TOOL_LOG}"\n' "${name}"
		printf '%s\n' "${body}"
	} >"${stub_src}/${name}"
	chmod +x "${stub_src}/${name}"
}

write_stubs() {
	write_stub govulncheck 'printf "No vulnerabilities found.\n"; exit 0'
	write_stub nancy 'printf "audited 1 dependency, no issues\n"; exit 0'
	write_stub golangci-lint 'exit 0'
	# gosec must leave a well-formed empty SARIF where -out points, because the
	# real gate jq-counts that file.
	write_stub gosec '
out=""
prev=""
for arg in "$@"; do
	case "${prev}" in -out) out="${arg}" ;; esac
	case "${arg}" in -out=*) out="${arg#-out=}" ;; esac
	prev="${arg}"
done
[ -n "${out}" ] && printf "{\"runs\":[{\"results\":[]}]}\n" >"${out}"
exit 0'
}

# make_mini_repo builds a throwaway git repo with just the files
# precommit-go.sh reads, so the script derives every cache path inside it.
make_mini_repo() {
	local mini="${tmp_root}/mini"
	mkdir -p "${mini}/go" "${mini}/scripts/dev" "${mini}/.github/workflows"
	git -C "${mini}" init -q 2>/dev/null || git init -q "${mini}"
	printf 'module mini\n\ngo 1.26.6\n' >"${mini}/go/go.mod"
	printf 'linters:\n  enable:\n    - filelength\n' >"${mini}/go/.golangci.yml"
	printf 'run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2\n' \
		>"${mini}/.github/workflows/test.yml"
	printf 'run: go install github.com/securego/gosec/v2/cmd/gosec@v2.27.1\n' \
		>"${mini}/.github/workflows/security-scan.yml"
	cp "${repo_root}/scripts/dev/nancy-local.sh" "${mini}/scripts/dev/nancy-local.sh"
	printf '%s' "${mini}"
}

# run_case drives one precommit-go.sh subcommand under a deliberately leaked
# GOROOT and asserts the four properties above. $1 is the subcommand, $2 the
# tool that must have run.
run_case() {
	local subcmd="$1" tool="$2" out rc before
	out="${tmp_root}/${subcmd}.out"
	before="${failures}"
	: >"${go_log}"
	: >"${tool_log}"

	set +e
	( cd "${mini_repo}" && \
		PATH="${fake_bin}:${PATH}" \
		GOROOT="${leaked_goroot}" \
		FAKE_GO_LOG="${go_log}" \
		FAKE_GO_STUBS="${stub_src}" \
		TOOL_LOG="${tool_log}" \
		bash "${script}" "${subcmd}" ) >"${out}" 2>&1
	rc=$?
	set -e

	local installs_clean installs_leaked
	installs_clean="$(grep -c '^install GOROOT=unset$' "${go_log}" || true)"
	installs_leaked="$(grep -c '^install GOROOT=set$' "${go_log}" || true)"

	if [ "${installs_leaked}" -ne 0 ]; then
		fail "${subcmd}: ${installs_leaked} 'go install' invocation(s) inherited the leaked GOROOT — the env -u GOROOT in go_install_tool is missing or bypassed"
	fi
	if [ "${installs_clean}" -ne 1 ]; then
		fail "${subcmd}: expected exactly 1 'go install' with GOROOT cleared, recorded ${installs_clean} (log: $(tr '\n' ';' <"${go_log}"))"
	fi
	if [ "${rc}" -ne 0 ]; then
		fail "${subcmd}: exit ${rc}, want 0 (output: $(tail -n 3 "${out}" | tr '\n' ' '))"
	fi
	if grep -q "${mismatch_text}" "${out}"; then
		fail "${subcmd}: output carries the toolchain-mismatch failure"
	fi
	if ! grep -qx "${tool}" "${tool_log}"; then
		fail "${subcmd}: ${tool} never ran — the case cannot prove the install path works"
	fi
	# An `if` (not `&&`) so a failing case returns 0 and `set -e` does not abort
	# the run — every case must report, not just the first one that breaks.
	if [ "${failures}" -eq "${before}" ]; then
		pass "${subcmd}: install ran with GOROOT cleared, ${tool} executed, exit 0"
	fi
}

# ---------------------------------------------------------------------------
# Case 0 — self-check: the shim must fail loudly when GOROOT is present.
# ---------------------------------------------------------------------------
write_fake_go
write_stubs
mini_repo="$(make_mini_repo)"

: >"${go_log}"
set +e
selfcheck_out="$(GOROOT="${leaked_goroot}" FAKE_GO_LOG="${go_log}" FAKE_GO_STUBS="${stub_src}" \
	GOBIN="${tmp_root}/selfcheck-bin" \
	"${fake_bin}/go" install golang.org/x/vuln/cmd/govulncheck@latest 2>&1)"
selfcheck_rc=$?
set -e
if [ "${selfcheck_rc}" -eq 0 ]; then
	fail "self-check: the fake go succeeded with GOROOT set — the harness cannot detect the regression it exists to catch"
elif ! printf '%s' "${selfcheck_out}" | grep -q "${mismatch_text}"; then
	fail "self-check: the fake go failed without the compile-mismatch text, so a red result could not be attributed to this bug"
else
	pass "self-check: fake go reproduces the compile mismatch when GOROOT leaks"
fi

# ---------------------------------------------------------------------------
# Cases 1-4 — one per ensure_* install site in precommit-go.sh.
# ---------------------------------------------------------------------------
run_case govulncheck govulncheck
run_case nancy nancy
run_case lint-all golangci-lint
run_case gosec-all gosec

# ---------------------------------------------------------------------------
# Case 5 — structural backstop.
#
# The behavioural cases above only reach the install sites that today's
# subcommands call. This asserts the property directly on the source so a
# newly added tool cannot reintroduce a bare `go install`: there must be exactly
# ONE executable `go install` line in the whole script, it must clear GOROOT,
# and every install site must route through it.
# ---------------------------------------------------------------------------
raw_installs="$(grep -c '^[^#]*[^#]go install ' "${script}" || true)"
if [ "${raw_installs}" -ne 1 ]; then
	fail "expected exactly 1 executable 'go install' line in precommit-go.sh (the go_install_tool helper), found ${raw_installs} — route every install through go_install_tool"
elif ! grep '^[^#]*[^#]go install ' "${script}" | grep -q 'env -u GOROOT'; then
	fail "the 'go install' in precommit-go.sh no longer clears GOROOT (env -u GOROOT)"
else
	pass "precommit-go.sh has exactly one 'go install', and it clears GOROOT"
fi

# Match call sites only: the helper's own definition line is `go_install_tool()`,
# so requiring a non-"(" character after the name excludes it.
call_sites="$(grep -c '^[[:space:]]*go_install_tool[^(]' "${script}" || true)"
if [ "${call_sites}" -ne 4 ]; then
	fail "expected 4 go_install_tool call sites (golangci-lint, gosec, govulncheck, nancy), found ${call_sites} — if a tool was added or removed, update this count AND add a behavioural case above"
else
	pass "all 4 ensure_* install sites route through go_install_tool"
fi

if [ "${failures}" -ne 0 ]; then
	printf 'test-precommit-go-toolchain-isolation: %d failure(s)\n' "${failures}" >&2
	exit 1
fi
printf 'test-precommit-go-toolchain-isolation: all checks passed\n'
