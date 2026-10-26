#!/usr/bin/env bash
#
# test-go-toolchain.sh - behavioural mirror for scripts/lib/go-toolchain.sh
# (#9098).
#
# On a host whose `go` is newer than go/go.mod (go 1.27.x against a 1.26.10
# module), GOTOOLCHAIN=auto never switches down, so the floor lints and scans
# with the newer compiler. golangci-lint and gosec, built with the module's Go,
# then fail on every package with export-data errors. The floor therefore pins
# GOTOOLCHAIN to the version go/go.mod declares. These cases run the REAL
# library against fixture go.mod files and assert on the environment it leaves,
# including the failing starting point (GOTOOLCHAIN=local on a newer host).
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
lib="$repo_root/scripts/lib/go-toolchain.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
passed=0
failed=0

ok() { printf 'ok - %s\n' "$1"; passed=$((passed + 1)); }
no() { printf 'FAIL - %s\n' "$1" >&2; failed=$((failed + 1)); }

# fixture NAME GOMOD_BODY builds a repo root with go/go.mod (none when the body
# is empty) and prints its path.
fixture() {
  local name="$1" body="$2"
  mkdir -p "$tmp/$name/go"
  [ -n "$body" ] && printf '%s' "$body" >"$tmp/$name/go/go.mod"
  printf '%s' "$tmp/$name"
}

run_lib() {
  local root="$1"; shift
  env -i PATH="$PATH" "$@" bash -c '
    . "$1"
    eshu_export_go_toolchain "$2"
    printf "GOTOOLCHAIN=%s GOROOT=%s\n" "${GOTOOLCHAIN-<unset>}" "${GOROOT-<unset>}"
  ' _ "$lib" "$root"
}

if [ ! -f "$lib" ]; then
  no "missing $lib"
  printf '\n%s passed, %s failed\n' "$passed" "$failed"
  exit 1
fi

# 1. the go directive is the pin.
root="$(fixture plain $'module example.com/m\n\ngo 1.26.10\n')"
got="$(run_lib "$root")"
[ "$got" = "GOTOOLCHAIN=go1.26.10 GOROOT=<unset>" ] \
  && ok "go directive 1.26.10 pins GOTOOLCHAIN=go1.26.10" \
  || no "go directive: got [$got]"

# 2. the failing starting point: a newer host forced local is pinned back.
got="$(run_lib "$root" GOTOOLCHAIN=local)"
[ "$got" = "GOTOOLCHAIN=go1.26.10 GOROOT=<unset>" ] \
  && ok "GOTOOLCHAIN=local is replaced by the go.mod pin" \
  || no "local override: got [$got]"

# 3. GOTOOLCHAIN=auto is replaced too (auto never switches down).
got="$(run_lib "$root" GOTOOLCHAIN=auto)"
[ "$got" = "GOTOOLCHAIN=go1.26.10 GOROOT=<unset>" ] \
  && ok "GOTOOLCHAIN=auto is replaced by the go.mod pin" \
  || no "auto override: got [$got]"

# 4. a toolchain line wins over the go directive, as it does for the go command.
root="$(fixture withtoolchain $'module example.com/m\n\ngo 1.26.10\n\ntoolchain go1.26.12\n')"
got="$(run_lib "$root")"
[ "$got" = "GOTOOLCHAIN=go1.26.12 GOROOT=<unset>" ] \
  && ok "a toolchain line wins over the go directive" \
  || no "toolchain line: got [$got]"

# 5. a stale GOROOT is cleared; it is what a switched toolchain leaks to children.
got="$(run_lib "$root" GOROOT=/nonexistent/goroot)"
[ "$got" = "GOTOOLCHAIN=go1.26.12 GOROOT=<unset>" ] \
  && ok "an inherited GOROOT is cleared" \
  || no "GOROOT: got [$got]"

# 6. no go.mod: leave the environment alone rather than guess.
root="$(fixture nomod '')"
got="$(run_lib "$root" GOTOOLCHAIN=local GOROOT=/keep)"
[ "$got" = "GOTOOLCHAIN=local GOROOT=/keep" ] \
  && ok "no go/go.mod leaves GOTOOLCHAIN and GOROOT untouched" \
  || no "no go.mod: got [$got]"

# 6b. a two-part go directive (go 1.22) is a language version, not a toolchain
# name; pinning it makes the go command fail with "invalid toolchain", so leave
# the environment alone.
root="$(fixture langonly $'module example.com/m\n\ngo 1.22\n')"
got="$(run_lib "$root" GOTOOLCHAIN=local GOROOT=/keep)"
[ "$got" = "GOTOOLCHAIN=local GOROOT=/keep" ] \
  && ok "a two-part go directive is not pinned" \
  || no "language-only go directive: got [$got]"

# 7. the real go.mod yields a pin that equals its go directive.
want="go$(awk '$1=="go"{print $2; exit}' "$repo_root/go/go.mod")"
got="$(run_lib "$repo_root")"
[ "$got" = "GOTOOLCHAIN=$want GOROOT=<unset>" ] \
  && ok "the repo go/go.mod pins $want" \
  || no "repo go.mod: want $want got [$got]"

printf '\n%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
