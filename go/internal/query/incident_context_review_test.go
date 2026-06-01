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

func TestBuildIncidentReviewWorkItemEvidenceDoesNotVerifyJiraOnlyPullRequestURL(t *testing.T) {
	t.Parallel()

	got := buildIncidentReviewWorkItemEvidence(incidentReviewWorkItemInput{
		CommitSHA: "3333333333333333333333333333333333333333",
		WorkItemLinks: []incidentWorkItemExternalLink{
			{
				FactID:      "jira-link",
				Provider:    "jira_cloud",
				WorkItemKey: "INC-123",
				URL:         "https://github.com/eshu-hq/eshu/pull/42",
				Title:       "INC-123 fix checkout deploy",
				AnchorClass: "github_pull_request",
			},
		},
	})

	assertIncidentNoEdge(t, got, IncidentSlotPullRequest)
	assertIncidentEdge(t, got, IncidentSlotWorkItem, IncidentTruthDerived)
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
		WorkItemLinks: []incidentWorkItemExternalLink{
			{
				FactID:      "jira-link",
				Provider:    "jira_cloud",
				WorkItemKey: "INC-123",
				URL:         "https://github.com/eshu-hq/eshu/pull/42",
				Title:       "INC-123 fix checkout deploy",
				AnchorClass: "github_pull_request",
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
				StatusName:  "Done",
				SourceURL:   "https://example.atlassian.net/browse/INC-123",
			},
		},
	})

	assertIncidentEdge(t, got, IncidentSlotPullRequest, IncidentTruthExact)
	assertIncidentEdge(t, got, IncidentSlotWorkItem, IncidentTruthDerived)
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
