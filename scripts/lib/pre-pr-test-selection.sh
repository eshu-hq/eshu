#!/usr/bin/env bash
# Pure package-target selection for scripts/dev/pre-pr.sh's focused Go tests.
#
# Sourced, never executed. The caller writes changed Go package directories and
# fixture-consumer directories to stdin. This function deduplicates them and
# expands a direct parent-parser change to the recursive parser tree so child
# external tests still exercise the parent Engine contract.

pre_pr_select_test_dirs() {
	local input_dirs=() selected=() d existing seen
	local has_parser_tree=0

	while IFS= read -r d; do
		[[ -n "${d}" ]] && input_dirs+=("${d}")
	done

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
