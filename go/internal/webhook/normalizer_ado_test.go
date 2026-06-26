// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"testing"
)

func TestNormalizeAzureDevOpsPushAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"resource": {
			"refUpdates": [{
				"name": "refs/heads/main",
				"oldObjectId": "1111111111111111111111111111111111111111",
				"newObjectId": "2222222222222222222222222222222222222222"
			}],
			"repository": {
				"id": "repo-guid-here",
				"name": "eshu-hq/eshu",
				"defaultBranch": "refs/heads/main"
			},
			"pushedBy": {"displayName": "linuxdynasty"}
		}
	}`)

	trigger, err := NormalizeAzureDevOps("git.push", "delivery-ado-1", payload, "")
	if err != nil {
		t.Fatalf("NormalizeAzureDevOps() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderAzureDevOps,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-ado-1",
		RepositoryExternalID: "repo-guid-here",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		BeforeSHA:            "1111111111111111111111111111111111111111",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeAzureDevOpsPullRequestMergedAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"resource": {
			"status": "completed",
			"mergeStatus": "succeeded",
			"targetRefName": "refs/heads/main",
			"lastMergeCommit": {"commitId": "3333333333333333333333333333333333333333"},
			"repository": {
				"id": "repo-guid-here",
				"name": "eshu-hq/eshu",
				"defaultBranch": "refs/heads/main"
			},
			"createdBy": {"displayName": "linuxdynasty"},
			"pullRequestId": 42
		}
	}`)

	trigger, err := NormalizeAzureDevOps("git.pullrequest.updated", "delivery-ado-2", payload, "")
	if err != nil {
		t.Fatalf("NormalizeAzureDevOps() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderAzureDevOps,
		EventKind:            EventKindPullRequestMerged,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-ado-2",
		RepositoryExternalID: "repo-guid-here",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "3333333333333333333333333333333333333333",
		Action:               "completed",
		Sender:               "linuxdynasty",
		PullRequestNumber:    "42",
	})
}

func TestNormalizeAzureDevOpsIgnoresNonDefaultRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ref    string
		reason DecisionReason
	}{
		{name: "feature branch", ref: "refs/heads/feature", reason: ReasonNonDefaultBranch},
		{name: "tag", ref: "refs/tags/v1.0.0", reason: ReasonTagRef},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := []byte(`{
				"resource": {
					"refUpdates": [{
						"name": "` + tt.ref + `",
						"newObjectId": "2222222222222222222222222222222222222222"
					}],
					"repository": {
						"id": "repo-guid-here",
						"name": "eshu-hq/eshu",
						"defaultBranch": "refs/heads/main"
					}
				}
			}`)

			trigger, err := NormalizeAzureDevOps("git.push", "delivery-ado-3", payload, "")
			if err != nil {
				t.Fatalf("NormalizeAzureDevOps() error = %v, want nil", err)
			}
			if trigger.Decision != DecisionIgnored {
				t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
			}
			if trigger.Reason != tt.reason {
				t.Fatalf("Reason = %q, want %q", trigger.Reason, tt.reason)
			}
		})
	}
}

func TestNormalizeAzureDevOpsPullRequestIgnoresUnmerged(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"resource": {
			"status": "active",
			"mergeStatus": "succeeded",
			"targetRefName": "refs/heads/main",
			"repository": {
				"id": "repo-guid-here",
				"name": "eshu-hq/eshu",
				"defaultBranch": "refs/heads/main"
			},
			"createdBy": {"displayName": "linuxdynasty"}
		}
	}`)

	trigger, err := NormalizeAzureDevOps("git.pullrequest.updated", "delivery-ado-4", payload, "")
	if err != nil {
		t.Fatalf("NormalizeAzureDevOps() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonPullRequestNotMerged {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonPullRequestNotMerged)
	}
}

func TestNormalizeAzureDevOpsPullRequestRequiresMergeCommit(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"resource": {
			"status": "completed",
			"mergeStatus": "succeeded",
			"targetRefName": "refs/heads/main",
			"repository": {
				"id": "repo-guid-here",
				"name": "eshu-hq/eshu",
				"defaultBranch": "refs/heads/main"
			},
			"createdBy": {"displayName": "linuxdynasty"}
		}
	}`)

	trigger, err := NormalizeAzureDevOps("git.pullrequest.updated", "delivery-ado-5", payload, "")
	if err != nil {
		t.Fatalf("NormalizeAzureDevOps() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonMissingMergeCommit {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonMissingMergeCommit)
	}
}

func TestNormalizeAzureDevOpsRejectsUnsupportedEvent(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeAzureDevOps("workitem.updated", "delivery-ado-6", []byte(`{}`), ""); err == nil {
		t.Fatal("NormalizeAzureDevOps() error = nil, want unsupported event error")
	}
}
