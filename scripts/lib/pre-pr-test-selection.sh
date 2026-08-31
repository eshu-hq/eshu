#!/usr/bin/env bash
# Pure package-target selection for scripts/dev/pre-pr.sh's focused Go tests.
#
# Sourced, never executed. The caller writes changed Go package directories and
# fixture-consumer directories to stdin. This function deduplicates them,
# expands a direct parent-parser change to the recursive parser tree so child
# external tests still exercise the parent Engine contract, and applies the
# cross-package table below.

# pre_pr_cross_package_test_dirs: sibling package(s) that hold the only
# assertions for a changed package's behavior, printed one per line for the
# changed directory passed as $1 (nothing for a directory with no entry).
#
# The parent-tree expansion below covers a parent change reaching child tests.
# This table covers the other direction: a child package whose black-box Engine
# coverage lives in a SIBLING child package, which the focused selector would
# otherwise never reach, because a child-only change deliberately stays focused.
#
# `./internal/parser/json`: the manifest payload rows `json/dbt_manifest.go`
# builds -- the `COMPILES_TO` and `USES_MACRO` relationship rows and the
# `coalesce` transform metadata -- are asserted only by
# `dbtsql/json_dbt_test.go`, which drives the parent Engine over a dbt manifest.
# The json package's own tests assert `COLUMN_DERIVES_FROM` and coverage state
# and nothing else, so without this entry a `COMPILES_TO`/`USES_MACRO`
# regression passes `make pre-pr` and fails only in CI's whole-module run.
#
# Entries are exact directory matches, and are not existence-checked, matching
# fixture_consumer_dirs in scripts/lib/pre-pr-fixture-consumers.sh: an entry
# whose target package is deleted fails loudly in `go test` rather than
# silently selecting nothing. Extend this table when black-box coverage for one
# package is the only coverage and lives in another.
pre_pr_cross_package_test_dirs() {
	case "$1" in
	./internal/parser/json) printf './internal/parser/dbtsql\n' ;;
	esac
}

pre_pr_select_test_dirs() {
	local input_dirs=() expanded=() selected=() d mapped existing seen
	local has_parser_tree=0

	while IFS= read -r d; do
		[[ -n "${d}" ]] && input_dirs+=("${d}")
	done

	# Cross-package expansion runs first so the parent-tree collapse below still
	# absorbs anything it adds under ./internal/parser.
	if [[ ${#input_dirs[@]} -gt 0 ]]; then
		for d in "${input_dirs[@]}"; do
			expanded+=("${d}")
			while IFS= read -r mapped; do
				[[ -n "${mapped}" ]] && expanded+=("${mapped}")
			done < <(pre_pr_cross_package_test_dirs "${d}")
		done
		input_dirs=("${expanded[@]}")
	fi

	if [[ ${#input_dirs[@]} -gt 0 ]]; then
		for d in "${input_dirs[@]}"; do
			if [[ "${d}" == "./internal/parser" || "${d}" == "./internal/parser/..." ]]; then
				has_parser_tree=1
				break
			fi
		done
	fi

	if [[ ${#input_dirs[@]} -gt 0 ]]; then
		for d in "${input_dirs[@]}"; do
			if [[ "${d}" == "./internal/parser" || "${d}" == "./internal/parser/..." ]]; then
				d="./internal/parser/..."
			elif [[ ${has_parser_tree} -eq 1 && "${d}" == ./internal/parser/* ]]; then
				continue
			fi

			seen=0
			if [[ ${#selected[@]} -gt 0 ]]; then
				for existing in "${selected[@]}"; do
					if [[ "${existing}" == "${d}" ]]; then
						seen=1
						break
					fi
				done
			fi
			[[ ${seen} -eq 0 ]] && selected+=("${d}")
		done
	fi

	if [[ ${#selected[@]} -gt 0 ]]; then
		printf '%s\n' "${selected[@]}"
	fi
}
