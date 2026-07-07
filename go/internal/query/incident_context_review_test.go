// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildIncidentReviewWorkItemEvidenceAddsExactPullRequest(t *testing.T) {
	t.Parallel()

	got := buildIncidentReviewWorkItemEvidence(incidentReviewWorkItemInput{
		CommitSHA: "3333333333333333333333333333333333333333",
		PullRequests: []incidentPullRequestEvidence{
			{
				TriggerID:          "trigger-1",
				Provider:           "github",
				RepositoryFullName: "eshu-hq/eshu",
				CommitSHA:          "3333333333333333333333333333333333333333",
				Number:             "42",
				URL:                "https://github.com/eshu-hq/eshu/pull/42",
				Title:              "INC-123 fix checkout deploy",
			},
		},
	})

	assertIncidentEdge(t, got, IncidentSlotPullRequest, IncidentTruthExact)
	assertIncidentNoEdge(t, got, IncidentSlotWorkItem)
}

func TestBuildIncidentReviewWorkItemEvidenceDoesNotDeriveWorkItemFromAmbiguousProviderPullRequest(t *testing.T) {
	t.Parallel()

	got := buildIncidentReviewWorkItemEvidence(incidentReviewWorkItemInput{
		CommitSHA: "3333333333333333333333333333333333333333",
		PullRequests: []incidentPullRequestEvidence{
			{
				TriggerID: "trigger-1",
				Provider:  "github",
				CommitSHA: "3333333333333333333333333333333333333333",
				Number:    "42",
				URL:       "https://github.com/eshu-hq/eshu/pull/42",
			},
			{
				TriggerID: "trigger-2",
				Provider:  "github",
				CommitSHA: "3333333333333333333333333333333333333333",
				Number:    "43",
				URL:       "https://github.com/eshu-hq/eshu/pull/43",
			},
		},
	})

	assertIncidentEdge(t, got, IncidentSlotPullRequest, IncidentTruthAmbiguous)
	assertIncidentNoEdge(t, got, IncidentSlotWorkItem)
}

func TestBuildIncidentReviewWorkItemEvidenceAddsIssueKeyDerivedWorkItem(t *testing.T) {
	t.Parallel()

	got := buildIncidentReviewWorkItemEvidence(incidentReviewWorkItemInput{
		CommitSHA: "3333333333333333333333333333333333333333",
		PullRequests: []incidentPullRequestEvidence{
			{
				TriggerID:          "trigger-1",
				Provider:           "github",
				RepositoryFullName: "eshu-hq/eshu",
				CommitSHA:          "3333333333333333333333333333333333333333",
				Number:             "42",
				URL:                "https://github.com/eshu-hq/eshu/pull/42",
				Title:              "INC-123 fix checkout deploy",
			},
		},
		WorkItems: []incidentWorkItemRecord{
			{
				FactID:      "jira-record",
				Provider:    "jira_cloud",
				WorkItemID:  "10001",
				WorkItemKey: "INC-123",
				Summary:     "Fix checkout deploy",
				StatusID:    "3",
				StatusName:  "Done",
				ProjectID:   "10000",
				ProjectKey:  "INC",
				SourceURL:   "https://example.atlassian.net/browse/INC-123",
			},
		},
		ProjectMetadata: []incidentWorkItemProjectMetadata{
			{FactID: "jira-project", Provider: "jira_cloud", ProjectID: "10000", ProjectKey: "INC", VisibilityState: "active"},
		},
		StatusMetadata: []incidentWorkItemStatusMetadata{
			{FactID: "jira-status", Provider: "jira_cloud", StatusID: "3", StatusCategory: "DONE", StatusCategoryKey: "done"},
		},
	})

	assertIncidentEdge(t, got, IncidentSlotPullRequest, IncidentTruthExact)
	edge := assertIncidentEdge(t, got, IncidentSlotWorkItem, IncidentTruthDerived)
	if got := edge.Value["project_key"]; got != "INC" {
		t.Fatalf("project_key = %q, want INC", got)
	}
	if got := edge.Value["project_visibility_state"]; got != "active" {
		t.Fatalf("project_visibility_state = %q, want active", got)
	}
	if got := edge.Value["status_category"]; got != "DONE" {
		t.Fatalf("status_category = %q, want DONE", got)
	}
}

func TestBuildIncidentReviewWorkItemEvidenceKeepsMissingWorkItemImplicit(t *testing.T) {
	t.Parallel()

	got := buildIncidentReviewWorkItemEvidence(incidentReviewWorkItemInput{
		CommitSHA: "3333333333333333333333333333333333333333",
		PullRequests: []incidentPullRequestEvidence{
			{
				TriggerID:          "trigger-1",
				Provider:           "github",
				RepositoryFullName: "eshu-hq/eshu",
				CommitSHA:          "3333333333333333333333333333333333333333",
				Number:             "42",
				URL:                "https://github.com/eshu-hq/eshu/pull/42",
			},
		},
	})

	assertIncidentEdge(t, got, IncidentSlotPullRequest, IncidentTruthExact)
	assertIncidentNoEdge(t, got, IncidentSlotWorkItem)
}

func assertIncidentNoEdge(
	t *testing.T,
	edges []IncidentContextEvidenceEdge,
	slot IncidentEvidenceSlot,
) {
	t.Helper()
	if edge := findIncidentEdge(edges, slot); edge != nil {
		t.Fatalf("edge %s = %#v, want no edge", slot, *edge)
	}
}
