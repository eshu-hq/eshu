#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# fail(), and the ${repo_root}/${registry}/${workflow} path variables are all
# defined by scripts/test-verify-ifa-determinism.sh before it sources this
# file; shellcheck cannot see that from this file alone.
# CI-gate registry/workflow lockstep mechanism cases for
# scripts/test-verify-ifa-determinism.sh, sourced so the top-level mirror
# stays below the repository's 500-line cap (mirroring the fault-injection
# sibling's per-mechanism case-module split, e.g.
# scripts/lib/test-ifa-fault-injection-marker-cases.sh). Proves, through the
# real ci-gates registry matcher rather than a text grep, that: every IFA
# proof-input seam the hand-maintained selector-cases table lists also
# retriggers the workflow AND is present in both the ifa-determinism and
# ifa-fault-injection registry entries (workflow superset-of-table); every
# trigger the registry actually declares also appears in the workflow, so a
# registry-only trigger can never leave a BLOCKING gate that GitHub never
# starts (registry subset-of-workflow); a fault-only input selects
# ifa-fault-injection but never ifa-determinism; and a determinism-only input
# (the mirror's own sourced case modules, classified by where they execute,
# not what they are about) selects ifa-determinism but never
# ifa-fault-injection -- guarding against over-triggering the fault gate's
# four-shard Docker matrix on an edit it cannot even observe. The self-check on
# `${BASH_SOURCE[0]}` below intentionally resolves to THIS file once sourced
# into a function (proven empirically: inside a bash function, BASH_SOURCE[0]
# is the file where the function is lexically defined, not the caller) --
# both the check and the `source` line it is checking for must stay together
# in this same file for that to keep working.
run_ifa_determinism_registry_lockstep_cases() {
determinism_registry="$(sed -n '/^  - id: ifa-determinism$/,/^  - id:/p' "${registry}")"
fault_registry="$(sed -n '/^  - id: ifa-fault-injection$/,/^  - id:/p' "${registry}")"
selector_cases_lib="${repo_root}/scripts/lib/ifa_live_gate_selector_cases.sh"
rg --quiet --fixed-strings --line-regexp -- 'source "${selector_cases_lib}"' "${BASH_SOURCE[0]}" \
	|| fail "selector cases must be sourced from scripts/lib/ifa_live_gate_selector_cases.sh"
# shellcheck source=scripts/lib/ifa_live_gate_selector_cases.sh
source "${selector_cases_lib}"
for seam in "${ifa_live_gate_common_seams[@]}"; do
	trigger="${seam%%|*}"
	concrete_path="${seam#*|}"
	rg --fixed-strings --quiet -- "- '${trigger}'" "${workflow}" \
		|| fail "workflow does not retrigger the live matrices for IFA proof input: ${trigger}"
	printf '%s\n' "${determinism_registry}" | rg --fixed-strings --quiet -- "- \"${trigger}\"" \
		|| fail "ifa-determinism registry entry omits IFA proof input: ${trigger}"
	printf '%s\n' "${fault_registry}" | rg --fixed-strings --quiet -- "- \"${trigger}\"" \
		|| fail "ifa-fault-injection registry entry omits IFA proof input: ${trigger}"
	selection="$(printf '%s\n' "${concrete_path}" | (
		cd "${repo_root}/go"
		go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain
	))"
	for gate in ifa-determinism ifa-fault-injection; do
		printf '%s\n' "${selection}" | rg --quiet -- "^SELECTED[[:space:]]+${gate}[[:space:]]" \
			|| fail "${concrete_path} does not select ${gate} through the real registry matcher"
	done
done

