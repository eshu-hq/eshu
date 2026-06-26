// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"os"
	"testing"
)

type githubExpected struct {
	Provider             string `json:"provider"`
	EventKind            string `json:"event_kind"`
	Decision             string `json:"decision"`
	DeliveryID           string `json:"delivery_id"`
	RepositoryExternalID string `json:"repository_external_id"`
	RepositoryFullName   string `json:"repository_full_name"`
	DefaultBranch        string `json:"default_branch"`
	Ref                  string `json:"ref"`
	BeforeSHA            string `json:"before_sha"`
	TargetSHA            string `json:"target_sha"`
	Action               string `json:"action"`
	Sender               string `json:"sender"`
	PullRequestNumber    string `json:"pull_request_number"`
	PullRequestURL       string `json:"pull_request_url"`
	PullRequestTitle     string `json:"pull_request_title"`
	Reason               string `json:"reason"`
}

func loadFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}
	return data
}

func loadExpected(t *testing.T, path string) githubExpected {
	t.Helper()
	data := loadFixture(t, path)
	var exp githubExpected
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("failed to unmarshal expected %s: %v", path, err)
	}
	return exp
}

func assertTriggerFromExpected(t *testing.T, got Trigger, want githubExpected) {
	t.Helper()
	if string(got.Provider) != want.Provider {
		t.Fatalf("Provider = %q, want %q", got.Provider, want.Provider)
	}
	if string(got.EventKind) != want.EventKind {
		t.Fatalf("EventKind = %q, want %q", got.EventKind, want.EventKind)
	}
	if string(got.Decision) != want.Decision {
		t.Fatalf("Decision = %q, want %q", got.Decision, want.Decision)
	}
	if got.Reason != "" && string(got.Reason) != want.Reason {
		t.Fatalf("Reason = %q, want %q", got.Reason, want.Reason)
	}
	if want.DeliveryID != "" && got.DeliveryID != want.DeliveryID {
		t.Fatalf("DeliveryID = %q, want %q", got.DeliveryID, want.DeliveryID)
	}
	if want.RepositoryExternalID != "" && got.RepositoryExternalID != want.RepositoryExternalID {
		t.Fatalf("RepositoryExternalID = %q, want %q", got.RepositoryExternalID, want.RepositoryExternalID)
	}
	if want.RepositoryFullName != "" && got.RepositoryFullName != want.RepositoryFullName {
		t.Fatalf("RepositoryFullName = %q, want %q", got.RepositoryFullName, want.RepositoryFullName)
	}
	if want.DefaultBranch != "" && got.DefaultBranch != want.DefaultBranch {
		t.Fatalf("DefaultBranch = %q, want %q", got.DefaultBranch, want.DefaultBranch)
	}
	if want.Ref != "" && got.Ref != want.Ref {
		t.Fatalf("Ref = %q, want %q", got.Ref, want.Ref)
	}
	if want.BeforeSHA != "" && got.BeforeSHA != want.BeforeSHA {
		t.Fatalf("BeforeSHA = %q, want %q", got.BeforeSHA, want.BeforeSHA)
	}
	if want.TargetSHA != "" && got.TargetSHA != want.TargetSHA {
		t.Fatalf("TargetSHA = %q, want %q", got.TargetSHA, want.TargetSHA)
	}
	if want.Action != "" && got.Action != want.Action {
		t.Fatalf("Action = %q, want %q", got.Action, want.Action)
	}
	if want.Sender != "" && got.Sender != want.Sender {
		t.Fatalf("Sender = %q, want %q", got.Sender, want.Sender)
	}
	if want.PullRequestNumber != "" && got.PullRequestNumber != want.PullRequestNumber {
		t.Fatalf("PullRequestNumber = %q, want %q", got.PullRequestNumber, want.PullRequestNumber)
	}
	if want.PullRequestURL != "" && got.PullRequestURL != want.PullRequestURL {
		t.Fatalf("PullRequestURL = %q, want %q", got.PullRequestURL, want.PullRequestURL)
	}
	if want.PullRequestTitle != "" && got.PullRequestTitle != want.PullRequestTitle {
		t.Fatalf("PullRequestTitle = %q, want %q", got.PullRequestTitle, want.PullRequestTitle)
	}
}

func TestNormalizeGitHubPushFromFixture(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "testdata/github/push.json")
	expected := loadExpected(t, "testdata/github/push_expected.json")

	trigger, err := NormalizeGitHub("push", expected.DeliveryID, payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitHub() error = %v, want nil", err)
	}

	assertTriggerFromExpected(t, trigger, expected)
}

func TestNormalizeGitHubPullRequestMergedFromFixture(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "testdata/github/pull_request_merged.json")
	expected := loadExpected(t, "testdata/github/pull_request_merged_expected.json")

	trigger, err := NormalizeGitHub("pull_request", expected.DeliveryID, payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitHub() error = %v, want nil", err)
	}

	assertTriggerFromExpected(t, trigger, expected)
}
