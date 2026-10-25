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
# shellcheck source=lib/cgo-cflags-guard-evasion-cases.sh
source "${script_dir}/lib/cgo-cflags-guard-evasion-cases.sh"
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
# The lib seeds from the process env, then from `go env CGO_CFLAGS` (which
# honours a developer's `go env -w`), then from Go's own "-O2 -g" default. A
# fake `go` on PATH stands in for the host's Go env so the cases do not depend
# on the host.
fake_go_dir="$(mktemp -d)"
printf '%s\n' "$fake_go_dir" >>"$tmp_list"
cat >"${fake_go_dir}/go" <<'EOF_GO'
#!/bin/sh
if [ "$1" = env ] && [ "$2" = CGO_CFLAGS ]; then
	printf '%s\n' "${FAKE_GO_ENV_CGO_CFLAGS-}"
fi
EOF_GO
chmod +x "${fake_go_dir}/go"

lib_case() {
	# $1 = process env value, or UNSET; $2 = what `go env CGO_CFLAGS` prints, or NOGO
	(
		if [ "$1" = UNSET ]; then unset CGO_CFLAGS; else export CGO_CFLAGS="$1"; fi
		if [ "$2" = NOGO ]; then
			PATH=/nonexistent
		else
			export FAKE_GO_ENV_CGO_CFLAGS="$2"
			PATH="${fake_go_dir}:${PATH}"
		fi
		eshu_cgo_cflags
	)
}

