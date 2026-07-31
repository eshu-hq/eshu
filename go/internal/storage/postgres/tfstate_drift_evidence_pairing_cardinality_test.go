// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// pairSpuriousModuleMismatches's cardinality/ambiguity-guard tests. Split
// from tfstate_drift_evidence_pairing_test.go (which keeps the
// resourceAddressKey table test) to stay under the CLAUDE.md 500-line cap.
package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// TestPairSpuriousModuleMismatchesMirrorsReasonOntoUnambiguousStateOnlyRow is
// the direct unit test for the defect: an unresolved module-prefix chain
// produces a config-only row (fallback address, ModuleResolutionReason set)
// and a DIFFERENT, state-only row for the SAME real resource (the real,
// prefixed address). Because the two addresses differ, they never share a
// map key, so nothing upstream of this function would ever mark the
// state-only row low-confidence on its own. pairSpuriousModuleMismatches must
// mirror the reason onto the state-only row when the <type>.<name> pairing is
// unambiguous (exactly one candidate on each side).
func TestPairSpuriousModuleMismatchesMirrorsReasonOntoUnambiguousStateOnlyRow(t *testing.T) {
	t.Parallel()

	config := map[string]*tfconfigstate.ResourceRow{
		"aws_security_group.vpc_endpoints": {
			Address:                "aws_security_group.vpc_endpoints",
			ResourceType:           "aws_security_group",
			ModuleResolutionReason: "external_registry",
		},
	}
	state := map[string]*tfconfigstate.ResourceRow{
		"module.vpc.aws_security_group.vpc_endpoints": {
			Address:      "module.vpc.aws_security_group.vpc_endpoints",
			ResourceType: "aws_security_group",
		},
	}

	pairSpuriousModuleMismatches(config, state)

	got := state["module.vpc.aws_security_group.vpc_endpoints"].ModuleResolutionReason
	if got != "external_registry" {
		t.Fatalf("state row ModuleResolutionReason = %q, want %q (unambiguous pairing must mirror the reason)", got, "external_registry")
	}
}

// TestPairSpuriousModuleMismatchesSkipsAmbiguousResourceKeyCollision bounds
// the false-pairing risk of matching on <type>.<name> alone: Terraform
// modules commonly reuse generic resource names across unrelated modules
// (e.g. terraform-aws-modules's own "aws_s3_bucket.this" convention), so a
// blind match could pair two DIFFERENT resources that merely share a name.
// When more than one state-only row shares the resource key, the pairing
// must refuse to guess and leave every candidate's ModuleResolutionReason
// untouched.
func TestPairSpuriousModuleMismatchesSkipsAmbiguousResourceKeyCollision(t *testing.T) {
	t.Parallel()

	config := map[string]*tfconfigstate.ResourceRow{
		"aws_s3_bucket.this": {
			Address:                "aws_s3_bucket.this",
			ResourceType:           "aws_s3_bucket",
			ModuleResolutionReason: "external_registry",
		},
	}
	state := map[string]*tfconfigstate.ResourceRow{
		"module.a.aws_s3_bucket.this": {
			Address:      "module.a.aws_s3_bucket.this",
			ResourceType: "aws_s3_bucket",
		},
		"module.b.aws_s3_bucket.this": {
			Address:      "module.b.aws_s3_bucket.this",
			ResourceType: "aws_s3_bucket",
		},
	}

	pairSpuriousModuleMismatches(config, state)

	for address, row := range state {
		if row.ModuleResolutionReason != "" {
			t.Fatalf("state row %q ModuleResolutionReason = %q, want empty (ambiguous 1:2 collision must not guess)", address, row.ModuleResolutionReason)
		}
	}
}

// TestPairSpuriousModuleMismatchesIgnoresCleanConfigRows is the negative
// case: a config-only row with no ModuleResolutionReason must never cause a
// pairing, so a genuinely exact added_in_config/added_in_state pair (real,
// independent resources that happen to share a resource key) never gets
// spuriously downgraded.
func TestPairSpuriousModuleMismatchesIgnoresCleanConfigRows(t *testing.T) {
	t.Parallel()

	config := map[string]*tfconfigstate.ResourceRow{
		"aws_iam_role.svc": {
			Address:      "aws_iam_role.svc",
			ResourceType: "aws_iam_role",
		},
	}
	state := map[string]*tfconfigstate.ResourceRow{
		"module.vpc.aws_iam_role.svc": {
			Address:      "module.vpc.aws_iam_role.svc",
			ResourceType: "aws_iam_role",
		},
	}

	pairSpuriousModuleMismatches(config, state)

	if got := state["module.vpc.aws_iam_role.svc"].ModuleResolutionReason; got != "" {
		t.Fatalf("state row ModuleResolutionReason = %q, want empty (config row was clean, no reason to mirror)", got)
	}
}

