#!/usr/bin/env bash
#
# test-verify-cgo-cflags-guard.sh - RED/GREEN proof for #9066.
#
# Three parts:
#   1. scripts/lib/cgo-cflags.sh unit cases: append never clobbers, Go's
#      "-O2 -g" default survives, a caller's own -std= wins, and re-applying is
#      a no-op.
#   2. scripts/verify-cgo-cflags-guard.sh on seeded fixture trees: a workflow
#      that compiles Go without the flag, a clobbering CGO_CFLAGS, an
#      unprotected `CGO_ENABLED=1 go` script, and a missing entrypoint each
#      FAIL; the matching clean trees PASS; the real tree PASSES. This part
#      never skips.
#   3. A compiler reproduction of the perl grammar failure. A probe that
#      defines its own bsearch after <stdlib.h> must fail with the host's
#      default C dialect when the host libc reproduces the bug, and must pass
#      with the flag eshu_cgo_cflags chooses. The flag half always runs; the
#      default-dialect half skips cleanly on a host that does not reproduce
#      (any libc before 2.43 or any compiler before GCC 15). When the perl
#      module is in the module cache its real scanner.c gets the same check.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
guard="${script_dir}/verify-cgo-cflags-guard.sh"
failures=0
tmp_list="$(mktemp)"
trap 'while IFS= read -r d; do [ -n "$d" ] && rm -rf "$d"; done <"$tmp_list"; rm -f "$tmp_list"' EXIT

# A host go env file (go env -w CGO_CFLAGS=...) or exported variable would hide
# the default-dialect failure from this test; the cases below set it explicitly.
unset CGO_CFLAGS

pass() { printf 'ok   %s\n' "$1"; }
fail() {
	printf 'FAIL %s\n' "$1" >&2
	failures=$((failures + 1))
}

expect_eq() {
	# $1 = case, $2 = want, $3 = got
	if [ "$2" = "$3" ]; then pass "$1"; else fail "$1: want '$2' got '$3'"; fi
}

# shellcheck source=lib/cgo-cflags.sh
source "${repo_root}/scripts/lib/cgo-cflags.sh"

# ---- 1. lib unit cases ------------------------------------------------------
expect_eq "lib: unset keeps Go's -O2 -g default" "-O2 -g -std=gnu17" "$(CGO_CFLAGS='' eshu_cgo_cflags)"
expect_eq "lib: appends to the caller's value" "-O1 -Wall -std=gnu17" "$(CGO_CFLAGS='-O1 -Wall' eshu_cgo_cflags)"
expect_eq "lib: idempotent" "-O2 -g -std=gnu17" "$(CGO_CFLAGS='-O2 -g -std=gnu17' eshu_cgo_cflags)"
expect_eq "lib: caller's own -std wins" "-std=c2x -O2" "$(CGO_CFLAGS='-std=c2x -O2' eshu_cgo_cflags)"
expect_eq "lib: export sets the variable" "-O2 -g -std=gnu17" "$(unset CGO_CFLAGS; eshu_export_cgo_cflags; printf '%s' "${CGO_CFLAGS}")"

# ---- 2. guard on seeded fixture trees --------------------------------------
new_tree() {
	local dir
	dir="$(mktemp -d)"
	printf '%s\n' "$dir" >>"$tmp_list"
	mkdir -p "${dir}/.github/workflows" "${dir}/scripts/lib" "${dir}/scripts/dev"
	cp "${repo_root}/scripts/lib/cgo-cflags.sh" "${dir}/scripts/lib/"
	# Entrypoints the guard requires, in their compliant shape.
	local f
	for f in $(guard_entrypoints); do
		mkdir -p "$(dirname "${dir}/${f}")"
		case "$f" in
		Makefile) printf 'export CGO_CFLAGS := -O2 -g -std=gnu17\n' >"${dir}/${f}" ;;
		*) printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\n' >"${dir}/${f}" ;;
		esac
	done
	printf '%s' "$dir"
}

guard_entrypoints() {
	"${guard}" --list-entrypoints
}

good_workflow() {
	cat <<'EOF'
name: Fixture
on: push
env:
  CGO_CFLAGS: "-O2 -g -std=gnu17"
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v6
      - run: go build ./...
EOF
}

run_guard() {
	# $1 = tree; stderr to $2. Prints exit status.
	local rc=0
	"${guard}" --root "$1" >/dev/null 2>"$2" || rc=$?
	printf '%s' "$rc"
}

check_guard() {
	# $1 = case, $2 = want rc, $3 = tree, $4 = optional stderr needle
	local err rc
	err="$(mktemp)"
	printf '%s\n' "$err" >>"$tmp_list"
	rc="$(run_guard "$3" "$err")"
	if [ "$rc" != "$2" ]; then
		fail "$1: exit=$rc want=$2 ($(head -c 300 "$err"))"
		return
	fi
	if [ -n "${4:-}" ] && ! rg -q --fixed-strings -- "$4" "$err"; then
		fail "$1: stderr lacks '$4'"
		return
	fi
	pass "$1"
}

if [ ! -x "${guard}" ]; then
	fail "guard missing or not executable: ${guard}"