expect_eq "lib: Go's own -O2 -g default survives" "-O2 -g -std=gnu17" "$(lib_case UNSET '-O2 -g')"
expect_eq "lib: no go on PATH falls back to -O2 -g" "-O2 -g -std=gnu17" "$(lib_case UNSET NOGO)"
expect_eq "lib: empty go env falls back to -O2 -g" "-O2 -g -std=gnu17" "$(lib_case UNSET '')"
expect_eq "lib: a developer's go env -w value is kept and appended to" "-O3 -march=native -std=gnu17" "$(lib_case UNSET '-O3 -march=native')"
expect_eq "lib: a go env -w value with its own -std is untouched" "-O2 -std=c2x" "$(lib_case UNSET '-O2 -std=c2x')"
expect_eq "lib: process env wins over go env" "-O1 -Wall -std=gnu17" "$(lib_case '-O1 -Wall' '-O3')"
expect_eq "lib: empty process env falls through to go env" "-O3 -std=gnu17" "$(lib_case '' '-O3')"
expect_eq "lib: idempotent" "-O2 -g -std=gnu17" "$(lib_case '-O2 -g -std=gnu17' '-O2 -g')"
expect_eq "lib: caller's own -std wins" "-std=c2x -O2" "$(lib_case '-std=c2x -O2' '-O2 -g')"
expect_eq "lib: export sets the variable" "-O2 -g -std=gnu17" "$(unset CGO_CFLAGS; PATH="${fake_go_dir}:${PATH}" FAKE_GO_ENV_CGO_CFLAGS='-O2 -g' bash -c 'source "$1"; eshu_export_cgo_cflags; printf "%s" "${CGO_CFLAGS}"' _ "${repo_root}/scripts/lib/cgo-cflags.sh")"

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
		*) printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\neshu_export_cgo_cflags\n' >"${dir}/${f}" ;;
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
	if [ -n "${GUARD_LIST_FILE:-}" ]; then
		"${guard}" --root "$1" --entrypoints-file "${GUARD_LIST_FILE}" >/dev/null 2>"$2" || rc=$?
	else
		"${guard}" --root "$1" >/dev/null 2>"$2" || rc=$?
	fi
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
	# Drop the workflow-level env stanza. awk, not sed: a one-line sed range
	# with braces is rejected by BSD sed on macOS.
	good_workflow | awk '/^env:/{drop=1; next} /^jobs:/{drop=0} !drop' >"${t}/.github/workflows/bad.yml"
	check_guard "guard: workflow compiling Go without the flag FAILS" 1 "$t" "bad.yml"

	t="$(new_tree)"
	good_workflow | sed 's/-std=gnu17/-O3/' >"${t}/.github/workflows/clobber.yml"
	check_guard "guard: CGO_CFLAGS without -std=gnu17 FAILS" 1 "$t" "clobber.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: go test ./...\n        env:\n          CGO_CFLAGS: -O2\n'; } >"${t}/.github/workflows/step.yml"
	check_guard "guard: step-level CGO_CFLAGS clobber FAILS" 1 "$t" "step.yml"

	run_evasion_cases

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
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\neshu_export_cgo_cflags\nCGO_ENABLED=1 go -C go build ./cmd/api\n' >"${t}/scripts/build-y.sh"
	check_guard "guard: CGO_ENABLED=1 go script sourcing the lib passes" 0 "$t"

	t="$(new_tree)"
	rm "${t}/Makefile"
	check_guard "guard: missing entrypoint FAILS" 1 "$t" "Makefile"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\necho no lib\n' >"${t}/scripts/dev/pre-push.sh"
	check_guard "guard: entrypoint that does not source the lib FAILS" 1 "$t" "pre-push.sh"

	# F1 of the #9066 review: a comment that names the library must not satisfy
	# the guard, and sourcing the library without exporting does nothing.
	t="$(new_tree)"
	printf '#!/usr/bin/env bash\n# #9066: see scripts/lib/cgo-cflags.sh\n# shellcheck source=../lib/cgo-cflags.sh\n# eshu_export_cgo_cflags\n' >"${t}/scripts/dev/pre-push.sh"
	check_guard "guard: entrypoint that only mentions the lib in comments FAILS" 1 "$t" "pre-push.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\n' >"${t}/scripts/dev/pre-pr.sh"
	check_guard "guard: entrypoint that sources but never exports FAILS" 1 "$t" "pre-pr.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\neshu_export_cgo_cflags\n' >"${t}/scripts/dev/prove.sh"
	check_guard "guard: entrypoint that exports without sourcing FAILS" 1 "$t" "prove.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\n# eshu_export_cgo_cflags\n' >"${t}/scripts/dev/run-selected-gates.sh"
	check_guard "guard: export only in a comment FAILS" 1 "$t" "run-selected-gates.sh"

	# P3-f of the round 2 review: trailing comments and string text are not code.
	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\ntrue # eshu_export_cgo_cflags\n' >"${t}/scripts/dev/pre-push.sh"
	check_guard "guard: export only in a trailing comment FAILS" 1 "$t" "pre-push.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh" # trailing note\neshu_export_cgo_cflags # trailing note\n' >"${t}/scripts/dev/pre-push.sh"
	check_guard "guard: real source and export with trailing comments pass" 0 "$t"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\necho "source lib/cgo-cflags.sh"\neshu_export_cgo_cflags\n' >"${t}/scripts/dev/pre-pr.sh"
	check_guard "guard: source text inside an echo string FAILS" 1 "$t" "pre-pr.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\necho "eshu_export_cgo_cflags"\n' >"${t}/scripts/dev/prove.sh"
	check_guard "guard: export text inside an echo string FAILS" 1 "$t" "prove.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\n. "$(dirname "$0")/lib/cgo-cflags.sh"\nif true; then eshu_export_cgo_cflags; fi\n' >"${t}/scripts/dev/run-selected-gates.sh"
	check_guard "guard: dot source and an export after then pass" 0 "$t"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\neshu_export_cgo_cflags() { :; }\n' >"${t}/scripts/dev/pre-pr.sh"
	check_guard "guard: redefining the export function without calling it FAILS" 1 "$t" "pre-pr.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\neshu_export_cgo_cflags () { :; }\n' >"${t}/scripts/dev/pre-pr.sh"
	check_guard "guard: a spaced definition name () without a call FAILS" 1 "$t" "pre-pr.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\nfunction eshu_export_cgo_cflags { :; }\n' >"${t}/scripts/dev/prove.sh"
	check_guard "guard: a function-keyword definition without a call FAILS" 1 "$t" "prove.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\neshu_export_cgo_cflags && echo "ready (ok)"\n' >"${t}/scripts/dev/run-selected-gates.sh"
	check_guard "guard: a real call followed by text with parentheses passes" 0 "$t"

	t="$(new_tree)"
	printf 'export CGO_CFLAGS := -O2 -g -std=gnu17\n' >"${t}/Makefile"
	check_guard "guard: Makefile with a real export passes" 0 "$t"
	t="$(new_tree)"
	printf '# export CGO_CFLAGS := -O2 -g -std=gnu17\n' >"${t}/Makefile"
	check_guard "guard: Makefile with the export only in a comment FAILS" 1 "$t" "Makefile"

	t="$(new_tree)"
	printf 'X := -std=gnu17\nexport CGO_CFLAGS := -O2 -g\n' >"${t}/Makefile"
	check_guard "guard: Makefile with -std=gnu17 on a different line than the export FAILS" 1 "$t" "Makefile"

	t="$(new_tree)"
	printf 'export CGO_CFLAGS := -O2 -g # -std=gnu17 kept for docs\n' >"${t}/Makefile"
	check_guard "guard: Makefile with -std=gnu17 only in a trailing comment FAILS" 1 "$t" "Makefile"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\n# see cgo-cflags.sh\nCGO_ENABLED=1 go -C go build ./cmd/api\n' >"${t}/scripts/build-z.sh"
	check_guard "guard: CGO_ENABLED=1 go script with a comment-only mention FAILS" 1 "$t" "build-z.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/cgo-cflags.sh"\nCGO_ENABLED=1 go -C go build ./cmd/api\n' >"${t}/scripts/build-w.sh"
	check_guard "guard: CGO_ENABLED=1 go script that sources but never exports FAILS" 1 "$t" "build-w.sh"

	t="$(new_tree)"
	printf '#!/usr/bin/env bash\nCGO_ENABLED=1 go -C go build ./cmd/api\n' >"${t}/scripts/lib/helper.sh"
	check_guard "guard: nested scripts/lib helper without the lib FAILS" 1 "$t" "helper.sh"

	# P3-g of the round 5 review: a data file that silently loses a line must fail.
	list_src="${repo_root}/scripts/lib/cgo-cflags-entrypoints.txt"
	list_tmp="$(mktemp)"
	printf '%s\n' "$list_tmp" >>"$tmp_list"
	t="$(new_tree)"
	GUARD_LIST_FILE="$list_src"
	check_guard "guard: the shipped entrypoint list passes" 0 "$t"
	for dropped in precommit-go.sh install-local-binaries.sh Makefile; do
		rg -v -e "(^|/)${dropped}\$" "$list_src" >"$list_tmp" || true
		GUARD_LIST_FILE="$list_tmp"
		check_guard "guard: entrypoint list without ${dropped} FAILS" 1 "$t" "${dropped}"
	done
	printf '# only a comment\n\n' >"$list_tmp"
	check_guard "guard: an empty entrypoint list FAILS" 1 "$t" "pre-push.sh"
	: >"$list_tmp"
	check_guard "guard: a zero-byte entrypoint list FAILS" 1 "$t" "Makefile"
	# No trailing newline: the last entry must still be read and checked.
	printf '%s' "$(cat "$list_src")" >"$list_tmp"
	check_guard "guard: a complete list without a trailing newline passes" 0 "$t"
	printf '#!/usr/bin/env bash\necho no lib\n' >"${t}/scripts/install-local-binaries.sh"
	check_guard "guard: the last list line without a newline is still checked" 1 "$t" "install-local-binaries.sh"
	GUARD_LIST_FILE=""

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
	flags="$(lib_case UNSET '-O2 -g')"
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
