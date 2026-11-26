#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2154
# Parser-relationship trigger cases for test-verify-ci-gates-registry.sh.
# Sourced by the parent test, which owns repo_root, registry, fail(), and
# require_path_line(); not intended to run standalone.

check_parser_relationship_trigger_parity() {
	local gate_block gate_job ledger_path selection
	local helper_path docs_path docs_trigger root_docs_path root_docs_trigger
	local parser_docs_path parser_docs_trigger test_helper_trigger

	gate_block="$(
		sed -n '/^  - id: parser-relationship-kit$/,/^  - id:/p' "${registry}"
	)"
	gate_job="$(
		sed -n '/^  gate:$/,$p' "${static_contract_workflow}"
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

	require "parser relationship verifier step" \
		'bash scripts/verify-parser-relationship-kit.sh' \
		"${static_contract_workflow}"
	rg --multiline --fixed-strings -- \
		"      - name: Checkout repository
        uses: actions/checkout@v5
        with:
          fetch-depth: 2" \
		<(printf '%s\n' "${gate_job}") >/dev/null ||
		fail "the shared static-contract gate checkout needs exactly fetch-depth 2 for parser relationship diff-base selection"
	printf '%s\n' "${gate_block}" |
		rg -F 'workflow: static-contract-gates.yml' >/dev/null ||
		fail "parser-relationship-kit registry does not name static-contract-gates.yml"
	printf '%s\n' "${gate_block}" |
		rg -F 'job: "Product Contract Gates"' >/dev/null ||
		fail "parser-relationship-kit registry does not name the Product Contract Gates job"

	docs_path='docs/public/reference/dependency-coverage.md'
	docs_trigger='docs/**/*.md'
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

	root_docs_path='README.md'
	root_docs_trigger='*.md'
	require_path_line "${gate_block}" "${root_docs_trigger}" \
		"parser-relationship-kit registry triggers omit root-level Markdown"

	parser_docs_path='go/internal/parser/rust/README.md'
	parser_docs_trigger='go/internal/parser/**'
	require_path_line "${gate_block}" "${parser_docs_trigger}" \
		"parser-relationship-kit registry triggers omit parser package Markdown"

	test_helper_trigger='scripts/lib/test-verify-parser-relationship-kit-*'
	require_path_line "${gate_block}" "${test_helper_trigger}" \
		"parser-relationship-kit registry triggers omit its sourced test helpers"

	for helper_path in \
		scripts/lib/parser_documented_test_commands.sh \
		scripts/lib/parser_documented_test_goflags.sh \
		scripts/lib/parser_documented_test_preamble_assignments.sh \
		scripts/lib/parser_documented_test_scan.sh; do
		require_path_line "${gate_block}" "${helper_path}" \
			"parser-relationship-kit registry triggers omit sourced helper ${helper_path}"
	done
}
