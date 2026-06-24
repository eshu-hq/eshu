// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildIncidentContextResponseKeepsPagerDutyOnlyContextUseful(t *testing.T) {
	t.Parallel()

	snapshot := IncidentContextSnapshot{
		Query: IncidentContextQuery{
			Provider:           "pagerduty",
			ProviderIncidentID: "PABC123",
			ScopeID:            "pagerduty-prod",
			Limit:              10,
		},
		Incident: IncidentContextIncident{
			Provider:           "pagerduty",
			ProviderIncidentID: "PABC123",
			Title:              "Checkout elevated error rate",
			Status:             "triggered",
			Service: IncidentContextReference{
				ID:      "P-SVC",
				Type:    "service",
				Summary: "checkout-api",
				URL:     "https://example.pagerduty.com/service/P-SVC",
			},
			SourceURL:      "https://example.pagerduty.com/incidents/PABC123",
			EvidenceFactID: "incident-fact",
		},
		Timeline: []IncidentContextTimelineEvent{
			{
				EventID:        "event-1",
				EventType:      "triggered",
				Summary:        "Incident triggered",
				CreatedAt:      "2026-05-31T12:00:00Z",
				EvidenceFactID: "event-fact",
			},
		},
		RelatedChanges: []IncidentContextChangeCandidate{
			{
				ChangeID:       "change-1",
				Summary:        "checkout-api deploy",
				Timestamp:      "2026-05-31T11:45:00Z",
				TruthLabel:     IncidentTruthFallback,
				Explanation:    "candidate matched PagerDuty service and incident time window",
				EvidenceFactID: "change-fact",
			},
		},
	}

	got := BuildIncidentContextResponse(snapshot)
	if got.Incident.ProviderIncidentID != "PABC123" {
		t.Fatalf("incident id = %q, want PABC123", got.Incident.ProviderIncidentID)
	}
	if len(got.Timeline) != 1 {
		t.Fatalf("timeline len = %d, want 1", len(got.Timeline))
	}
	if len(got.RelatedChanges) != 1 || got.RelatedChanges[0].TruthLabel != IncidentTruthFallback {
		t.Fatalf("related changes = %#v, want one fallback candidate", got.RelatedChanges)
	}

	assertIncidentEdge(t, got.EvidencePath, IncidentSlotIncident, IncidentTruthExact)
	assertIncidentEdge(t, got.EvidencePath, IncidentSlotService, IncidentTruthExact)
	for _, slot := range []IncidentEvidenceSlot{
		IncidentSlotDeployable,
		IncidentSlotRuntimeArtifact,
		IncidentSlotImage,
		IncidentSlotBuildDeploy,
		IncidentSlotCommit,
		IncidentSlotPullRequest,
		IncidentSlotWorkItem,
	} {
		assertIncidentEdge(t, got.EvidencePath, slot, IncidentTruthMissing)
		assertIncidentMissing(t, got.MissingEvidence, slot)
	}
}

func TestBuildIncidentContextResponsePreservesDerivedAndAmbiguousEvidence(t *testing.T) {
	t.Parallel()

	snapshot := IncidentContextSnapshot{
		Query: IncidentContextQuery{
			Provider:           "pagerduty",
			ProviderIncidentID: "PABC123",
			Limit:              10,
		},
		Incident: IncidentContextIncident{
			Provider:           "pagerduty",
			ProviderIncidentID: "PABC123",
			EvidenceFactID:     "incident-fact",
		},
		EvidencePath: []IncidentContextEvidenceEdge{
			{
				Slot:        IncidentSlotCommit,
				TruthLabel:  IncidentTruthDerived,
				Explanation: "commit derived from deployment image provenance",
				Value:       map[string]string{"commit_sha": "abc123"},
				Evidence: []IncidentContextEvidenceRef{
					{FactID: "deploy-fact", Source: "ci_cd", Kind: "deployment"},
				},
			},
			{
				Slot:        IncidentSlotPullRequest,
				TruthLabel:  IncidentTruthAmbiguous,
				Explanation: "two pull requests reference the same Jira issue",
				Candidates: []IncidentContextEvidenceCandidate{
					{ID: "pr-1", Label: "PR #1"},
					{ID: "pr-2", Label: "PR #2"},
				},
			},
		},
	}

	got := BuildIncidentContextResponse(snapshot)
	assertIncidentEdge(t, got.EvidencePath, IncidentSlotCommit, IncidentTruthDerived)
	assertIncidentEdge(t, got.EvidencePath, IncidentSlotPullRequest, IncidentTruthAmbiguous)
	if len(got.AmbiguousEvidence) != 1 {
		t.Fatalf("ambiguous evidence len = %d, want 1", len(got.AmbiguousEvidence))
	}
	if got.AmbiguousEvidence[0].Slot != IncidentSlotPullRequest {
		t.Fatalf("ambiguous slot = %q, want pull_request", got.AmbiguousEvidence[0].Slot)
	}
	assertIncidentMissing(t, got.MissingEvidence, IncidentSlotWorkItem)
}

func assertIncidentEdge(
	t *testing.T,
	edges []IncidentContextEvidenceEdge,
	slot IncidentEvidenceSlot,
	label IncidentTruthLabel,
) *IncidentContextEvidenceEdge {
	t.Helper()
	for _, edge := range edges {
		if edge.Slot == slot {
			if edge.TruthLabel != label {
				t.Fatalf("edge %s truth_label = %q, want %q", slot, edge.TruthLabel, label)
			}
			return &edge
		}
	}
	t.Fatalf("missing edge for slot %s in %#v", slot, edges)
	return nil
}

func assertIncidentMissing(
	t *testing.T,
	missing []IncidentMissingEvidence,
	slot IncidentEvidenceSlot,
) {
	t.Helper()
	for _, item := range missing {
		if item.Slot == slot {
			if item.Reason == "" {
				t.Fatalf("missing slot %s reason is blank", slot)
			}
			return
		}
	}
	t.Fatalf("missing evidence does not include slot %s: %#v", slot, missing)
}