// TestPairSpuriousModuleMismatchesRefusesDataSourceManagedResourceCollision
// is the permanent regression for the reviewer-driven scenario that
// motivated the "data." prefix decision in resourceAddressKey: a
// low-confidence MANAGED config-only row ("aws_ami.ubuntu") must never pair
// with an unrelated STATE-only data source of the same type and name
// ("data.aws_ami.ubuntu"), since Terraform treats them as entirely
// different resources. Complements
// TestResourceAddressKeyStripsModulePrefixes's "data source never collides"
// subtest, which only proves the two addresses key differently; this test
// proves that difference is actually load-bearing at the
// pairSpuriousModuleMismatches call site the reviewer exercised directly.
func TestPairSpuriousModuleMismatchesRefusesDataSourceManagedResourceCollision(t *testing.T) {
	t.Parallel()

	config := map[string]*tfconfigstate.ResourceRow{
		"aws_ami.ubuntu": {
			Address:                "aws_ami.ubuntu",
			ResourceType:           "aws_ami",
			ModuleResolutionReason: "external_registry",
		},
	}
	state := map[string]*tfconfigstate.ResourceRow{
		"data.aws_ami.ubuntu": {
			Address:      "data.aws_ami.ubuntu",
			ResourceType: "aws_ami",
		},
	}

	pairSpuriousModuleMismatches(config, state)

	if got := state["data.aws_ami.ubuntu"].ModuleResolutionReason; got != "" {
		t.Fatalf("state row ModuleResolutionReason = %q, want empty (a data source must never pair with a managed resource of the same type and name)", got)
	}
}

// TestPairSpuriousModuleMismatchesRefusesWhenMultipleIndexedStateInstancesShareStrippedKey
// covers the count>1 side of the second reviewer's finding: a `count = 3`
// resource inside an unresolved module produces ONE config-only row (the
// parser has no per-instance information, only a static resource block) but
// THREE state-only rows, one per instance, each carrying a DIFFERENT index
// suffix that resourceAddressKey now strips down to the SAME key. That is
// genuinely a 1:3 situation, not 1:1 -- pairSpuriousModuleMismatches's
// unambiguity guard must see 3 candidates on the state side and refuse to
// attribute the mismatch to any single instance, exactly as it already does
// for the unrelated-resources-sharing-a-name case. This is the CORRECT
// outcome, not a residual gap: a spurious mismatch genuinely cannot be
// pinned on one of three siblings.
func TestPairSpuriousModuleMismatchesRefusesWhenMultipleIndexedStateInstancesShareStrippedKey(t *testing.T) {
	t.Parallel()

	config := map[string]*tfconfigstate.ResourceRow{
		"aws_instance.web": {
			Address:                "aws_instance.web",
			ResourceType:           "aws_instance",
			ModuleResolutionReason: "external_registry",
		},
	}
	state := map[string]*tfconfigstate.ResourceRow{
		"module.x.aws_instance.web": {
			Address:      "module.x.aws_instance.web",
			ResourceType: "aws_instance",
		},
		"module.x.aws_instance.web[index:1]": {
			Address:      "module.x.aws_instance.web[index:1]",
			ResourceType: "aws_instance",
		},
		"module.x.aws_instance.web[index:2]": {
			Address:      "module.x.aws_instance.web[index:2]",
			ResourceType: "aws_instance",
		},
	}

	pairSpuriousModuleMismatches(config, state)

	for address, row := range state {
		if row.ModuleResolutionReason != "" {
			t.Fatalf("state row %q ModuleResolutionReason = %q, want empty (count=3 must refuse -- ambiguous 1:3, cannot attribute to one instance)", address, row.ModuleResolutionReason)
		}
	}
}

