// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// TestResourceAddressKeyStripsModulePrefixes proves resourceAddressKey
// recovers the trailing "TYPE.NAME[INDEX]" segment of a canonical Terraform
// address by FRONT-stripping leading "module.<name>[<index>]." pairs, not by
// taking the last two dot-separated segments (that end-taking shape was the
// bug: reviewer-supplied empirical proof showed
// `aws_route53_record.this["api.example.com"]` and
// `aws_acm_certificate.cert["www.example.com"]` collapsing to the identical,
// wrong key "example.com\"]", and `data.aws_ami.ubuntu` colliding with the
// unrelated managed resource `aws_ami.ubuntu`). Front-stripping instead
// walks from the left, so a dot or "]" INSIDE a quoted for_each key, or
// inside an indexed MODULE name's own index, is never mistaken for a
// segment boundary.
func TestResourceAddressKeyStripsModulePrefixes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		address string
		want    string
		wantOK  bool
	}{
		{
			name:    "plain root resource",
			address: "aws_security_group.vpc_endpoints",
			want:    "aws_security_group.vpc_endpoints",
			wantOK:  true,
		},
		{
			name:    "single module prefix",
			address: "module.vpc.aws_security_group.vpc_endpoints",
			want:    "aws_security_group.vpc_endpoints",
			wantOK:  true,
		},
		{
			name:    "nested module prefix",
			address: "module.platform.module.vpc.aws_instance.web",
			want:    "aws_instance.web",
			wantOK:  true,
		},
		{
			name:    "numeric count index, no dots",
			address: "aws_instance.web[0]",
			want:    "aws_instance.web[0]",
			wantOK:  true,
		},
		{
			name:    "for_each key containing dots, no module prefix",
			address: `aws_route53_record.this["api.example.com"]`,
			want:    `aws_route53_record.this["api.example.com"]`,
			wantOK:  true,
		},
		{
			name:    "for_each key containing dots, with module prefix",
			address: `module.dns.aws_route53_record.this["api.example.com"]`,
			want:    `aws_route53_record.this["api.example.com"]`,
			wantOK:  true,
		},
		{
			name:    "different resource, same dotted-index tail, must not collapse",
			address: `aws_acm_certificate.cert["www.example.com"]`,
			want:    `aws_acm_certificate.cert["www.example.com"]`,
			wantOK:  true,
		},
		{
			name:    "data source keeps its data. token, distinct from a managed resource",
			address: "data.aws_ami.ubuntu",
			want:    "data.aws_ami.ubuntu",
			wantOK:  true,
		},
		{
			name:    "managed resource sharing a data source's type and name",
			address: "aws_ami.ubuntu",
			want:    "aws_ami.ubuntu",
			wantOK:  true,
		},
		{
			name:    "indexed module name whose own index contains a dot",
			address: `module.vpc["a.b"].aws_security_group.x`,
			want:    "aws_security_group.x",
			wantOK:  true,
		},
		{
			name:    "empty address",
			address: "",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "single segment, no type.name shape",
			address: "aws_instance",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "module name consumes the whole address, no trailing resource",
			address: "module.vpc",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "unterminated bracket refuses to guess",
			address: `module.vpc["unterminated.aws_instance.web`,
			want:    "",
			wantOK:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resourceAddressKey(tc.address)
			if ok != tc.wantOK {
				t.Fatalf("resourceAddressKey(%q) ok = %v, want %v", tc.address, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("resourceAddressKey(%q) = %q, want %q", tc.address, got, tc.want)
			}
		})
	}

	// Explicit non-collision assertions mirroring the reviewer's empirical
	// proof of the old end-taking bug: these two pairs MUST resolve to
	// different keys, or pairSpuriousModuleMismatches's "unambiguous 1:1"
	// guard would see a false-unambiguous match and mirror a
	// ModuleResolutionReason onto a genuinely unrelated resource.
	t.Run("dotted for_each keys of different resources never collide", func(t *testing.T) {
		t.Parallel()
		key1, ok1 := resourceAddressKey(`aws_route53_record.this["api.example.com"]`)
		key2, ok2 := resourceAddressKey(`aws_acm_certificate.cert["www.example.com"]`)
		if !ok1 || !ok2 {
			t.Fatalf("resourceAddressKey() ok = (%v, %v), want (true, true)", ok1, ok2)
		}
		if key1 == key2 {
			t.Fatalf("collision: both resolved to %q", key1)
		}
	})
	t.Run("data source never collides with a managed resource of the same type and name", func(t *testing.T) {
		t.Parallel()
		key1, ok1 := resourceAddressKey("data.aws_ami.ubuntu")
		key2, ok2 := resourceAddressKey("aws_ami.ubuntu")
		if !ok1 || !ok2 {
			t.Fatalf("resourceAddressKey() ok = (%v, %v), want (true, true)", ok1, ok2)
		}
		if key1 == key2 {
			t.Fatalf("collision: both resolved to %q", key1)
		}
	})
}

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