# The loop above validates workflow ⊇ selector-cases: every seam in the
# hand-maintained table must appear in the workflow. It cannot see a registry
# trigger that was never added to the table, so a family could add triggers to
# specs/ci-gates.v1.yaml, omit them here, and stay green while the workflow
# never starts for those paths -- the registry marks both gates BLOCKING, GitHub
# never runs them, and the required-gates publisher waits forever on checks that
# never arrive. #5994 landed 3 of its 10 triggers that way and this loop is what
# caught the other 7 (plus a pre-existing gap on go/cmd/ifa/assert_edges.go).
#
# This second loop closes the other direction: registry ⊆ workflow, derived from
# the committed registry rather than from any hand-maintained list, so it cannot
# drift out of date the way the table can.
for gate_id in ifa-determinism ifa-fault-injection; do
	case "${gate_id}" in
	ifa-determinism) gate_block="${determinism_registry}" ;;
	*) gate_block="${fault_registry}" ;;
	esac
	while IFS= read -r registry_trigger; do
		[[ -n "${registry_trigger}" ]] || continue
		rg --fixed-strings --quiet -- "- '${registry_trigger}'" "${workflow}" \
			|| fail "${gate_id} registry triggers on ${registry_trigger} but ${workflow##*/} never lists it; the gate is selected as blocking and then never starts"
		# Slice the triggers: section before extracting, rather than taking
		# every quoted list item in the gate block. A gate block also carries
		# ci.check_names, whose entries are GitHub check names, not paths --
		# "fault-injection (shard 1/4)" is not a file and must never be
		# demanded of the workflow's paths: list. Extracting blockwide made
		# the first gate to declare check_names fail with a nonsense message
		# about a path the workflow "never lists".
	done < <(printf '%s\n' "${gate_block}" \
		| sed -n '/^    triggers:$/,/^    [a-z_]*:$/p' \
		| rg --only-matching --replace '$1' -- '^\s+- "([^"]+)"\s*$')
done

# Fault-only case data stays separate so the matcher proves these inputs do
# not accidentally broaden the determinism registry.
for seam in "${ifa_live_gate_fault_only_seams[@]}"; do
	trigger="${seam%%|*}"
	concrete_path="${seam#*|}"
	rg --quiet --fixed-strings --line-regexp -- "      - '${trigger}'" "${workflow}" \
		|| fail "workflow does not retrigger fault injection for fault-only input: ${trigger}"
	printf '%s\n' "${fault_registry}" | rg --quiet --fixed-strings --line-regexp -- "      - \"${trigger}\"" \
		|| fail "ifa-fault-injection registry entry omits fault-only input: ${trigger}"
	selection="$(printf '%s\n' "${concrete_path}" | (
		cd "${repo_root}/go"
		go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain
	))"
	printf '%s\n' "${selection}" | rg --quiet '^SELECTED[[:space:]]+ifa-fault-injection[[:space:]]' \
		|| fail "${concrete_path} does not select ifa-fault-injection through the real registry matcher"
	if printf '%s\n' "${selection}" | rg --quiet '^SELECTED[[:space:]]+ifa-determinism[[:space:]]'; then
		fail "fault-only input must not select ifa-determinism: ${concrete_path}"
	fi
done

# Determinism-only case data is the mirror image of the fault-only loop
# above: these inputs (the mirror's own sourced case modules, classified by
# where they EXECUTE -- inside test-verify-ifa-determinism.sh -- not by what
# their content is about) must retrigger ifa-determinism but never
# ifa-fault-injection. Without this, a determinism-only test module could
# silently broaden the fault registry, costing every future family an
# unexercised ~22-minute four-shard fault-injection run on every edit.
for seam in "${ifa_live_gate_determinism_only_seams[@]}"; do
	trigger="${seam%%|*}"
	concrete_path="${seam#*|}"
	rg --quiet --fixed-strings --line-regexp -- "      - '${trigger}'" "${workflow}" \
		|| fail "workflow does not retrigger the determinism matrix for determinism-only input: ${trigger}"
	printf '%s\n' "${determinism_registry}" | rg --quiet --fixed-strings --line-regexp -- "      - \"${trigger}\"" \
		|| fail "ifa-determinism registry entry omits determinism-only input: ${trigger}"
	selection="$(printf '%s\n' "${concrete_path}" | (
		cd "${repo_root}/go"
		go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain
	))"
	printf '%s\n' "${selection}" | rg --quiet '^SELECTED[[:space:]]+ifa-determinism[[:space:]]' \
		|| fail "${concrete_path} does not select ifa-determinism through the real registry matcher"
	if printf '%s\n' "${selection}" | rg --quiet '^SELECTED[[:space:]]+ifa-fault-injection[[:space:]]'; then
		fail "determinism-only input must not select ifa-fault-injection: ${concrete_path}"
	fi
done
}
