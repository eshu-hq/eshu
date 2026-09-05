#!/usr/bin/env bash
# #6545: docs/internal and docs/public must bite through both real CLI modes.
# Existing go/ cap, ledger, and exclusion cases remain in the other helpers.
run_markdown_scope_cases() {
	run_markdown_debt_import_cases
	local scope mode repo tsv path
	for scope in internal/evidence public/reference; do
		for mode in --all --files; do
			repo="$(new_scratch_repo)"
			tsv="$(write_ledger "${repo}")"
			path="docs/${scope}/scope-proof.md"
			write_md_lines "${repo}/${path}" 501
			if [[ "${mode}" == --all ]]; then
				run_mdcap "${repo}" "${tsv}" --all
			else
				run_mdcap "${repo}" "${tsv}" --files "${path}"
			fi
			printf 'scope CLI: %s %s seeded exit=%d\n' "${scope}" "${mode}" "${MDCAP_EXIT}"
			assert_exit "${MDCAP_EXIT}" 1 "${scope} ${mode}: over-cap fails"
			assert_contains "${MDCAP_OUT}" "${path} has 501 lines, exceeding the 500-line cap" \
				"${scope} ${mode}: failure names file and violation"
			assert_contains "${MDCAP_OUT}" 'evaluated 1 Markdown file(s)' \
				"${scope} ${mode}: nonzero scan count"
			write_md_lines "${repo}/${path}" 500
			if [[ "${mode}" == --all ]]; then
				run_mdcap "${repo}" "${tsv}" --all
			else
				run_mdcap "${repo}" "${tsv}" --files "${path}"
			fi
			assert_exit "${MDCAP_EXIT}" 0 "${scope} ${mode}: reduction to 500 restores green"
			assert_contains "${MDCAP_OUT}" 'evaluated 1 Markdown file(s)' \
				"${scope} ${mode}: green still scans page"
			rm "${repo}/${path}"
			if [[ "${mode}" == --all ]]; then
				run_mdcap "${repo}" "${tsv}" --all
			else
				run_mdcap "${repo}" "${tsv}" --files "${path}"
			fi
			assert_exit "${MDCAP_EXIT}" 0 "${scope} ${mode}: removal restores green"
			rm -rf "${repo}"
		done
	done
}

# A newly governed doc can import only debt already present at the exact base
# path. The ledger exists (without that row) in a real immutable base commit.
run_markdown_debt_import_cases() {
	local scope scenario mode repo tsv path base base_lines live_lines pin expected
	for scope in internal/evidence public/reference; do
		for scenario in equal shrink new compliant inflated moved; do
			case "${scenario}" in
				equal) base_lines=600; live_lines=600; pin=600; expected=0 ;;
				shrink) base_lines=600; live_lines=550; pin=550; expected=0 ;;
				new|moved) base_lines=0; live_lines=600; pin=600; expected=1 ;;
				compliant) base_lines=500; live_lines=600; pin=600; expected=1 ;;
				inflated) base_lines=600; live_lines=650; pin=650; expected=1 ;;
			esac
			for mode in --all --files; do
				repo="$(new_scratch_repo)"
				tsv="${repo}/ledger.tsv"
				path="docs/${scope}/scope-debt.md"
				if [[ "${base_lines}" -gt 0 ]]; then
					write_md_lines "${repo}/${path}" "${base_lines}"
				fi
				if [[ "${scenario}" == moved ]]; then
					write_md_lines "${repo}/docs/${scope}/old-name.md" 600
				fi
				base="$(mdcap_commit_baseline "${repo}" "${tsv}")"
				write_md_lines "${repo}/${path}" "${live_lines}"
				if [[ "${scenario}" == moved ]]; then
					rm "${repo}/docs/${scope}/old-name.md"
				fi
				printf '%s\t%s\n' "${path}" "${pin}" >>"${tsv}"
				if [[ "${mode}" == --all ]]; then
					MARKDOWN_LINE_CAP_BASE_REF="${base}" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
						MARKDOWN_LINE_CAP_REQUIRE_BASE=1 run_mdcap "${repo}" "${tsv}" --all
				else
					MARKDOWN_LINE_CAP_BASE_REF="${base}" MARKDOWN_LINE_CAP_TSV_REL="ledger.tsv" \
						MARKDOWN_LINE_CAP_REQUIRE_BASE=1 run_mdcap "${repo}" "${tsv}" --files "${path}"
				fi
				printf 'debt CLI: %s %s %s exit=%d expected=%d base=%s\n' \
					"${scope}" "${scenario}" "${mode}" "${MDCAP_EXIT}" "${expected}" "${base}"
				assert_exit "${MDCAP_EXIT}" "${expected}" "${scope} ${scenario} ${mode}: immutable-base debt import"
				if [[ "${MDCAP_EXIT}" -ne "${expected}" ]]; then
					printf '%s\n' "${MDCAP_OUT}"
				fi
				if [[ "${expected}" -eq 1 ]]; then
					assert_contains "${MDCAP_OUT}" "${path}" "${scope} ${scenario} ${mode}: refusal names path"
				fi
				rm -rf "${repo}"
			done
		done
	done
}
