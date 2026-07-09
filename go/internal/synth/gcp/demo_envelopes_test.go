// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestDemoOrgFactEnvelopesRepliesThroughProductionSeam proves
// DemoOrgFactEnvelopes returns the generated demo-org cassette's facts by
// driving the same cassette.Source replay seam collector.Service uses in
// production, not a hand-built mirror: every fact kind the generator emits
// must be present, envelope identity must be consistent (single scope,
// non-empty generation id), and each kind's population count must match the
// generator's own fixed proportions (resources.go/relationships.go/
// collection_warnings.go/dns_records.go/iam_policy_observations.go) exactly,
// so a change to those proportions would fail this test rather than silently
// drift.
func TestDemoOrgFactEnvelopesRepliesThroughProductionSeam(t *testing.T) {
	profile := DefaultDemoOrgProfile()
	envelopes, err := DemoOrgFactEnvelopes(profile)
	if err != nil {
		t.Fatalf("DemoOrgFactEnvelopes: %v", err)
	}
	if len(envelopes) == 0 {
		t.Fatal("DemoOrgFactEnvelopes returned zero envelopes")
	}

	wantScopeID := "gcp:project:acme-demo-gcp:seed:4592"
	counts := map[string]int{}
	seenKeys := make(map[string]struct{}, len(envelopes))
	for _, env := range envelopes {
		if env.ScopeID != wantScopeID {
			t.Fatalf("envelope scope_id = %q, want %q", env.ScopeID, wantScopeID)
		}
		if env.GenerationID == "" {
			t.Fatal("envelope generation_id is empty")
		}
		if env.StableFactKey == "" {
			t.Fatal("envelope stable_fact_key is empty")
		}
		if _, dup := seenKeys[env.StableFactKey]; dup {
			t.Fatalf("duplicate stable_fact_key %q", env.StableFactKey)
		}
		seenKeys[env.StableFactKey] = struct{}{}
		counts[env.FactKind]++
	}

	want := map[string]int{
		factschema.FactKindGCPCloudResource:        profile.ResourceCount,
		factschema.FactKindGCPCloudRelationship:    profile.ResourceCount - 1,
		factschema.FactKindGCPCollectionWarning:    10,
		factschema.FactKindGCPDNSRecord:            6,
		factschema.FactKindGCPIAMPolicyObservation: 13,
	}
	for kind, wantCount := range want {
		if got := counts[kind]; got != wantCount {
			t.Errorf("counts[%q] = %d, want %d", kind, got, wantCount)
		}
	}

	total := 0
	for _, c := range counts {
		total += c
	}
	if total != len(envelopes) {
		t.Fatalf("sum of per-kind counts = %d, want %d (every envelope must be one of the known kinds)", total, len(envelopes))
	}

	// Every gcp_cloud_resource fact must decode against the fixturepack schema
	// via the real typed decode seam, proving DemoOrgFactEnvelopes' payloads are
	// genuinely schema-valid, not just structurally present.
	decodedResources := 0
	for _, env := range envelopes {
		if env.FactKind != factschema.FactKindGCPCloudResource {
			continue
		}
		fsEnv := factschema.Envelope{
			FactKind:      env.FactKind,
			SchemaVersion: env.SchemaVersion,
			Payload:       env.Payload,
		}
		if _, err := factschema.DecodeGCPCloudResource(fsEnv); err != nil {
			t.Fatalf("DecodeGCPCloudResource(%s): %v", env.StableFactKey, err)
		}
		decodedResources++
	}
	if decodedResources != profile.ResourceCount {
		t.Fatalf("decoded %d gcp_cloud_resource facts, want %d", decodedResources, profile.ResourceCount)
	}
}

// TestDemoOrgFactEnvelopesRejectsInvalidProfile proves DemoOrgFactEnvelopes
// fails closed (propagates the profile validation error) rather than
// generating a degenerate cassette when the profile itself is invalid.
func TestDemoOrgFactEnvelopesRejectsInvalidProfile(t *testing.T) {
	profile := DefaultDemoOrgProfile()
	profile.ProjectID = ""
	if _, err := DemoOrgFactEnvelopes(profile); err == nil {
		t.Fatal("DemoOrgFactEnvelopes with a blank ProjectID = nil error, want a validation error")
	}
}
