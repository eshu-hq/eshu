#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Install pinned Go CI tools, retrying transient proxy failures.
#
# Why this exists. A `bench script contract` run died installing benchstat:
#
#   go install golang.org/x/perf/cmd/benchstat@latest
#   ... proxy.golang.org ... 502 Bad Gateway
#
# and passed on a plain rerun. Every CI tool install (benchstat, golangci-lint,
# govulncheck, gosec, nancy) was a bare `go install pkg@version` with no retry,
# so the same proxy flake that go-mod-download-retry.sh already absorbs for
# module pre-warm could still fail a job before it ran anything -- labelled as
# the bench, lint, or security job rather than as the network fault it was.
#
# Go's own `,direct` GOPROXY fallback does not cover this: it applies to 404
# and 410 from the proxy, not to a 5xx or a connection that fails partway
# through a transfer.
#
# The retry shape and knobs are shared with go-mod-download-retry.sh on
# purpose (ESHU_GO_DOWNLOAD_ATTEMPTS, ESHU_GO_DOWNLOAD_RETRY_DELAY, doubling
# backoff, exit 2 on bad input), so one setting tunes every Go network step in
# CI. A tool that fails every attempt is not a transient fault and still fails,
# as it should.
#
# Usage: scripts/ci/go-install-retry.sh <pkg@version> [pkg@version ...]
#   Each argument is installed with its own `go install` call and its own
#   retry budget, in argument order. `go install` of several packages in one
#   call requires them to share a module version, which unrelated tools do not.
#
#   Every argument MUST carry a version suffix. `go install pkg@version` runs
#   in module-aware mode and ignores any go.mod in or above the current
#   directory, so the install is pinned to exactly that version wherever it
#   runs. An unversioned `go install pkg` inside a module resolves through that
#   module's go.mod instead (or fails outside one), silently changing which
#   tool version CI uses -- so it is refused rather than passed through.
#
#   Runs from the caller's current directory. The workflows call it from the
#   repo root, which has no go.mod, matching the bare `go install` it replaced.
set -uo pipefail

attempts="${ESHU_GO_DOWNLOAD_ATTEMPTS:-3}"
delay="${ESHU_GO_DOWNLOAD_RETRY_DELAY:-5}"

# Validate up front. Without this a 0 or non-numeric attempts count makes `seq`
# emit nothing, the loop body never runs, and -- since this script does not use
# `set -e` -- it exits 0 having installed nothing; the job then fails later on
# a missing binary, far from the cause. Same guard as go-mod-download-retry.sh.
if ! [[ "${attempts}" =~ ^[1-9][0-9]*$ ]]; then
	# Plain quoting, not ${var@Q}: that parameter transformation needs bash
	# 4.4+ and macOS ships 3.2.
	printf 'go-install-retry: ESHU_GO_DOWNLOAD_ATTEMPTS must be a positive integer, got "%s"\n' "${attempts}" >&2
	exit 2
fi
if ! [[ "${delay}" =~ ^[0-9]+$ ]]; then
	printf 'go-install-retry: ESHU_GO_DOWNLOAD_RETRY_DELAY must be a non-negative integer, got "%s"\n' "${delay}" >&2
	exit 2
fi

if [[ "$#" -eq 0 ]]; then
	echo "usage: go-install-retry.sh <pkg@version> [pkg@version ...]" >&2
	exit 2
fi

# Validate every argument before installing any, so a typo in the last one
# does not leave the job with a half-installed tool set.
for pkg in "$@"; do
	if ! [[ "${pkg}" =~ ^[^@[:space:]]+@[^@[:space:]]+$ ]]; then
		printf 'go-install-retry: "%s" is not a versioned package (want pkg@version, e.g. golang.org/x/perf/cmd/benchstat@latest)\n' "${pkg}" >&2
		exit 2
	fi
done

for pkg in "$@"; do
	pkg_delay="${delay}"
	for attempt in $(seq 1 "${attempts}"); do
		if go install "${pkg}"; then
			[[ "${attempt}" -gt 1 ]] && echo "go-install-retry: succeeded on attempt ${attempt}/${attempts} (${pkg})"
			break
		fi
		if [[ "${attempt}" -eq "${attempts}" ]]; then
			echo "go-install-retry: still failing after ${attempts} attempt(s) for \"${pkg}\"." >&2
			echo "  An install that fails every time is not the transient proxy" >&2
			echo "  fault this wrapper exists for -- check the package and version resolve." >&2
			exit 1
		fi
		echo "go-install-retry: attempt ${attempt}/${attempts} failed for \"${pkg}\"; retrying in ${pkg_delay}s" >&2
		sleep "${pkg_delay}"
		pkg_delay=$((pkg_delay * 2))
	done
done
