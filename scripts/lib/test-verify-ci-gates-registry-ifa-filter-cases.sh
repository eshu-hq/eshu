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
# trigger containing "*", and drift skips those. So the GLOB triggers are the
# half nothing checked -- and for THIS gate that means exactly one,
# scripts/lib/ifa_family_registry/**, which is the only door a family's row
# files are reached through. (The sibling scripts/lib/ifa_family_registry_pins/**
# belongs to ifa-determinism, not to this gate; its parity is covered by the
# registry-subset-of-workflow loop in
# scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh.)
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
	workflow_filter="$(sed -n '/^[[:space:]]*ifa:$/,/^[[:space:]]*[a-z][a-z0-9]*:$/p' "${static_contract_workflow}")"
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
		'append_gate "${{ steps.filter.outputs.ifa }}" "ifa" "Verify Ifa contract-layer gate" "cd go && go test ./internal/ifa ./cmd/ifa -count=1 && go test ./internal/reducer -count=1" "cd go && go test ./internal/ifa ./cmd/ifa -count=1 && go test ./internal/reducer -count=1"' \
		"${static_contract_workflow}"


	# Triggers are READ OUT OF the gate block, never restated here. The sibling
	# module (test-verify-ci-gates-registry-docs-cli-env-cases.sh) records why:
	# a hand-maintained copy only ever proves the pairs someone remembered to
	# add, and #6059's go/internal/cli/** reached the spec, never reached the
	# workflow, and stayed green because the test's own list did not know about
	# it either. A third family-registry glob added later is covered by this
	# loop the moment it lands in the spec.
	# Scoped to the shell family-registry globs. Two reasons, both load-bearing.
	# First, this check asserts EXACT STRING presence, and that is only a valid
	# test when no broader pattern in the filter already covers the glob:
	# go/internal/ifa/materialized_edges*.go is a registry glob the filter does
	# not name, and does not need to, because go/internal/ifa/** covers it.
	# Asserting on every glob reports that as missing, which is a false positive.
	# Second, the family-registry globs are the ones with no broader pattern
	# above them -- nothing in the ifa filter covers scripts/lib/** -- so exact
	# presence is exactly right for them and wrong for the rest.
	#
	# Still derived within that scope: a third scripts/lib/ifa_family_registry*
	# glob added to the spec is picked up by this loop without editing here.
	# A glob that DOES gain a broader covering pattern is out of scope by
	# construction, and stating that is better than a check whose passes and
	# failures both need interpreting.
	local -a glob_triggers=()
	while IFS= read -r trigger; do
		[[ -n "${trigger}" ]] || continue
		[[ "${trigger}" == *"*"* ]] || continue
		[[ "${trigger}" == scripts/lib/ifa_family_registry* ]] || continue
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
		|| fail "extracted zero scripts/lib/ifa_family_registry* glob triggers from the ifa-materialized-edge-coverage gate block -- the trigger parse broke, so the ifa filter parity check would pass vacuously"

	for trigger in "${glob_triggers[@]}"; do
		printf '%s\n' "${workflow_filter}" | rg --fixed-strings --quiet -- "${trigger}" \
			|| fail "static-contract-gates.yml's ifa filter omits the glob trigger ${trigger}, which specs/ci-gates.v1.yaml declares for ifa-materialized-edge-coverage -- drift.go skips glob triggers, so nothing else catches this: the gate is selected as blocking and its job never scheduled"
	done
	printf 'test-verify-ci-gates-registry: ifa filter carries all %d family-registry glob trigger(s) the registry declares\n' "${#glob_triggers[@]}"
}
