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
# trigger containing "*", and drift skips those. So the GLOB triggers -- the
# ones this design leans on hardest, because a family's row and pin files are
# reached only through scripts/lib/ifa_family_registry/** and
# scripts/lib/ifa_family_registry_pins/** -- are the half nothing checked.
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
