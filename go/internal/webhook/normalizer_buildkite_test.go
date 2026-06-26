// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import "testing"

func TestNormalizeBuildkiteAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"build": {
			"commit": "2222222222222222222222222222222222222222",
			"branch": "main"
		},
		"pipeline": {"slug": "eshu-hq/eshu"},
		"sender": {"name": "linuxdynasty"}
	}`)

	trigger, err := NormalizeBuildkite("build.finished", "delivery-b1", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeBuildkite() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderBuildkite,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-b1",
		RepositoryExternalID: "eshu-hq/eshu",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeBuildkiteIgnoresNonDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"build": {
			"commit": "2222222222222222222222222222222222222222",
			"branch": "feature"
		},
		"pipeline": {"slug": "eshu-hq/eshu"}
	}`)

	trigger, err := NormalizeBuildkite("build.finished", "delivery-b2", payload, "main")
	if err != nil {
		t.Fatalf("NormalizeBuildkite() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonNonDefaultBranch {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonNonDefaultBranch)
	}
}

func TestNormalizeBuildkiteRejectsUnsupportedEvent(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeBuildkite("build.queued", "delivery-b3", []byte(`{}`), "main"); err == nil {
		t.Fatal("NormalizeBuildkite() error = nil, want unsupported event error")
	}
}

func TestNormalizeBuildkiteRejectsMalformedPayload(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeBuildkite("build.finished", "delivery-b4", []byte(`{`), "main"); err == nil {
		t.Fatal("NormalizeBuildkite() error = nil, want malformed JSON error")
	}
}