else
	t="$(new_tree)"
	good_workflow >"${t}/.github/workflows/ok.yml"
	check_guard "guard: compliant fixture passes" 0 "$t"

	t="$(new_tree)"
	good_workflow | sed '/^env:/,/^jobs:/{/^env:/d;/CGO_CFLAGS/d}' >"${t}/.github/workflows/bad.yml"
	check_guard "guard: workflow compiling Go without the flag FAILS" 1 "$t" "bad.yml"

	t="$(new_tree)"
	good_workflow | sed 's/-std=gnu17/-O3/' >"${t}/.github/workflows/clobber.yml"
	check_guard "guard: CGO_CFLAGS without -std=gnu17 FAILS" 1 "$t" "clobber.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: go test ./...\n        env:\n          CGO_CFLAGS: -O2\n'; } >"${t}/.github/workflows/step.yml"
	check_guard "guard: step-level CGO_CFLAGS clobber FAILS" 1 "$t" "step.yml"

	t="$(new_tree)"
	cat >"${t}/.github/workflows/container.yml" <<'EOF'
name: Fixture
on: push
jobs:
  gate:
    runs-on: ubuntu-latest
    container: ghcr.io/eshu-hq/ci-tools@sha256:0000
    steps:
      - run: echo hi
EOF
	check_guard "guard: ci-tools container job without the flag FAILS" 1 "$t" "container.yml"

	t="$(new_tree)"
	cat >"${t}/.github/workflows/docs.yml" <<'EOF'
name: Docs
on: push
jobs:
  docs:
    runs-on: ubuntu-latest
    steps:
      - run: mkdocs build
EOF
	check_guard "guard: workflow that never compiles Go needs no flag" 0 "$t"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nCGO_ENABLED=1 go -C go build ./cmd/api\n' >"${t}/scripts/build-x.sh"
	check_guard "guard: CGO_ENABLED=1 go script without the lib FAILS" 1 "$t" "build-x.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\nCGO_ENABLED=1 go -C go build ./cmd/api\n' >"${t}/scripts/build-y.sh"
	check_guard "guard: CGO_ENABLED=1 go script sourcing the lib passes" 0 "$t"

	t="$(new_tree)"
	rm "${t}/Makefile"
	check_guard "guard: missing entrypoint FAILS" 1 "$t" "Makefile"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\necho no lib\n' >"${t}/scripts/dev/pre-push.sh"
	check_guard "guard: entrypoint that does not source the lib FAILS" 1 "$t" "pre-push.sh"

	check_guard "guard: real tree passes" 0 "${repo_root}"
fi

# ---- 3. compiler reproduction ----------------------------------------------
cc_bin="${CC:-cc}"
if ! command -v "${cc_bin}" >/dev/null 2>&1; then
	printf 'SKIP compiler reproduction: no C compiler (%s)\n' "${cc_bin}"
else
	probe_dir="$(mktemp -d)"
	printf '%s\n' "$probe_dir" >>"$tmp_list"
	probe="${probe_dir}/probe.c"
	# The same shape as the perl grammar's bsearch.h: <stdlib.h> first, then a
	# local definition of bsearch.
	cat >"$probe" <<'EOF'
#include <stdlib.h>
void *bsearch(const void *key, const void *base0, size_t nmemb, size_t size,
              int (*compar)(const void *, const void *)) {
  (void)key; (void)base0; (void)nmemb; (void)size; (void)compar;
  return 0;
}
EOF
	flags="$(unset CGO_CFLAGS; eshu_cgo_cflags)"
	# shellcheck disable=SC2086 # the flag string is intentionally word-split
	if "${cc_bin}" -fsyntax-only ${flags} "$probe" 2>"${probe_dir}/flag.err"; then
		pass "probe compiles with the chosen flags (${flags})"
	else
		fail "probe does not compile with the chosen flags (${flags}): $(head -c 300 "${probe_dir}/flag.err")"
	fi

	if "${cc_bin}" -fsyntax-only "$probe" 2>"${probe_dir}/default.err"; then
		printf 'SKIP default-dialect failure: this host (%s) does not reproduce #9066\n' "$("${cc_bin}" --version | head -1)"
	elif rg -q --fixed-strings '_Generic' "${probe_dir}/default.err"; then
		pass "probe FAILS with the default dialect on this host (RED reproduced)"
	else
		fail "probe failed for an unexpected reason: $(head -c 300 "${probe_dir}/default.err")"
	fi

	# The real grammar, when the module cache already holds it. Never download.
	perl_dir=""
	if command -v go >/dev/null 2>&1; then
		perl_dir="$(cd "${repo_root}/go" && GOFLAGS=-mod=mod GOPROXY=off go list -m -f '{{.Dir}}' github.com/alexaandru/go-sitter-forest/perl 2>/dev/null || true)"
	fi
	if [ -n "$perl_dir" ] && [ -f "${perl_dir}/scanner.c" ]; then
		# shellcheck disable=SC2086
		if "${cc_bin}" -fsyntax-only ${flags} -I"${perl_dir}" "${perl_dir}/scanner.c" 2>"${probe_dir}/perl.err"; then
			pass "perl scanner.c compiles with the chosen flags"
		else
			fail "perl scanner.c does not compile with the chosen flags: $(head -c 300 "${probe_dir}/perl.err")"
		fi
	else
		printf 'SKIP perl scanner.c check: go-sitter-forest/perl is not in the module cache\n'
	fi
fi

if [ "$failures" -ne 0 ]; then
	printf '%s case(s) failed\n' "$failures" >&2
	exit 1
fi
printf 'all cases passed\n'
