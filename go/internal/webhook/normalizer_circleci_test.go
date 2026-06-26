// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import "testing"

func TestNormalizeCircleCIAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"pipeline": {
			"vcs": {
				"revision": "2222222222222222222222222222222222222222",
				"branch": "main",
				"origin_repository_url": "https://github.com/eshu-hq/eshu",
				"provider_name": "github"
			}
		},
		"project": {"name": "eshu-hq/eshu"}
	}`)

	trigger, err := NormalizeCircleCI("workflow-completed", "delivery-c1", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeCircleCI() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderCircleCI,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-c1",
		RepositoryExternalID: "eshu-hq/eshu",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "2222222222222222222222222222222222222222",
	})
}

func TestNormalizeCircleCIIgnoresNonDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"pipeline": {
			"vcs": {
				"revision": "2222222222222222222222222222222222222222",
				"branch": "feature",
				"origin_repository_url": "https://github.com/eshu-hq/eshu",
				"provider_name": "github"
			}
		},
		"project": {"name": "eshu-hq/eshu"}
	}`)

	trigger, err := NormalizeCircleCI("workflow-completed", "delivery-c2", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeCircleCI() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonNonDefaultBranch {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonNonDefaultBranch)
	}
}

func TestNormalizeCircleCIIgnoresTagRef(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"pipeline": {
			"vcs": {
				"revision": "2222222222222222222222222222222222222222",
				"branch": "v1.0.0",
				"tag": "v1.0.0",
				"origin_repository_url": "https://github.com/eshu-hq/eshu",
				"provider_name": "github"
			}
		},
		"project": {"name": "eshu-hq/eshu"}
	}`)

	trigger, err := NormalizeCircleCI("workflow-completed", "delivery-c3", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeCircleCI() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonTagRef {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonTagRef)
	}
}

func TestNormalizeCircleCIRejectsMalformedPayload(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeCircleCI("workflow-completed", "delivery-c4", []byte(`{`), "main"); err == nil {
		t.Fatal("NormalizeCircleCI() error = nil, want malformed JSON error")
	}
	if _, err := NormalizeCircleCI("workflow-started", "delivery-c5", []byte(`{}`), "main"); err == nil {
		t.Fatal("NormalizeCircleCI() error = nil, want unsupported event error")
	}
}
