#!/usr/bin/env bash
#
# Regression guard for nested Go modules under go/ (eshu-hq/eshu-api#456):
# go/apilib carries its own go.mod, resolved independently of the go/ app
# module. The hook used to lint and scan nested files from go/, where the
# parent module cannot resolve them: golangci-lint typechecked them as a
# missing package yet exited 0 ("0 issues"), and gosec's `go list` resolved
# them to zero files ("nothing to scan"). Both read as clean without analyzing
# anything. scripts/dev/precommit-go.sh now partitions changed packages by
# enclosing module root and runs nested packages from inside their own root.
#
# CASES (G = must pass, R = must fail)
#   G1 lint arm on real go/apilib files: exit 0 and reports >= 1 package,
#      without the old "does not contain package" typecheck error.
#   R1 scratch repo: a planted unused variable inside a nested module fails
#      the lint arm and names the symbol (negative control — a vacuous scan
#      would exit 0, so expecting failure proves real analysis).
#   G2 lint arm on a real main-module file: exit 0 (routing regression check).
#   G3 gosec arm on a real go/apilib file: exit 0 and scans >= 1 package
#      (proves gosec resolves nested packages instead of "nothing to scan").
#   G4 scratch repo: gosec-changed on a nested-only change scans the nested
#      package in changed scope, not the whole tree.
#
# G1-G3 drive the REAL worktree; R1/G4 build a throwaway repo under $TMPDIR
# (precommit-go.sh derives every path from `git rev-parse --show-toplevel`,
# so the repo's own tool cache and worktree state are never touched). The
# pinned tool binaries are copied from the repo's shared tool cache when
# present, installed on demand otherwise — the same arrangement as
# scripts/test-precommit-go-gosec.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/precommit-go.sh"
[ -f "${script}" ] || { printf 'test-precommit-go-nested-module: missing %s\n' "${script}" >&2; exit 1; }

tmp_root="$(mktemp -d)"
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERFILE=0 GIT_TRACE2_PERF=0
trap 'rm -rf "${tmp_root}"' EXIT
fx="${tmp_root}/fx"
out_file="${tmp_root}/out.txt"
failures=0

