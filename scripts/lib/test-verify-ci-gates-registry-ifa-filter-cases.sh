#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2154
# Glob-trigger parity for the Ifa dorny path filter in
# .github/workflows/static-contract-gates.yml. Sourced by
# scripts/test-verify-ci-gates-registry.sh, which owns repo_root, fail(),
# require(), ${registry} and ${static_contract_workflow}.
#
# WHAT IS ALREADY COVERED, so this file does not claim it: checkPathFilterCoverage
# (go/internal/cigates/pathfilter.go:333) walks every gate whose CI workflow uses
# a dorny filter and asserts each registry trigger is selected by that gate's
# filter key. Moving scripts/lib/ifa_family_registry.sh out of the ifa: filter
# fails `verify-ci-gates-registry.sh --drift` today, without this module.
#
# WHAT IS NOT: isLiteralTrigger (pathfilter.go:305) returns false for any
# trigger containing "*", and drift skips those. This gate declares eleven glob
# triggers, so eleven of its triggers were the half nothing checked -- including
# go/internal/reducer/** and scripts/lib/ifa_family_registry/**, the two doors
# the Go blocker lockstep and a family's row files are reached through. The loop
# below is total over all eleven.
# Narrowing a "**" to a "*" in the workflow (dorny "*" does not cross "/") left
# the whole registry test green: an edit to rows/NN_<family>.sh would then
# select ifa-materialized-edge-coverage as BLOCKING while the job that runs it
# is never scheduled, so the required check stays MISSING and the PR cannot
# merge. Same dark-gate class as #6164, reached through the one door drift
# leaves open.

