#!/usr/bin/env bash
# pre-pr-go-paths.sh — changed-Go-file/package helpers shared by
# scripts/dev/pre-pr.sh and scripts/dev/pre-push.sh.
#
# Sourced, never executed. Depends on ${go_dir} (set by the caller) and on
# collect_changed_paths (scripts/lib/pre-pr-fixture-consumers.sh, which must be
# sourced first).
#
# Extracted out of pre-pr.sh so pre-push.sh — the new fast local floor run
# before every push — can select the same changed-package targets for
# gofumpt/lint/build/vet without a second copy of this git-plumbing-to-package-
# dir mapping drifting from the original the way collect_changed_paths itself
# used to (see pre-pr-fixture-consumers.sh's own header for that incident).

# changed_go_files: the Go files under go/ among the changed paths.
changed_go_files() {
	collect_changed_paths | sort -u | rg '^go/.*\.go$' || true
}

# changed_go_dirs: ./-relative package dirs (under go/) for the changed files.
# Directories that no longer exist on disk are dropped: a fully deleted package
# still appears in `git diff --name-only` (as removed files), so its dir would
# otherwise be handed to `go build`/`go test`/`go vet`, which error with
# "directory not found [setup failed]". CI's authoritative whole-module
# `go build ./...` skips absent dirs naturally; this focused selector must do
# the same.
changed_go_dirs() {
	local f d
	changed_go_files | while IFS= read -r f; do
		printf './%s\n' "$(dirname "${f#go/}")"
	done | sort -u | while IFS= read -r d; do
		[[ -d "${go_dir}/${d#./}" ]] && printf '%s\n' "${d}"
	done
}
