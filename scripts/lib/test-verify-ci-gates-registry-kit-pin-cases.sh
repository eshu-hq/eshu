#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2154
# Kit-pin trigger cases for test-verify-ci-gates-registry.sh.
# Sourced by the parent test, which owns repo_root, registry, and fail();
# not intended to run standalone.
#
# The kit revision is pinned by the SHA in scripts/ci/install-eshu-agent-kit.sh.
# .agent-kit-version only changes when the kit's version number does, and the
# kit has stayed at v0.1.0 while its content moved. A REF-only bump must
# therefore select every gate that reads the installed kit material, and the
# matching CI path filter must list the script too. skill-roundtrip is not in
# this list on purpose: it reads only the committed skill-fragments/,
# expected/ and specs/surface-inventory.v1.yaml, so a kit revision cannot
# change its result.

check_kit_pin_installer_selects_kit_gates() {
	local script='scripts/ci/install-eshu-agent-kit.sh'
	local filters="${repo_root}/.github/static-contract-filters.yml"
	local gate selection block filter_block

	for gate in codex-goal-continuation skill-workflow-refs; do
		selection="$(
			printf '%s\n' "${script}" |
				(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
					--registry "${registry}" --tier pre-pr --paths-from - --explain)
		)"
		printf '%s\n' "${selection}" |
			rg "^SELECTED[[:space:]]+${gate}[[:space:]]" >/dev/null ||
			fail "a kit-pin-only change (${script}) did not select ${gate}"
	done

	for block in skillworkflowrefs; do
		filter_block="$(sed -n "/^${block}:\$/,/^[a-z]*:\$/p" "${filters}")"
		printf '%s\n' "${filter_block}" |
			rg --fixed-strings --line-regexp -- "  - '${script}'" >/dev/null ||
			fail "static-contract-filters.yml ${block} filter omits ${script}"
	done
}
