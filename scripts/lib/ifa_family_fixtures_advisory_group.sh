#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # globals consumed by the gates sourcing ifa_family_fixtures.sh
# advisory_group fixture globals (#10466), split out of
# ifa_family_fixtures.sh because that file sits at the 500-line cap:
# the globals a new family registers there (comment, two paths, two
# existence guards) no longer fit, and trimming sibling rationale to
# fit is the failure mode the cap guidance forbids. Sourced by
# ifa_family_fixtures.sh after `repo_root` is set, same contract.
#
# The cassette is a projection of the compiled AdvisoryGroupFamilyOdu
# by construction (TestDirectFamilyCassettesMatchTheirCompiledOdu
# holds it to that) -- two git repository scopes and one findings
# scope holding six reducer_supply_chain_impact_finding facts in one
# CVE-keyed alias group, members under provenance. No projector
# trigger rides it: the live cells run the production winners rebuild
# and enqueuer between drains (eshu-ifa enqueue-advisory-groups).
advisory_group_cassette="${repo_root}/testdata/cassettes/advisorygroup/ifa-advisory-group-family.json"
advisory_group_expected_edges="${repo_root}/go/internal/ifa/testdata/advisorygroup/ifa-advisory-group-family-expected-edges.json"

# ifa_family_fixtures_advisory_group_require fails fast when either
# fixture is missing, called from ifa_family_fixtures_require so the
# gate dies before any Compose stack starts, like every sibling guard.
ifa_family_fixtures_advisory_group_require() {
	local gate="$1"
	[[ -f "${advisory_group_cassette}" ]] || { echo "${gate}: advisory-group cassette not found: ${advisory_group_cassette}" >&2; exit 1; }
	[[ -f "${advisory_group_expected_edges}" ]] || { echo "${gate}: advisory-group expected-edge set not found: ${advisory_group_expected_edges}" >&2; exit 1; }
}
