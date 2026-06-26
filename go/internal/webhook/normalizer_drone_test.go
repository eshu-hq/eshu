// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import "testing"

func TestNormalizeDronePushAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"build": {
			"after": "2222222222222222222222222222222222222222",
			"before": "1111111111111111111111111111111111111111",
			"branch": "main",
			"source": "main",
			"target": "main",
			"link": "https://drone.io/eshu-hq/eshu/42",
			"number": 42,
			"event": "push"
		},
		"repo": {"slug": "eshu-hq/eshu"},
		"sender": {"login": "linuxdynasty"}
	}`)

	trigger, err := NormalizeDrone("build.success", "delivery-d1", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeDrone() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderDrone,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-d1",
		RepositoryExternalID: "eshu-hq/eshu",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeDronePullRequestAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"build": {
			"after": "3333333333333333333333333333333333333333",
			"before": "1111111111111111111111111111111111111111",
			"branch": "feature",
			"source": "feature",
			"target": "main",
			"link": "https://drone.io/eshu-hq/eshu/43",
			"number": 43,
			"event": "pull_request"
		},
		"repo": {"slug": "eshu-hq/eshu"},
		"sender": {"login": "linuxdynasty"}
	}`)

	trigger, err := NormalizeDrone("build.success", "delivery-d2", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeDrone() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderDrone,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-d2",
		RepositoryExternalID: "eshu-hq/eshu",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "3333333333333333333333333333333333333333",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeDroneIgnoresNonDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"build": {
			"after": "2222222222222222222222222222222222222222",
			"branch": "feature",
			"event": "push"
		},
		"repo": {"slug": "eshu-hq/eshu"}
	}`)

	trigger, err := NormalizeDrone("build.success", "delivery-d3", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeDrone() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonNonDefaultBranch {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonNonDefaultBranch)
	}
}

func TestNormalizeDroneRejectsUnsupportedEvent(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeDrone("build.created", "delivery-d4", []byte(`{}`), "main"); err == nil {
		t.Fatal("NormalizeDrone() error = nil, want unsupported event error")
	}
}

func TestNormalizeDroneRejectsMalformedPayload(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeDrone("build.success", "delivery-d5", []byte(`{`), "main"); err == nil {
		t.Fatal("NormalizeDrone() error = nil, want malformed JSON error")
	}
}
