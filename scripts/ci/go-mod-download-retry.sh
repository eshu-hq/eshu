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
# actively misleading about where to look.
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
set -uo pipefail

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

cd "$(dirname "$0")/../../go" || exit 1

for attempt in $(seq 1 "${attempts}"); do
	# `go mod download` (NOT `... all`): plain download fetches what the main
	# module needs to build and test, and leaves go.sum alone. `all` walks the
	# whole module graph including transitive test dependencies and writes
	# thousands of new go.sum lines, which would leave every CI run with a
	# dirty tree.
	if go mod download; then
		[[ "${attempt}" -gt 1 ]] && echo "go-mod-download-retry: succeeded on attempt ${attempt}/${attempts}"
		exit 0
	fi
	if [[ "${attempt}" -eq "${attempts}" ]]; then
		echo "go-mod-download-retry: still failing after ${attempts} attempt(s)." >&2
		echo "  A module download that fails every time is not the transient proxy" >&2
		echo "  fault this wrapper exists for -- check the module actually resolves." >&2
		exit 1
	fi
	echo "go-mod-download-retry: attempt ${attempt}/${attempts} failed; retrying in ${delay}s" >&2
	sleep "${delay}"
	delay=$((delay * 2))
done
