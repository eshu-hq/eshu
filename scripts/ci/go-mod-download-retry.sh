#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Pre-warm the Go module cache, retrying transient proxy failures.
#
# Why this exists. `proxy.golang.org` intermittently drops a module download
# mid-stream:
#
#   github.com/tree-sitter/tree-sitter-haskell@v0.23.1: read
#   "https://proxy.golang.org/.../@v/v0.23.1.zip": stream error: stream ID 555;
#   INTERNAL_ERROR; received from peer
#
# Two jobs failed that way on one afternoon (go-race shard 2 on the module
# above, and the reducer latency gate on go-sitter-forest/sql@v1.9.9). Neither
# had run a test when it died -- both were still fetching dependencies -- so
# the red arrived labelled "race detector" and "latency budget", which is
# actively misleading about where to look. #6615 found the same shape hitting
# three more workflows on one commit (golang.org/x/net, github.com/
# hybridgroup/yzma, sigs.k8s.io/yaml), none of which called this script --
# each had to be found and fixed one at a time. Usage below adds a module-dir
# argument so every module this repo's CI actually builds (go/, sdk/go/
# collector, sdk/go/factschema, examples/collector-extensions/scorecard, ...)
# can share this one wrapper instead of a second one-off per module.
#
# Go's own `,direct` GOPROXY fallback does not cover this: it applies to 404
# and 410 from the proxy, not to a connection that fails partway through a
# transfer.
#
# Downloading up front, with retries, moves the flake to a step whose name says
# what it is and usually removes it entirely -- a later `go build` or `go test`
# reads an already-populated module cache instead of the network. It does not
# make the build hermetic; a module genuinely absent upstream still fails, and
# should.
#
# Usage: scripts/ci/go-mod-download-retry.sh [module-dir]
#   module-dir: path to the module to warm, relative to the repo root.
#     Defaults to "go" (the app module) to keep every pre-#6615 call site
#     working unchanged. Pass e.g. "sdk/go/collector" to warm a different
#     module -- each go.mod in this repo has its own dependency graph, so
#     warming go/ does not warm sdk/go/collector or sdk/go/factschema.
set -uo pipefail

module_dir="${1:-go}"
attempts="${ESHU_GO_DOWNLOAD_ATTEMPTS:-3}"
delay="${ESHU_GO_DOWNLOAD_RETRY_DELAY:-5}"

# Validate up front. Without this a 0 or non-numeric attempts count makes `seq`
# emit nothing, the loop body never runs, and -- since this script does not use
# `set -e` -- it exits 0 having downloaded nothing. A warmup that silently does
# not warm up is worse than no warmup: the proxy flake it exists to absorb
# comes back, and the step that was supposed to catch it reports success
# (#6083 review).
if ! [[ "${attempts}" =~ ^[1-9][0-9]*$ ]]; then
	# Plain quoting, not ${var@Q}: that parameter transformation needs bash
	# 4.4+ and macOS ships 3.2, where it is a bad substitution that makes this
	# guard fall through to the very seq failure it exists to prevent.
	printf 'go-mod-download-retry: ESHU_GO_DOWNLOAD_ATTEMPTS must be a positive integer, got "%s"\n' "${attempts}" >&2
	exit 2
fi
if ! [[ "${delay}" =~ ^[0-9]+$ ]]; then
	printf 'go-mod-download-retry: ESHU_GO_DOWNLOAD_RETRY_DELAY must be a non-negative integer, got "%s"\n' "${delay}" >&2
	exit 2
fi

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
target_dir="${repo_root}/${module_dir}"

# Validate the module dir up front, same reasoning as the attempts/delay
# guards above: a `cd` into a missing directory under `set -uo pipefail`
# (no `-e`) does not stop the script, so a typo'd module-dir would otherwise
# report success having warmed nothing.
if [[ ! -f "${target_dir}/go.mod" ]]; then
	printf 'go-mod-download-retry: no go.mod at "%s" (module-dir "%s") -- nothing to warm\n' "${target_dir}" "${module_dir}" >&2
	exit 2
fi

cd "${target_dir}" || exit 1

for attempt in $(seq 1 "${attempts}"); do
	# `go mod download` (NOT `... all`): plain download fetches what the main
	# module needs to build and test, and leaves go.sum alone. `all` walks the
	# whole module graph including transitive test dependencies and writes
	# thousands of new go.sum lines, which would leave every CI run with a
	# dirty tree.
	if go mod download; then
		[[ "${attempt}" -gt 1 ]] && echo "go-mod-download-retry: succeeded on attempt ${attempt}/${attempts} (${module_dir})"
		exit 0
	fi
	if [[ "${attempt}" -eq "${attempts}" ]]; then
		echo "go-mod-download-retry: still failing after ${attempts} attempt(s) for module \"${module_dir}\"." >&2
		echo "  A module download that fails every time is not the transient proxy" >&2
		echo "  fault this wrapper exists for -- check the module actually resolves." >&2
		exit 1
	fi
	echo "go-mod-download-retry: attempt ${attempt}/${attempts} failed for module \"${module_dir}\"; retrying in ${delay}s" >&2
	sleep "${delay}"
	delay=$((delay * 2))
done