run_ci_gates_registry_ifa_filter_cases() {
	local gate_block workflow_filter trigger
	gate_block="$(sed -n '/^  - id: ifa-materialized-edge-coverage$/,/^  - id: /p' "${registry}")"
	[[ -n "${gate_block}" ]] \
		|| fail "missing ifa-materialized-edge-coverage registry gate"
	# Scope to the ifa: filter block, not the whole workflow. Searching the file
	# would let a pattern living under some other filter key satisfy an
	# assertion whose message says "the ifa filter".
	workflow_filter="$(sed -n '/^[[:space:]]*ifa:$/,/^[[:space:]]*[a-z][a-z0-9_-]*:$/p' "${static_contract_workflow}")"
	[[ -n "${workflow_filter}" ]] \
		|| fail "missing ifa workflow filter in ${static_contract_workflow##*/}"

	# Literal pins on the workflow's ifa job. These are NOT redundant with the
	# derived glob loop below, and they are not redundant with drift.go either:
	# drift proves a registry trigger is SELECTED by the filter, and says nothing
	# about the job's command. Restored here after the rewrite of this module
	# silently dropped them -- `cat >` over a file that had just absorbed them
	# from scripts/test-verify-ci-gates-registry.sh, which is how a guard
	# disappears without anyone deciding to remove it. Both were proven
	# load-bearing by mutation: drop the reducer leg, or drop
	# go/internal/ifa/** from the filter, and without these the whole registry
	# test still exits 0.
	require "Ifa workflow filter" \
		"ifa:" \
		"${static_contract_workflow}"
	require "Ifa workflow path filter" \
		"go/internal/ifa/**" \
		"${static_contract_workflow}"
	# The reducer leg is part of the pinned text on purpose. The registry's
	# local.command for ifa-materialized-edge-coverage runs it, and the Go
	# blocker-shape lockstep this gate's triggers exist for lives in that package --
	# the CI job ran without it until #6147, so the gate's own comment claiming the
	# triggers protected that lockstep in CI was false. Pinning the whole command
	# keeps local and CI reading the same thing; dropping the leg here again should
	# fail loudly.
	require "Ifa workflow matrix entry" \
		'append_gate "${{ steps.filter.outputs.ifa }}" "ifa" "Verify Ifa contract-layer gate" "cd go && go test ./internal/ifa ./internal/ifa/materializededges ./cmd/ifa -count=1 && go test ./internal/reducer/... -count=1" "cd go && go test ./internal/ifa ./internal/ifa/materializededges ./cmd/ifa -count=1 && go test ./internal/reducer/... -count=1"' \
		"${static_contract_workflow}"


	# Triggers are READ OUT OF the gate block, never restated here. The sibling
	# module (test-verify-ci-gates-registry-docs-cli-env-cases.sh) records why:
	# a hand-maintained copy only ever proves the pairs someone remembered to
	# add, and #6059's go/internal/cli/** reached the spec, never reached the
	# workflow, and stayed green because the test's own list did not know about
	# it either. A third family-registry glob added later is covered by this
	# loop the moment it lands in the spec.
	# TOTAL over the gate's glob triggers, not a chosen subset. An earlier
	# version scoped this to scripts/lib/ifa_family_registry* and justified it by
	# claiming those were "the ones with no broader pattern above them". That was
	# false -- go/internal/reducer/**, go/cmd/ifa/** and the cassette globs have
	# no broader pattern in the ifa filter either -- and it left ten of the
	# gate's eleven glob triggers unchecked while the header claimed to close
	# "the half nothing checked".
	#
	# What made scoping look necessary was a real false positive:
	# go/internal/ifa/materializededges/** is a trigger the filter does not
	# name and does not need to, because go/internal/ifa/** already covers it.
	# The fix is to model that coverage rather than to duck it. A dorny entry
	# "P/**" covers a trigger T when T starts with "P/", so a trigger is
	# satisfied by an exact match OR by any prefix-glob entry above it. That is
	# sound for the "**" form the filter uses and lets the loop run over
	# everything.
	local -a glob_triggers=()
	while IFS= read -r trigger; do
		[[ -n "${trigger}" ]] || continue
		[[ "${trigger}" == *"*"* ]] || continue
		glob_triggers+=("${trigger}")
	done < <(
		printf '%s\n' "${gate_block}" |
			sed -n '/^[[:space:]]*triggers:$/,/^[[:space:]]*local:$/p' |
			sed -n 's/^      - "\(.*\)"$/\1/p'
	)

	# Derivation cuts both ways: if the extraction above silently yields nothing
	# (a reindent, a renamed key), every assertion below becomes vacuous. This
	# gate has carried glob triggers since it was written, so zero means the
	# parse broke, not that the spec changed.
	[[ "${#glob_triggers[@]}" -gt 0 ]] \
		|| fail "extracted zero glob triggers from the ifa-materialized-edge-coverage gate block -- the trigger parse broke, so the ifa filter parity check would pass vacuously"

	local -a filter_entries=()
	while IFS= read -r entry; do
		[[ -n "${entry}" ]] && filter_entries+=("${entry}")
	done < <(printf '%s\n' "${workflow_filter}" | sed -n "s/^ *- '\(.*\)'\$/\1/p")
	[[ "${#filter_entries[@]}" -gt 0 ]] \
		|| fail "extracted zero entries from static-contract-gates.yml's ifa filter -- the filter parse broke, so this check would pass vacuously"

	local entry prefix covered
	for trigger in "${glob_triggers[@]}"; do
		covered=0
		for entry in "${filter_entries[@]}"; do
			if [[ "${entry}" == "${trigger}" ]]; then
				covered=1
				break
			fi
			# A "P/**" entry covers everything under P/.
			if [[ "${entry}" == */'**' ]]; then
				prefix="${entry%/'**'}"
				if [[ "${trigger}" == "${prefix}/"* ]]; then
					covered=1
					break
				fi
			fi
		done
		[[ "${covered}" -eq 1 ]] \
			|| fail "static-contract-gates.yml's ifa filter neither names the glob trigger ${trigger} nor carries a broader \"P/**\" entry covering it, while specs/ci-gates.v1.yaml declares it for ifa-materialized-edge-coverage -- drift.go skips glob triggers, so nothing else catches this: the gate is selected as blocking and its job never scheduled"
	done

	# The literal (non-glob) family-registry trigger is asserted on the REGISTRY
	# side too. drift.go proves the workflow filter selects it; nothing proved
	# the gate still declares it, and deleting it from the spec left this whole
	# test green. The glob loop above cannot cover it -- it is not a glob.
	printf '%s\n' "${gate_block}" | rg --fixed-strings -- '- "scripts/lib/ifa_family_registry.sh"' >/dev/null \
		|| fail "the ifa-materialized-edge-coverage gate no longer triggers on scripts/lib/ifa_family_registry.sh -- an edit to the fail-closed row loader or its accessors would not select the gate that runs ifa_family_registry_parse_test.go"

	printf 'test-verify-ci-gates-registry: ifa filter covers all %d glob trigger(s) the registry declares\n' "${#glob_triggers[@]}"
}
