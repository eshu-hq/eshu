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
#
# Dirs that `./...` never matches are dropped too: any path segment named
# "testdata" or beginning with "." or "_" (`go help packages`). A named dir is
# built even when `./...` would skip it, so a fixture such as a `package main`
# file without main() under testdata/ failed `go build` here while CI, which
# builds `./...`, never compiles it (#7978).
changed_go_dirs() {
	local f d
	changed_go_files | while IFS= read -r f; do
		printf './%s\n' "$(dirname "${f#go/}")"
	done | sort -u | while IFS= read -r d; do
		go_dir_ignored_by_wildcard "${d}" && continue
		[[ -d "${go_dir}/${d#./}" ]] && printf '%s\n' "${d}"
	done
}

# go_dir_ignored_by_wildcard reports (exit 0) whether a ./-relative dir has a
# path segment the go tool's `./...` pattern skips: "testdata", or a name that
# begins with "." or "_". A bare "." segment (the module root, "./.") is the
# current directory, not a hidden name, so it is kept.
go_dir_ignored_by_wildcard() {
	local seg segs=()
	IFS=/ read -r -a segs <<<"${1#./}"
	for seg in "${segs[@]}"; do
		[[ "${seg}" == . ]] && continue
		[[ "${seg}" == testdata || "${seg}" == .* || "${seg}" == _* ]] && return 0
	done
	return 1
}

# pre_pr_nested_module_roots prints the go/-relative roots of nested Go modules
# under go/ (every directory holding a go.mod except go/ itself). Mirrors
# precommit-go.sh's nested_module_roots: modules at or under a testdata/
# component are intentionally-uncompilable fixtures and keep today's
# main-module behavior.
pre_pr_nested_module_roots() {
	local mod d
	while IFS= read -r mod; do
		d="${mod%/go.mod}"
		d="${d#./}"
		case "${d}" in
			""|".") continue ;;
			testdata|testdata/*|*/testdata|*/testdata/*) continue ;;
		esac
		printf '%s\n' "${d}"
	done < <(cd "${go_dir}" && find . -path '*/testdata/*' -prune -o -name go.mod -print | sort)
}

# pre_pr_ensure_nested_roots fills the global pre_pr_nested_roots array once
# per process, so the three partitioned steps share one find.
pre_pr_ensure_nested_roots() {
	if [[ -n "${pre_pr_nested_roots_init:-}" ]]; then
		return 0
	fi
	pre_pr_nested_roots=()
	local r
	while IFS= read -r r; do
		[[ -n "${r}" ]] && pre_pr_nested_roots+=("${r}")
	done < <(pre_pr_nested_module_roots)
	pre_pr_nested_roots_init=1
}

# pre_pr_partition_go_dirs splits ./-relative package dirs (read on stdin)
# into the globals main_go_dirs (served from go/ as before) and nested_go_runs
# ("<repo-rel-root>|<subpkg>" per nested-module package, mirroring
# precommit-go.sh's partition_dirs: subpkg "." when the changed package IS the
# module root, "./sub" below; longest-prefix match wins; entries dedupe).
# nested_go_roots holds the distinct nested roots in first-seen order, so
# drivers run one go invocation per module root. ./. , ./internal/... and
# fixture-consumer dirs never match a nested root and stay in the main group.
# Call with a herestring, never a pipe: a pipe runs this function in a
# subshell and the globals are lost.
pre_pr_partition_go_dirs() {
	pre_pr_ensure_nested_roots
	main_go_dirs=()
	nested_go_runs=()
	nested_go_roots=()
	local d gorel r root sub entry e seen
	while IFS= read -r d; do
		[[ -n "${d}" ]] || continue
		gorel="${d#./}"
		root=""
		for r in ${pre_pr_nested_roots[@]+"${pre_pr_nested_roots[@]}"}; do
			if [[ "${gorel}" == "${r}" || "${gorel}" == "${r}/"* ]]; then
				if [[ "${#r}" -gt "${#root}" ]]; then
					root="${r}"
				fi
			fi
		done
		if [[ -z "${root}" ]]; then
			main_go_dirs+=("${d}")
			continue
		fi
		sub="${gorel#"${root}"}"
		sub="${sub#/}"
		if [[ -z "${sub}" ]]; then
			sub="."
		else
			sub="./${sub}"
		fi
		entry="go/${root}|${sub}"
		seen=0
		for e in ${nested_go_runs[@]+"${nested_go_runs[@]}"}; do
			if [[ "${e}" == "${entry}" ]]; then
				seen=1
				break
			fi
		done
		if [[ "${seen}" -eq 0 ]]; then
			nested_go_runs+=("${entry}")
			seen=0
			for e in ${nested_go_roots[@]+"${nested_go_roots[@]}"}; do
				if [[ "${e}" == "go/${root}" ]]; then
					seen=1
					break
				fi
			done
			if [[ "${seen}" -eq 0 ]]; then
				nested_go_roots+=("go/${root}")
			fi
		fi
	done
}

# pre_pr_nested_group_pkgs prints the subpkg entries of nested_go_runs that
# belong to the module root passed as $1 (one per line), so drivers run one go
# invocation per module root. Subpkgs contain no spaces.
pre_pr_nested_group_pkgs() {
	local root="$1" spec
	for spec in ${nested_go_runs[@]+"${nested_go_runs[@]}"}; do
		if [[ "${spec%%|*}" == "${root}" ]]; then
			printf '%s\n' "${spec#*|}"
		fi
	done
}
