package webhook

import (
	"testing"
)

func TestVerifyGitHubSignature(t *testing.T) {
	t.Parallel()

	payload := []byte("Hello, World!")
	secret := "It's a Secret to Everybody"
	validSignature := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"

	if err := VerifyGitHubSignature(payload, secret, validSignature); err != nil {
		t.Fatalf("VerifyGitHubSignature() error = %v, want nil", err)
	}
}

func TestVerifyBitbucketSignature(t *testing.T) {
	t.Parallel()

	payload := []byte("Hello, World!")
	secret := "It's a Secret to Everybody"
	validSignature := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"

	if err := VerifyBitbucketSignature(payload, secret, validSignature); err != nil {
		t.Fatalf("VerifyBitbucketSignature() error = %v, want nil", err)
	}
}

func TestVerifyGitHubSignatureRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		secret    string
		signature string
	}{
		{
			name:      "missing signature",
			secret:    "secret",
			signature: "",
		},
		{
			name:      "legacy sha1 signature",
			secret:    "secret",
			signature: "sha1=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name:      "wrong signature",
			secret:    "secret",
			signature: "sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name:      "missing secret",
			secret:    "",
			signature: "sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := VerifyGitHubSignature([]byte("payload"), tt.secret, tt.signature); err == nil {
				t.Fatal("VerifyGitHubSignature() error = nil, want error")
			}
		})
	}
}

func TestVerifyGitLabToken(t *testing.T) {
	t.Parallel()

	if err := VerifyGitLabToken("secret", "secret"); err != nil {
		t.Fatalf("VerifyGitLabToken() error = %v, want nil", err)
	}
	if err := VerifyGitLabToken("secret", "wrong"); err == nil {
		t.Fatal("VerifyGitLabToken() error = nil, want mismatch error")
	}
	if err := VerifyGitLabToken("", "secret"); err == nil {
		t.Fatal("VerifyGitLabToken() error = nil, want missing secret error")
	}
}

func TestNormalizeGitHubPushAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"ref": "refs/heads/main",
		"before": "1111111111111111111111111111111111111111",
		"after": "2222222222222222222222222222222222222222",
		"repository": {
			"id": 42,
			"full_name": "eshu-hq/eshu",
			"default_branch": "main"
		},
		"sender": {"login": "linuxdynasty"}
	}`)

	trigger, err := NormalizeGitHub("push", "delivery-1", payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitHub() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderGitHub,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-1",
		RepositoryExternalID: "42",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		BeforeSHA:            "1111111111111111111111111111111111111111",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeGitHubPushIgnoresNonDefaultRefs(t *testing.T) {
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
					"id": 42,
					"full_name": "eshu-hq/eshu",
					"default_branch": "main"
				}
			}`)

			trigger, err := NormalizeGitHub("push", "delivery-1", payload, "")
			if err != nil {
				t.Fatalf("NormalizeGitHub() error = %v, want nil", err)
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

func TestNormalizeGitHubPullRequestAcceptsMergedDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"action": "closed",
		"number": 211,
		"pull_request": {
			"merged": true,
			"merge_commit_sha": "3333333333333333333333333333333333333333",
			"base": {"ref": "main"},
			"head": {"sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
		},
		"repository": {
			"id": 42,
			"full_name": "eshu-hq/eshu",
			"default_branch": "main"
		}
	}`)

	trigger, err := NormalizeGitHub("pull_request", "delivery-2", payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitHub() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderGitHub,
		EventKind:            EventKindPullRequestMerged,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-2",
		RepositoryExternalID: "42",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "3333333333333333333333333333333333333333",
		Action:               "closed",
	})
}

func TestNormalizeGitHubPullRequestIgnoresUnmergedClose(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"action": "closed",
		"pull_request": {
			"merged": false,
			"base": {"ref": "main"}
		},
		"repository": {
			"id": 42,
			"full_name": "eshu-hq/eshu",
			"default_branch": "main"
		}
	}`)

	trigger, err := NormalizeGitHub("pull_request", "delivery-3", payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitHub() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonPullRequestNotMerged {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonPullRequestNotMerged)
	}
}

func TestNormalizeGitLabPushAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"object_kind": "push",
		"event_name": "push",
		"ref": "refs/heads/main",
		"before": "1111111111111111111111111111111111111111",
		"after": "2222222222222222222222222222222222222222",
		"project": {
			"id": 77,
			"path_with_namespace": "eshu-hq/eshu",
			"default_branch": "main"
		},
		"user_username": "linuxdynasty"
	}`)

	trigger, err := NormalizeGitLab("Push Hook", "delivery-4", payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitLab() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderGitLab,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-4",
		RepositoryExternalID: "77",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		BeforeSHA:            "1111111111111111111111111111111111111111",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeGitLabMergeRequestAcceptsMergedDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"object_kind": "merge_request",
		"event_type": "merge_request",
		"project": {
			"id": 77,
			"path_with_namespace": "eshu-hq/eshu",
			"default_branch": "main"
		},
		"object_attributes": {
			"action": "merge",
			"target_branch": "main",
			"merge_commit_sha": "3333333333333333333333333333333333333333"
		},
		"user": {"username": "linuxdynasty"}
	}`)

	trigger, err := NormalizeGitLab("Merge Request Hook", "delivery-5", payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitLab() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderGitLab,
		EventKind:            EventKindMergeRequestMerged,
		Decision:             DecisionAccepted,
		DeliveryID:           "delivery-5",
		RepositoryExternalID: "77",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "3333333333333333333333333333333333333333",
		Action:               "merge",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeGitLabIgnoresNonRefreshingEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		eventHeader string
		payload     string
		reason      DecisionReason
	}{
		{
			name:        "tag push",
			eventHeader: "Tag Push Hook",
			payload: `{
				"object_kind": "tag_push",
				"ref": "refs/tags/v1.0.0",
				"after": "2222222222222222222222222222222222222222",
				"project": {"id": 77, "path_with_namespace": "eshu-hq/eshu", "default_branch": "main"}
			}`,
			reason: ReasonTagRef,
		},
		{
			name:        "unmerged merge request close",
			eventHeader: "Merge Request Hook",
			payload: `{
				"object_kind": "merge_request",
				"project": {"id": 77, "path_with_namespace": "eshu-hq/eshu", "default_branch": "main"},
				"object_attributes": {"action": "close", "target_branch": "main"}
			}`,
			reason: ReasonMergeRequestNotMerged,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			trigger, err := NormalizeGitLab(tt.eventHeader, "delivery-6", []byte(tt.payload), "")
			if err != nil {
				t.Fatalf("NormalizeGitLab() error = %v, want nil", err)
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

func TestNormalizeRejectsMalformedOrUnsupportedEvents(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeGitHub("push", "delivery-7", []byte(`{`), ""); err == nil {
		t.Fatal("NormalizeGitHub() error = nil, want malformed JSON error")
	}
	if _, err := NormalizeGitHub("issues", "delivery-8", []byte(`{}`), ""); err == nil {
		t.Fatal("NormalizeGitHub() error = nil, want unsupported event error")
	}
	if _, err := NormalizeGitLab("Issue Hook", "delivery-9", []byte(`{}`), ""); err == nil {
		t.Fatal("NormalizeGitLab() error = nil, want unsupported event error")
	}
}

func assertTrigger(t *testing.T, got Trigger, want Trigger) {
	t.Helper()

	if got.Provider != want.Provider {
		t.Fatalf("Provider = %q, want %q", got.Provider, want.Provider)
	}
	if got.EventKind != want.EventKind {
		t.Fatalf("EventKind = %q, want %q", got.EventKind, want.EventKind)
	}
	if got.Decision != want.Decision {
		t.Fatalf("Decision = %q, want %q", got.Decision, want.Decision)
	}
	if got.DeliveryID != want.DeliveryID {
		t.Fatalf("DeliveryID = %q, want %q", got.DeliveryID, want.DeliveryID)
	}
	if got.RepositoryExternalID != want.RepositoryExternalID {
		t.Fatalf("RepositoryExternalID = %q, want %q", got.RepositoryExternalID, want.RepositoryExternalID)
	}
	if got.RepositoryFullName != want.RepositoryFullName {
		t.Fatalf("RepositoryFullName = %q, want %q", got.RepositoryFullName, want.RepositoryFullName)
	}
	if got.DefaultBranch != want.DefaultBranch {
		t.Fatalf("DefaultBranch = %q, want %q", got.DefaultBranch, want.DefaultBranch)
	}
	if got.Ref != want.Ref {
		t.Fatalf("Ref = %q, want %q", got.Ref, want.Ref)
	}
	if got.BeforeSHA != want.BeforeSHA {
		t.Fatalf("BeforeSHA = %q, want %q", got.BeforeSHA, want.BeforeSHA)
	}
	if got.TargetSHA != want.TargetSHA {
		t.Fatalf("TargetSHA = %q, want %q", got.TargetSHA, want.TargetSHA)
	}
	if got.Action != want.Action {
		t.Fatalf("Action = %q, want %q", got.Action, want.Action)
	}
	if got.Sender != want.Sender {
		t.Fatalf("Sender = %q, want %q", got.Sender, want.Sender)
	}
}
