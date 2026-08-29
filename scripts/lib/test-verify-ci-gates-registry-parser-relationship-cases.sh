#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2154
# Parser-relationship trigger cases for test-verify-ci-gates-registry.sh.
# Sourced by the parent test, which owns repo_root, registry, fail(), and
# require_path_line(); not intended to run standalone.

check_parser_relationship_trigger_parity() {
	local gate_block ledger_path selection helper_path docs_path docs_trigger
	local parser_docs_path parser_docs_trigger
	local test_helper_trigger

	gate_block="$(
		sed -n '/^  - id: parser-relationship-kit$/,/^  - id:/p' "${registry}"
	)"
	ledger_path='specs/language-feature-parity-ledger.v1.yaml'

	require_path_line "${gate_block}" "${ledger_path}" \
		"parser-relationship-kit registry triggers omit the language feature parity ledger"

	selection="$(
		printf '%s\n' "${ledger_path}" |
			(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
				--registry "${registry}" --tier pre-pr --paths-from - --explain)
	)"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+parser-relationship-kit[[:space:]]' >/dev/null ||
		fail "a ledger-only change did not select parser-relationship-kit (${ledger_path})"
	printf '%s\n' "${selection}" |
		rg --fixed-strings -- "matched trigger \"${ledger_path}\" on path \"${ledger_path}\"" >/dev/null ||
		fail "parser-relationship-kit selected ledger-only change for the wrong reason (${ledger_path})"

	docs_path='docs/public/reference/dependency-coverage.md'
	docs_trigger='docs/**/*.md'
	require "parser relationship static-contract output" \
		'parserrelationship: ${{ steps.filter.outputs.parserrelationship }}' \
		"${static_contract_workflow}"
	rg --multiline --fixed-strings -- \
		"            parserrelationship:
              - '.github/workflows/static-contract-gates.yml'
              - 'specs/ci-gates.v1.yaml'
              - '${docs_trigger}'" \
		"${static_contract_workflow}" >/dev/null ||
		fail "static-contract-gates.yml parserrelationship filter omits ${docs_trigger}"
	require "parser relationship static-contract matrix entry" \
		'append_gate "${{ steps.filter.outputs.parserrelationship }}" "parserrelationship" "Verify parser relationship kit gate" "bash scripts/test-verify-parser-relationship-kit.sh" "bash scripts/verify-parser-relationship-kit.sh"' \
		"${static_contract_workflow}"
	printf '%s\n' "${gate_block}" |
		rg -F 'workflow: static-contract-gates.yml' >/dev/null ||
		fail "parser-relationship-kit registry does not name static-contract-gates.yml"
	printf '%s\n' "${gate_block}" |
		rg -F 'job: "Verify parser relationship kit gate"' >/dev/null ||
		fail "parser-relationship-kit registry does not name the static matrix job"

	require_path_line "${gate_block}" "${docs_trigger}" \
		"parser-relationship-kit registry triggers omit Markdown docs scanned for stale Cargo selectors"
	selection="$(
		printf '%s\n' "${docs_path}" |
			(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
				--registry "${registry}" --tier pre-pr --paths-from - --explain)
	)"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+parser-relationship-kit[[:space:]]' >/dev/null ||
		fail "a docs-only change did not select parser-relationship-kit (${docs_path})"
	printf '%s\n' "${selection}" |
		rg --fixed-strings -- "matched trigger \"${docs_trigger}\" on path \"${docs_path}\"" >/dev/null ||
		fail "parser-relationship-kit selected docs-only change for the wrong reason (${docs_path})"

	parser_docs_path='go/internal/parser/rust/README.md'
	parser_docs_trigger='go/internal/parser/**'
	require_path_line "${gate_block}" "${parser_docs_trigger}" \
		"parser-relationship-kit registry triggers omit parser package Markdown"
	rg --multiline --fixed-strings -- \
		"              - '${docs_trigger}'
              - '${parser_docs_trigger}'" \
		"${static_contract_workflow}" >/dev/null ||
		fail "static-contract-gates.yml parserrelationship filter omits ${parser_docs_trigger}"
	selection="$(
		printf '%s\n' "${parser_docs_path}" |
			(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
				--registry "${registry}" --tier pre-pr --paths-from - --explain)
	)"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+parser-relationship-kit[[:space:]]' >/dev/null ||
		fail "a parser README change did not select parser-relationship-kit (${parser_docs_path})"
	printf '%s\n' "${selection}" |
		rg --fixed-strings -- "matched trigger \"${parser_docs_trigger}\" on path \"${parser_docs_path}\"" >/dev/null ||
		fail "parser-relationship-kit selected parser README for the wrong reason (${parser_docs_path})"

	test_helper_trigger='scripts/lib/test-verify-parser-relationship-kit-*'
	require_path_line "${gate_block}" "${test_helper_trigger}" \
		"parser-relationship-kit registry triggers omit its sourced test helpers"

	for helper_path in \
		scripts/lib/parser_documented_test_commands.sh \
		scripts/lib/parser_documented_test_goflags.sh \
		scripts/lib/parser_documented_test_preamble_assignments.sh \
		scripts/lib/parser_documented_test_scan.sh \
		scripts/lib/test-verify-parser-relationship-kit-cargo-selector-cases.sh \
		scripts/lib/test-verify-parser-relationship-kit-cargo-selector-integration-cases.sh \
		scripts/lib/test-verify-parser-relationship-kit-rust-selector-derived-cases.sh; do
		if [[ "${helper_path}" != scripts/lib/test-verify-parser-relationship-kit-* ]]; then
			require_path_line "${gate_block}" "${helper_path}" \
				"parser-relationship-kit registry triggers omit sourced helper ${helper_path}"
		fi
		selection="$(
			printf '%s\n' "${helper_path}" |
				(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
					--registry "${registry}" --tier pre-pr --paths-from - --explain)
		)"
		printf '%s\n' "${selection}" |
			rg '^SELECTED[[:space:]]+parser-relationship-kit[[:space:]]' >/dev/null ||
			fail "a helper-only change did not select parser-relationship-kit (${helper_path})"
	done
}
