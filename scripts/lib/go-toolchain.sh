#!/usr/bin/env bash
#
# go-toolchain.sh - the one place local scripts learn which Go toolchain the
# module is built with (#9098).
#
# Why: go/go.mod declares the Go version the repo builds, lints and scans with.
# On a host whose own `go` is newer (go 1.27.x against a 1.26.10 module),
# GOTOOLCHAIN=auto never switches down, so the floor compiles with the newer Go.
# golangci-lint and gosec, installed with the module's Go, then fail on every
# package with export-data errors. Pinning GOTOOLCHAIN to the go.mod version
# makes the `go` command run that exact toolchain (and fetch it once if the host
# lacks it), the same one CI's setup-go installs.
#
# GOROOT is cleared as well. A switched toolchain exports its GOROOT to every
# child, and a shell can carry a GOROOT for an install that no longer exists
# ("go: cannot find GOROOT directory"). With GOROOT unset, `go` derives it from
# its own binary, which is always consistent. See "Go gates fail with a
# toolchain version mismatch" in docs/internal/agent-verification-details.md.
#
# A repo without go/go.mod is left alone: guessing a version would be worse than
# keeping the caller's environment.
#
# Usage (bash 3.2 compatible):
#   source "<repo>/scripts/lib/go-toolchain.sh"
#   eshu_export_go_toolchain "<repo root>"
#   eshu_go_toolchain "<repo root>"   # prints the pin without exporting it

# eshu_go_toolchain prints the toolchain name go/go.mod selects, e.g. go1.26.10,
# or nothing when go/go.mod is missing or declares neither a toolchain line nor
# a go directive. A `toolchain` line wins over the `go` directive, as it does
# for the go command.
eshu_go_toolchain() {
	local mod="${1}/go/go.mod" version
	[ -f "${mod}" ] || return 0
	version="$(awk '$1 == "toolchain" && $2 ~ /^go[0-9]/ { print $2; exit }' "${mod}")"
	if [ -z "${version}" ]; then
		version="$(awk '$1 == "go" && $2 ~ /^[0-9]/ { print "go" $2; exit }' "${mod}")"
	fi
	[ -n "${version}" ] && printf '%s' "${version}"
	return 0
}

# eshu_export_go_toolchain exports GOTOOLCHAIN with eshu_go_toolchain's value
# and unsets GOROOT. It changes nothing when no version can be read.
eshu_export_go_toolchain() {
	local version
	version="$(eshu_go_toolchain "${1}")"
	[ -n "${version}" ] || return 0
	GOTOOLCHAIN="${version}"
	export GOTOOLCHAIN
	unset GOROOT
	return 0
}