fail() { printf 'test-precommit-go-nested-module: FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }
pass() { printf 'test-precommit-go-nested-module: ok: %s\n' "$*"; }

# tool_seed copies the pinned tool binary from the repo's shared cache into
# the fixture cache, installing it on demand like precommit-go.sh does.
tool_seed() {
	local name="$1" version="$2" pkg="$3" common cache bin
	common="$(git -C "${repo_root}" rev-parse --git-common-dir)"
	case "${common}" in /*) ;; *) common="${repo_root}/${common}" ;; esac
	cache="${common}/eshu-precommit"
	bin="${cache}/${name}-${version}"
	if [ ! -x "${bin}" ]; then
		mkdir -p "${cache}"
		GOBIN="${cache}" GOFLAGS=-mod=mod env -u GOROOT go install "${pkg}@${version}" >&2 \
			&& mv "${cache}/${name}" "${bin}"
	fi
	printf '%s' "${bin}"
}

golangci_version="$(rg -o 'golangci-lint@v[0-9.]+' "${repo_root}/.github/workflows/test.yml" 2>/dev/null | head -1 | sed 's/.*@//')"
gosec_version="$(rg -o 'gosec@v[0-9][0-9A-Za-z._+-]*' "${repo_root}/.github/workflows/security-scan.yml" 2>/dev/null | head -1 | sed 's/.*@//')"
golangci_version="${golangci_version:-v2.12.2}"
gosec_version="${gosec_version:-v2.30.1-0.20261026162949-ff25931a2edd}"

rc=0
run_repo() {
	set +e
	( cd "${repo_root}" && bash "${script}" "$@" ) >"${out_file}" 2>&1
	rc=$?
	set -e
}

# --- G1: lint arm on real go/apilib files routes into the nested module.
run_repo lint go/apilib/readyz.go go/apilib/readyz_test.go
if [ "${rc}" -ne 0 ]; then
	fail "G1: lint go/apilib files exited ${rc}, want 0 (output: $(tail -n 3 "${out_file}" | tr '\n' ' '))"
else
	n="$(sed -n 's/^lint: \([0-9][0-9]*\) package.*/\1/p' "${out_file}" | head -1)"
	if [ -z "${n}" ] || [ "${n}" -lt 1 ]; then
		fail "G1: lint reported '${n}' packages, want >= 1"
	elif rg --quiet --fixed-strings -- 'does not contain package' "${out_file}"; then
		fail "G1: lint still typechecks nested files from the parent module"
	else
		pass "G1: lint go/apilib files: exit 0, ${n} package(s), no missing-package error"
	fi
fi

# --- G2: a main-module file still lints as before (no routing regression).
run_repo lint go/internal/cigates/changeset.go
if [ "${rc}" -ne 0 ]; then
	fail "G2: lint main-module file exited ${rc}, want 0 (output: $(tail -n 3 "${out_file}" | tr '\n' ' '))"
elif ! rg --quiet --fixed-strings -- 'lint: 1 package(s)' "${out_file}"; then
	fail "G2: lint main-module file reports an unexpected package count (output: $(head -n 1 "${out_file}"))"
else
	pass "G2: lint main-module file: exit 0, 1 package"
fi

# --- G3: gosec arm on a real go/apilib file scans the nested package.
run_repo gosec go/apilib/readyz.go
if [ "${rc}" -ne 0 ]; then
	fail "G3: gosec go/apilib file exited ${rc}, want 0 (output: $(tail -n 3 "${out_file}" | tr '\n' ' '))"
else
	line="$(rg --no-filename -- 'file\(s\) scanned in .* package' "${out_file}" | head -1)"
	pkgs="$(printf '%s' "${line}" | sed -n 's/.*in \([0-9][0-9]*\) package.*/\1/p')"
	if [ -z "${pkgs}" ] || [ "${pkgs}" -lt 1 ]; then
		fail "G3: gosec scanned '${pkgs}' packages, want >= 1 (output: $(tail -n 2 "${out_file}" | tr '\n' ' '))"
	else
		pass "G3: gosec go/apilib file: exit 0, ${pkgs} package(s) scanned"
	fi
fi

# --- Scratch fixture: a main module plus one nested module.
mkdir -p "${fx}/go/internal/ok" "${fx}/go/nest" "${fx}/.github/workflows"
git init -q -b main "${fx}"
git -C "${fx}" config user.email t@example.invalid
git -C "${fx}" config user.name t
printf 'module fx\n\ngo 1.22\n' >"${fx}/go/go.mod"
: >"${fx}/go/go.sum"
cp "${repo_root}/go/.golangci.yml" "${fx}/go/.golangci.yml"
printf 'package ok\n\n// Add adds two ints.\nfunc Add(a, b int) int { return a + b }\n' >"${fx}/go/internal/ok/ok.go"
printf 'module example.com/fxnest\n\ngo 1.22\n' >"${fx}/go/nest/go.mod"
printf 'package nest\n\n// Double doubles n.\nfunc Double(n int) int { return 2 * n }\n' >"${fx}/go/nest/nest.go"
printf 'run: scripts/ci/go-install-retry.sh github.com/golangci/golangci-lint/v2/cmd/golangci-lint@%s\n' \
	"${golangci_version}" >"${fx}/.github/workflows/test.yml"
printf 'run: scripts/ci/go-install-retry.sh github.com/securego/gosec/v2/cmd/gosec@%s\n' \
	"${gosec_version}" >"${fx}/.github/workflows/security-scan.yml"
git -C "${fx}" add -A
git -C "${fx}" commit -q -m base
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
mkdir -p "${fx}/.git/eshu-precommit"
cp "$(tool_seed golangci-lint "${golangci_version}" github.com/golangci/golangci-lint/v2/cmd/golangci-lint)" \
	"${fx}/.git/eshu-precommit/golangci-lint-${golangci_version}"
cp "$(tool_seed gosec "${gosec_version}" github.com/securego/gosec/v2/cmd/gosec)" \
	"${fx}/.git/eshu-precommit/gosec-${gosec_version}"

# --- R1: a nested-module violation fails the lint arm (negative control).
printf 'package nest\n\n// nestedUnusedSeed must be reported by the unused linter.\nvar nestedUnusedSeed = 42\n' \
	>"${fx}/go/nest/bad.go"
set +e
( cd "${fx}" && bash "${script}" lint go/nest/bad.go ) >"${out_file}" 2>&1
rc=$?
set -e
if [ "${rc}" -eq 0 ]; then
	fail "R1: lint nested violation exited 0 — the nested package was not analyzed"
elif ! rg --quiet --fixed-strings -- 'nestedUnusedSeed' "${out_file}"; then
	fail "R1: lint nested violation names no symbol (output: $(tail -n 3 "${out_file}" | tr '\n' ' '))"
else
	pass "R1: lint nested violation: exit ${rc}, names nestedUnusedSeed"
fi

# --- G4: gosec-changed on a nested-only change scans the nested package.
git -C "${fx}" add -A
set +e
( cd "${fx}" && bash "${script}" gosec-changed ) >"${out_file}" 2>&1
rc=$?
set -e
if [ "${rc}" -ne 0 ]; then
	fail "G4: gosec-changed nested-only change exited ${rc} (output: $(tail -n 3 "${out_file}" | tr '\n' ' '))"
elif ! rg --quiet --fixed-strings -- 'gosec-changed-nest' "${out_file}"; then
	fail "G4: gosec-changed did not run the nested-module scan (output: $(tail -n 3 "${out_file}" | tr '\n' ' '))"
elif rg --quiet --fixed-strings -- 'whole-module scan' "${out_file}"; then
	fail "G4: gosec-changed widened a nested-only change to the whole tree"
else
	pass "G4: gosec-changed nested-only change scans the nested package in scope"
fi

if [ "${failures}" -ne 0 ]; then
	printf 'test-precommit-go-nested-module: %d failure(s)\n' "${failures}" >&2
	exit 1
fi
printf 'test-precommit-go-nested-module: all cases passed\n'
