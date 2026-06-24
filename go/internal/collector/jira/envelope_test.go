// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewWorkItemRecordEnvelopeBuildsReportedSourceFact(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	issue := Issue{
		ID:         "10001",
		Key:        "OPS-123",
		Summary:    "Investigate checkout alert",
		IssueType:  Reference{ID: "10002", Name: "Incident"},
		Status:     Reference{ID: "3", Name: "In Progress"},
		Project:    Reference{ID: "10000", Key: "OPS", Name: "Operations"},
		Assignee:   Reference{AccountID: "acct-1", DisplayName: "Primary Oncall"},
		Reporter:   Reference{AccountID: "acct-2", DisplayName: "SRE Lead"},
		CreatedAt:  ctx.ObservedAt.Add(-2 * time.Hour),
		UpdatedAt:  ctx.ObservedAt.Add(-10 * time.Minute),
		ResolvedAt: ctx.ObservedAt,
		Self:       "https://example.atlassian.net/rest/api/3/issue/10001?token=secret",
		BrowseURL:  "https://example.atlassian.net/browse/OPS-123?jwt=secret",
	}

	env, err := NewWorkItemRecordEnvelope(ctx, issue)
	if err != nil {
		t.Fatalf("NewWorkItemRecordEnvelope() error = %v, want nil", err)
	}
	if env.FactKind != facts.WorkItemRecordFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.WorkItemRecordFactKind)
	}
	if env.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want reported", env.SourceConfidence)
	}
	if strings.Contains(env.SourceRef.SourceURI, "token=secret") || strings.Contains(env.Payload["source_url"].(string), "jwt=secret") {
		t.Fatalf("source URLs were not sanitized: ref=%q payload=%q", env.SourceRef.SourceURI, env.Payload["source_url"])
	}
}

func TestNewWorkItemTransitionEnvelopeUsesChangelogIdentity(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	transition := Transition{
		ID:        "20001",
		IssueID:   "10001",
		IssueKey:  "OPS-123",
		Field:     "status",
		From:      "To Do",
		To:        "In Progress",
		Author:    Reference{AccountID: "acct-1", DisplayName: "Primary Oncall"},
		CreatedAt: ctx.ObservedAt.Add(-time.Hour),
	}

	env, err := NewWorkItemTransitionEnvelope(ctx, transition)
	if err != nil {
		t.Fatalf("NewWorkItemTransitionEnvelope() error = %v, want nil", err)
	}
	if env.FactKind != facts.WorkItemTransitionFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.WorkItemTransitionFactKind)
	}
	if got, want := env.Payload["provider_changelog_id"], "20001"; got != want {
		t.Fatalf("provider_changelog_id = %q, want %q", got, want)
	}
}

func TestNewWorkItemExternalLinkEnvelopePreservesBoundedLinkEvidence(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	link := ExternalLink{
		ID:           "30001",
		IssueID:      "10001",
		IssueKey:     "OPS-123",
		GlobalID:     "github:pr:42",
		Application:  LinkApplication{Name: "GitHub", Type: "com.github"},
		Relationship: "causes",
		Object: LinkObject{
			URL:     "https://github.com/example/app/pull/42?access_token=secret",
			Title:   "PR 42",
			Summary: "Deploy checkout-api",
		},
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}
	if env.FactKind != facts.WorkItemExternalLinkFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.WorkItemExternalLinkFactKind)
	}
	if strings.Contains(env.Payload["url"].(string), "access_token=secret") {
		t.Fatalf("external link URL = %q, want sensitive query redacted", env.Payload["url"])
	}
}

func TestNewWorkItemExternalLinkEnvelopeAcceptsURLOnlyIdentity(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	link := ExternalLink{
		IssueID:      "10001",
		IssueKey:     "OPS-123",
		Relationship: "relates to",
		Object: LinkObject{
			URL:   "https://github.com/example/app/pull/42?jwt=secret",
			Title: "PR 42",
		},
	}

	env, err := NewWorkItemExternalLinkEnvelope(ctx, link)
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}
	if got := env.Payload["url"].(string); strings.Contains(got, "jwt=secret") {
		t.Fatalf("external link URL = %q, want sensitive query redacted", got)
	}
	if env.SourceRef.SourceRecordID == "" {
		t.Fatal("SourceRecordID is blank, want URL fallback identity")
	}
}

