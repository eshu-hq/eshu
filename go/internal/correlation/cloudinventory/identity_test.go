// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudinventory

import "testing"

func TestResolveProviderIdentityKnownProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		raw      string
		wantKind ResolutionOutcome
	}{
		{
			name:     "aws_arn",
			provider: ProviderAWS,
			raw:      "arn:aws:lambda:us-east-1:123456789012:function:payments",
			wantKind: ResolutionOutcomeAdmitted,
		},
		{
			name:     "gcp_cai_full_resource_name",
			provider: ProviderGCP,
			raw:      "//compute.googleapis.com/projects/eshu-prod/zones/us-central1-a/instances/api-1",
			wantKind: ResolutionOutcomeAdmitted,
		},
		{
			name:     "azure_arm_resource_id",
			provider: ProviderAzure,
			raw:      "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1",
			wantKind: ResolutionOutcomeAdmitted,
		},
	}

	uids := map[string]struct{}{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := ResolveProviderIdentity(tc.provider, tc.raw)
			if res.Outcome != tc.wantKind {
				t.Fatalf("ResolveProviderIdentity(%q,...).Outcome = %q, want %q", tc.provider, res.Outcome, tc.wantKind)
			}
			if res.CloudResourceUID == "" {
				t.Fatalf("ResolveProviderIdentity(%q,...) returned empty uid", tc.provider)
			}
			if _, dup := uids[res.CloudResourceUID]; dup {
				t.Fatalf("ResolveProviderIdentity produced a colliding uid %q across providers", res.CloudResourceUID)
			}
			uids[res.CloudResourceUID] = struct{}{}
		})
	}
}

func TestResolveProviderIdentityIsDeterministicAndStable(t *testing.T) {
	t.Parallel()

	const raw = "//storage.googleapis.com/projects/_/buckets/eshu-artifacts"
	first := ResolveProviderIdentity(ProviderGCP, raw)
	second := ResolveProviderIdentity(ProviderGCP, "  "+raw+"  ")
	if first.CloudResourceUID != second.CloudResourceUID {
		t.Fatalf("uid not stable under surrounding whitespace: %q vs %q", first.CloudResourceUID, second.CloudResourceUID)
	}
	if first.Outcome != ResolutionOutcomeAdmitted || second.Outcome != ResolutionOutcomeAdmitted {
		t.Fatalf("expected admitted outcomes, got %q and %q", first.Outcome, second.Outcome)
	}
}

func TestResolveProviderIdentityUnsupportedProviderCounted(t *testing.T) {
	t.Parallel()

	res := ResolveProviderIdentity("oraclecloud", "ocid1.instance.oc1..abc")
	if res.Outcome != ResolutionOutcomeUnsupported {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, ResolutionOutcomeUnsupported)
	}
	if res.CloudResourceUID != "" {
		t.Fatalf("unsupported provider must not fabricate a uid, got %q", res.CloudResourceUID)
	}
}

func TestResolveProviderIdentityBlankRawIsUnresolved(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "   ", "\t"} {
		res := ResolveProviderIdentity(ProviderAWS, raw)
		if res.Outcome != ResolutionOutcomeUnresolved {
			t.Fatalf("ResolveProviderIdentity(blank raw=%q).Outcome = %q, want %q", raw, res.Outcome, ResolutionOutcomeUnresolved)
		}
		if res.CloudResourceUID != "" {
			t.Fatalf("blank raw must not fabricate a uid, got %q", res.CloudResourceUID)
		}
	}
}

func TestResolveProviderIdentityMalformedProviderIDIsAmbiguous(t *testing.T) {
	t.Parallel()

	// An ARM id missing the /subscriptions/ prefix cannot be safely keyed.
	res := ResolveProviderIdentity(ProviderAzure, "resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1")
	if res.Outcome != ResolutionOutcomeAmbiguous {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, ResolutionOutcomeAmbiguous)
	}
	if res.CloudResourceUID != "" {
		t.Fatalf("ambiguous identity must not fabricate a uid, got %q", res.CloudResourceUID)
	}
}

func TestNormalizeProviderTrimsAndLowercases(t *testing.T) {
	t.Parallel()

	if got := NormalizeProvider("  GCP "); got != ProviderGCP {
		t.Fatalf("NormalizeProvider = %q, want %q", got, ProviderGCP)
	}
}