// TestPairSpuriousModuleMismatchesPairsSingleIndexedStateInstance covers the
// count=1 / single-key for_each side: exactly ONE state instance exists for
// the indexed resource, so after resourceAddressKey strips its index suffix
// the pairing is genuinely unambiguous (1 config-only row, 1 state-only row
// sharing the stripped key) and MUST pair -- this is the exact case the
// pre-index-stripping code silently missed entirely, since
// "aws_instance.web" (config, never indexed) and
// "module.x.aws_instance.web[key:<hash>]" (state, always indexed for a
// for_each instance) never matched as literal strings.
func TestPairSpuriousModuleMismatchesPairsSingleIndexedStateInstance(t *testing.T) {
	t.Parallel()

	t.Run("count=1 style [index:N] suffix", func(t *testing.T) {
		t.Parallel()
		config := map[string]*tfconfigstate.ResourceRow{
			"aws_instance.web": {
				Address:                "aws_instance.web",
				ResourceType:           "aws_instance",
				ModuleResolutionReason: "external_registry",
			},
		}
		state := map[string]*tfconfigstate.ResourceRow{
			"module.x.aws_instance.web[index:0]": {
				Address:      "module.x.aws_instance.web[index:0]",
				ResourceType: "aws_instance",
			},
		}

		pairSpuriousModuleMismatches(config, state)

		got := state["module.x.aws_instance.web[index:0]"].ModuleResolutionReason
		if got != "external_registry" {
			t.Fatalf("state row ModuleResolutionReason = %q, want %q (single indexed instance must pair)", got, "external_registry")
		}
	})

	t.Run("single-key for_each [key:hash] suffix", func(t *testing.T) {
		t.Parallel()
		config := map[string]*tfconfigstate.ResourceRow{
			"aws_route53_record.this": {
				Address:                "aws_route53_record.this",
				ResourceType:           "aws_route53_record",
				ModuleResolutionReason: "depth_exceeded",
			},
		}
		state := map[string]*tfconfigstate.ResourceRow{
			`module.dns.aws_route53_record.this[key:9f86d081884c7d659a2feaa0c55ad015]`: {
				Address:      `module.dns.aws_route53_record.this[key:9f86d081884c7d659a2feaa0c55ad015]`,
				ResourceType: "aws_route53_record",
			},
		}

		pairSpuriousModuleMismatches(config, state)

		got := state[`module.dns.aws_route53_record.this[key:9f86d081884c7d659a2feaa0c55ad015]`].ModuleResolutionReason
		if got != "depth_exceeded" {
			t.Fatalf("state row ModuleResolutionReason = %q, want %q (single for_each key must pair)", got, "depth_exceeded")
		}
	})
}

// TestLoadDriftEvidencePairsSpuriousMismatchAcrossModuleResolutionFailure is
// the end-to-end regression for issue #5572's follow-up defect: only the
// config-only half of a module-resolution-failure mismatch pair used to
// carry ModuleResolutionReason, so an outcome=exact query still returned the
// state-only half of a known-spurious pair. This drives the same
// registry-heuristic-misclassified-local-module fixture as
// TestLoadDriftEvidenceMarksLowConfidenceForRegistryHeuristicMisclassifiedLocalModule
// but ALSO seeds a state-only row at the real, prefixed address so both
// halves of the mismatch pair are present in one join.
func TestLoadDriftEvidencePairsSpuriousMismatchAcrossModuleResolutionFailure(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a1",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules: one call whose source LOOKS like registry
			//    shorthand but is actually the repo's own local directory.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc", "terraform-aws-modules/vpc/aws", "main.tf"),
			)}}},
			// 2. terraform_resources: a real resource physically declared
			//    under that same directory. The loader emits it at the
			//    fallback ROOT address "aws_instance.web".
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "terraform-aws-modules/vpc/aws/main.tf"),
			)}}},
			// 3. snapshot serial=0.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 0, "gen-state-current")}},
			// 4. state-resource: the REAL resource, correctly prefixed by
			//    Terraform itself at apply time. Different address string
			//    than the config-side fallback, so the two never join.
			{rows: [][]any{fixtureStateResourceRow(
				"module.vpc.aws_instance.web",
				fixtureStatePayload("module.vpc.aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 5. state has an address absent from config (the state-only
			//    row above), so LoadDriftEvidence walks prior-config
			//    addresses too (hasStateOnlyAddress). No prior generations
			//    declare anything relevant here.
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d (config-only fallback + state-only real address)", got, want)
	}

	var configOnly, stateOnly *tfconfigstate.AddressedRow
	for i := range rows {
		switch rows[i].Address {
		case "aws_instance.web":
			configOnly = &rows[i]
		case "module.vpc.aws_instance.web":
			stateOnly = &rows[i]
		}
	}
	if configOnly == nil || configOnly.Config == nil {
		t.Fatalf("missing config-only row at the fallback address; rows = %+v", rows)
	}
	if got, want := configOnly.Config.ModuleResolutionReason, "external_registry"; got != want {
		t.Fatalf("config-only row ModuleResolutionReason = %q, want %q", got, want)
	}
	if stateOnly == nil || stateOnly.State == nil {
		t.Fatalf("missing state-only row at the real address; rows = %+v", rows)
	}
	if got, want := stateOnly.State.ModuleResolutionReason, "external_registry"; got != want {
		t.Fatalf("state-only row ModuleResolutionReason = %q, want %q (the paired state-only half of the spurious mismatch must also be flagged low-confidence)", got, want)
	}
}
