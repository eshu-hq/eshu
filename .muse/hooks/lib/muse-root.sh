#!/usr/bin/env bash
# Sourced by .muse/hooks/* -- not run on its own.
#
# Resolves the Eshu checkout root from the calling wrapper's own location, so
# a hook fires correctly no matter which subdirectory the session cwd is in.
# (The hook command in .muse/hooks.json finds the wrapper via
# `git rev-parse --show-toplevel`; from there BASH_SOURCE is exact.)
muse_repo_root() {
	# BASH_SOURCE[1] is the wrapper that sourced this file; [0] is this file.
	local src="${BASH_SOURCE[1]:-${BASH_SOURCE[0]}}"
	local d
	d="$(cd "$(dirname "${src}")/../.." && pwd)" || return 1
	printf '%s\n' "${d}"
}
