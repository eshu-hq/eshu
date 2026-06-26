// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"testing"
)

func TestNormalizeJenkinsPushAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"ref": "refs/heads/main",
		"before": "1111111111111111111111111111111111111111",
		"after": "2222222222222222222222222222222222222222",
		"repository": {
			"id": 42,
			"full_name": "eshu-hq/eshu"
		},
		"pusher": {"name": "linuxdynasty"}
	}`)

	trigger, err := NormalizeJenkins("push", "delivery-jenkins-1", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeJenkins() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderJenkins,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-jenkins-1",
		RepositoryExternalID: "42",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		BeforeSHA:            "1111111111111111111111111111111111111111",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeJenkinsPushAcceptsDefaultBranchWithFallbackFields(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"GIT_BRANCH": "main",
		"GIT_COMMIT": "2222222222222222222222222222222222222222",
		"repository": {
			"full_name": "eshu-hq/eshu"
		},
		"GIT_AUTHOR": "linuxdynasty"
	}`)

	trigger, err := NormalizeJenkins("push", "delivery-jenkins-2", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeJenkins() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderJenkins,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-jenkins-2",
		RepositoryExternalID: "eshu-hq/eshu",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeJenkinsIgnoresNonDefaultRefs(t *testing.T) {
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
				"ref": "` + tt.ref + `",
				"after": "2222222222222222222222222222222222222222",
				"repository": {
					"full_name": "eshu-hq/eshu"
				}
			}`)

			trigger, err := NormalizeJenkins("push", "delivery-jenkins-3", payload, "main")
			if err != nil {
				t.Fatalf("NormalizeJenkins() error = %v, want nil", err)
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

func TestNormalizeJenkinsRejectsUnsupportedEvent(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeJenkins("build", "delivery-jenkins-4", []byte(`{}`), ""); err == nil {
		t.Fatal("NormalizeJenkins() error = nil, want unsupported event error")
	}
}

func TestNormalizeJenkinsPushAcceptsDefaultBranchFromMergeEvent(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"ref": "refs/heads/main",
		"after": "3333333333333333333333333333333333333333",
		"repository": {
			"id": 42,
			"full_name": "eshu-hq/eshu"
		},
		"pusher": {"name": "linuxdynasty"}
	}`)

	trigger, err := NormalizeJenkins("merge", "delivery-jenkins-5", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeJenkins() error = %v, want nil", err)
	}

	if trigger.Decision != DecisionAccepted {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionAccepted)
	}
}

func TestNormalizeJenkinsUsesFallbackRepoIDFromFullName(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"ref": "refs/heads/main",
		"after": "2222222222222222222222222222222222222222",
		"repository": {
			"full_name": "eshu-hq/eshu"
		}
	}`)

	trigger, err := NormalizeJenkins("push", "delivery-jenkins-6", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeJenkins() error = %v, want nil", err)
	}
	if trigger.RepositoryExternalID != "eshu-hq/eshu" {
		t.Fatalf("RepositoryExternalID = %q, want %q", trigger.RepositoryExternalID, "eshu-hq/eshu")
	}
}