func TestWorkItemEnvelopesRedactPrivateTextUsersAndURLs(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	record, err := NewWorkItemRecordEnvelope(ctx, Issue{
		ID:       "10001",
		Key:      "OPS-123",
		Summary:  "Customer checkout is broken",
		Status:   Reference{ID: "3", Name: "In Progress"},
		Project:  Reference{ID: "10000", Key: "OPS", Name: "Private Operations"},
		Assignee: Reference{AccountID: "acct-private-1", DisplayName: "Private User"},
		Reporter: Reference{AccountID: "acct-private-2", DisplayName: "Reporter User"},
		Self:     "https://example.atlassian.net/rest/api/3/issue/10001?token=secret",
	})
	if err != nil {
		t.Fatalf("NewWorkItemRecordEnvelope() error = %v, want nil", err)
	}
	if got := record.Payload["summary"]; got != "" {
		t.Fatalf("summary payload = %q, want private issue text redacted", got)
	}
	if got := record.Payload["assignee_account_id"]; got != "" {
		t.Fatalf("assignee_account_id = %q, want user identifier redacted", got)
	}
	if got := record.Payload["reporter_display_name"]; got != "" {
		t.Fatalf("reporter_display_name = %q, want user display name redacted", got)
	}
	if got := record.Payload["self_url"]; got != "" {
		t.Fatalf("self_url = %q, want private Jira URL redacted", got)
	}
	if got := record.Payload["self_url_fingerprint"]; got == "" {
		t.Fatal("self_url_fingerprint is blank, want normalized URL fingerprint")
	}
	if got := record.Payload["redaction_policy_version"]; got != "jira_work_item_v1" {
		t.Fatalf("redaction_policy_version = %q, want jira_work_item_v1", got)
	}

	transition, err := NewWorkItemTransitionEnvelope(ctx, Transition{
		ID:             "20001",
		IssueID:        "10001",
		IssueKey:       "OPS-123",
		Field:          "assignee",
		From:           "Private User",
		To:             "Another Private User",
		Author:         Reference{AccountID: "acct-private-3", DisplayName: "Change Author"},
		ValueRedacted:  true,
		AuthorRedacted: true,
	})
	if err != nil {
		t.Fatalf("NewWorkItemTransitionEnvelope() error = %v, want nil", err)
	}
	if got := transition.Payload["from"]; got != "" {
		t.Fatalf("transition from = %q, want private field value redacted", got)
	}
	if got := transition.Payload["author_account_id"]; got != "" {
		t.Fatalf("author_account_id = %q, want user identifier redacted", got)
	}
	if got := transition.Payload["value_redacted"]; got != true {
		t.Fatalf("value_redacted = %v, want true", got)
	}

	link, err := NewWorkItemExternalLinkEnvelope(ctx, ExternalLink{
		ID:          "30001",
		IssueID:     "10001",
		IssueKey:    "OPS-123",
		Application: LinkApplication{Name: "Unknown Tracker", Type: "unknown"},
		Object: LinkObject{
			URL:     "https://private.example.invalid/deploy/123?token=secret",
			Title:   "Private deploy",
			Summary: "Private deployment summary",
		},
	})
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope() error = %v, want nil", err)
	}
	if got := link.Payload["url"]; got != "" {
		t.Fatalf("external link URL = %q, want private URL redacted", got)
	}
	if got := link.Payload["title"]; got != "" {
		t.Fatalf("external link title = %q, want private title redacted", got)
	}
	if got := link.Payload["url_fingerprint"]; got == "" {
		t.Fatal("url_fingerprint is blank, want normalized external URL fingerprint")
	}
	if got := link.Payload["provider_support_state"]; got != "unsupported_provider" {
		t.Fatalf("provider_support_state = %q, want unsupported_provider", got)
	}
}

func TestWorkItemStableKeysConvergeAcrossDuplicateWindows(t *testing.T) {
	t.Parallel()

	issue := Issue{
		ID:        "10001",
		Key:       "OPS-123",
		Status:    Reference{ID: "3", Name: "In Progress"},
		Project:   Reference{ID: "10000", Key: "OPS"},
		UpdatedAt: time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC),
	}
	firstCtx := testEnvelopeContext()
	firstCtx.GenerationID = "jira:generation-1"
	secondCtx := testEnvelopeContext()
	secondCtx.GenerationID = "jira:generation-2"

	first, err := NewWorkItemRecordEnvelope(firstCtx, issue)
	if err != nil {
		t.Fatalf("NewWorkItemRecordEnvelope(first) error = %v, want nil", err)
	}
	second, err := NewWorkItemRecordEnvelope(secondCtx, issue)
	if err != nil {
		t.Fatalf("NewWorkItemRecordEnvelope(second) error = %v, want nil", err)
	}
	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey mismatch for duplicate window: %q != %q", first.StableFactKey, second.StableFactKey)
	}
	if first.FactID == second.FactID {
		t.Fatalf("FactID = %q for both generations, want generation-specific fact IDs", first.FactID)
	}
}

func testEnvelopeContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "jira:site:example",
		GenerationID:        "jira:generation-1",
		CollectorInstanceID: "jira-primary",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC),
		SourceURI:           "https://example.atlassian.net",
	}
}
