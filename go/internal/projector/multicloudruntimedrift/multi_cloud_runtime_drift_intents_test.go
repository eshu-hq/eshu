// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloudruntimedrift

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildMultiCloudRuntimeDriftReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: facts.AWSResourceFactKind,
	}})
	if _, ok := BuildMultiCloudRuntimeDriftReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a multi_cloud_runtime_drift intent for an AWS-only generation")
	}
}

func TestBuildMultiCloudRuntimeDriftReducerIntentEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup(nil)
	if _, ok := BuildMultiCloudRuntimeDriftReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a multi_cloud_runtime_drift intent for a generation with no facts at all")
	}
}

func TestBuildMultiCloudRuntimeDriftReducerIntentFromGCPCandidate(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:  facts.GCPCloudResourceFactKind,
		FactID:    "fact-gcp-1",
		SourceRef: facts.Ref{SourceSystem: "gcp"},
	}})
	intent, ok := BuildMultiCloudRuntimeDriftReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a gcp_cloud_resource fact")
	}
	if intent.Domain != reducer.DomainMultiCloudRuntimeDrift {
		t.Fatalf("intent.Domain = %q, want multi_cloud_runtime_drift", intent.Domain)
	}
	if intent.EntityKey != "multi_cloud_runtime_drift:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.Reason != "gcp or azure cloud resource facts observed" {
		t.Fatalf("intent.Reason = %q", intent.Reason)
	}
	if intent.FactID != "fact-gcp-1" {
		t.Fatalf("intent.FactID = %q, want fact-gcp-1", intent.FactID)
	}
	if intent.SourceSystem != "gcp" {
		t.Fatalf("intent.SourceSystem = %q, want gcp", intent.SourceSystem)
	}
}

func TestBuildMultiCloudRuntimeDriftReducerIntentFromAzureCandidate(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:  facts.AzureCloudResourceFactKind,
		FactID:    "fact-azure-1",
		SourceRef: facts.Ref{SourceSystem: "azure"},
	}})
	intent, ok := BuildMultiCloudRuntimeDriftReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for an azure_cloud_resource fact")
	}
	if intent.FactID != "fact-azure-1" {
		t.Fatalf("intent.FactID = %q, want fact-azure-1", intent.FactID)
	}
	if intent.SourceSystem != "azure" {
		t.Fatalf("intent.SourceSystem = %q, want azure", intent.SourceSystem)
	}
}

// TestBuildMultiCloudRuntimeDriftReducerIntentEarliestAcrossKinds proves the
// candidateFactKinds order is not priority: FirstAcrossKinds walks original
// generation order, so an azure_cloud_resource fact earlier in the
// generation wins over a later gcp_cloud_resource fact even though GCP is
// listed first in candidateFactKinds.
func TestBuildMultiCloudRuntimeDriftReducerIntentEarliestAcrossKinds(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.AzureCloudResourceFactKind, FactID: "fact-azure-first"},
		{FactKind: facts.GCPCloudResourceFactKind, FactID: "fact-gcp-second"},
	})
	intent, ok := BuildMultiCloudRuntimeDriftReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued")
	}
	if intent.FactID != "fact-azure-first" {
		t.Fatalf("intent.FactID = %q, want fact-azure-first (earliest across kinds, not GCP priority)", intent.FactID)
	}
}

// TestBuildMultiCloudRuntimeDriftReducerIntentSourceSystemFallsBackToCollectorKind
// pins the shared two-tier projectorintent.SourceSystem label this family
// uses verbatim: SourceRef.SourceSystem wins when set, else the trimmed
// CollectorKind.
func TestBuildMultiCloudRuntimeDriftReducerIntentSourceSystemFallsBackToCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.GCPCloudResourceFactKind,
		FactID:        "fact-gcp-2",
		CollectorKind: "  gcp_cloud  ",
	}})
	intent, ok := BuildMultiCloudRuntimeDriftReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a gcp_cloud_resource fact")
	}
	if intent.SourceSystem != "gcp_cloud" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind fallback", intent.SourceSystem)
	}
}

// TestBuildMultiCloudRuntimeDriftReducerIntentSourceSystemPrefersSourceRef
// pins the tier ORDER, which the fallback test above cannot: it sets
// SourceRef.SourceSystem and CollectorKind to DIFFERENT values, so a
// regression that swapped the two tiers would change the result. A test
// where both tiers carry the same value passes either way and proves only
// that a label was produced.
func TestBuildMultiCloudRuntimeDriftReducerIntentSourceSystemPrefersSourceRef(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.AzureCloudResourceFactKind,
		FactID:        "fact-azure-2",
		CollectorKind: "azure_scanner",
		SourceRef:     facts.Ref{SourceSystem: "  azure_live  "},
	}})
	intent, ok := BuildMultiCloudRuntimeDriftReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for an azure_cloud_resource fact")
	}
	if intent.SourceSystem != "azure_live" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed SourceRef.SourceSystem to win over CollectorKind %q",
			intent.SourceSystem, "azure_scanner")
	}
}
