// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package documentationexport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/collector/exportmanifestpreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCollectEmitsOfflineExportDocumentationFacts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		sourceSystem string
		path         string
		record       map[string]any
		wantHeading  string
		wantMetadata string
	}{
		{
			name:         "github issue export",
			sourceSystem: exportmanifestpreflight.SourceSystemGitHub,
			path:         "github/issues/42.json",
			record: map[string]any{
				"id":    "issue-42",
				"title": "Rollout checklist",
				"body":  "Deploy the API after the reducer catches up.",
				"comments": []map[string]any{{
					"id":   "comment-1",
					"body": "Rollback owner confirmed.",
				}},
				"links": []map[string]any{{
					"id":     "runbook",
					"target": "docs/rollback.md",
					"anchor": "Rollback runbook",
				}},
			},
			wantHeading:  "Rollout checklist",
			wantMetadata: "github",
		},
		{
			name:         "jira issue export",
			sourceSystem: exportmanifestpreflight.SourceSystemJira,
			path:         "jira/issues/work-item.json",
			record: map[string]any{
				"id":    "jira-100",
				"title": "Incident follow-up",
				"body":  "Add evidence before closing the follow-up.",
				"changelog": []map[string]any{{
					"id":   "change-1",
					"body": "Status moved to In Review.",
				}},
			},
			wantHeading:  "Incident follow-up",
			wantMetadata: "jira",
		},
		{
			name:         "slack thread export",
			sourceSystem: exportmanifestpreflight.SourceSystemSlack,
			path:         "slack/threads/thread.json",
			record: map[string]any{
				"id":    "thread-1",
				"title": "Deploy discussion",
				"messages": []map[string]any{{
					"id":   "message-1",
					"body": "Wait for queue zero before deploy.",
				}},
			},
			wantHeading:  "Deploy discussion",
			wantMetadata: "slack",
		},
		{
			name:         "teams thread export",
			sourceSystem: exportmanifestpreflight.SourceSystemTeams,
			path:         "teams/chats/thread.json",
			record: map[string]any{
				"id":    "teams-thread-1",
				"title": "Design review",
				"messages": []map[string]any{{
					"id":      "reply-1",
					"body":    "The importer stays default-off.",
					"edited":  true,
					"deleted": false,
				}, {
					"id":      "reply-2",
					"deleted": true,
				}},
			},
			wantHeading:  "Design review",
			wantMetadata: "teams",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := Collect(context.Background(), safeRequest(t, tc.sourceSystem, tc.path, tc.record))
			if err != nil {
				t.Fatalf("Collect() error = %v, want nil", err)
			}
			if !result.Preflight.Safe {
				t.Fatalf("preflight Safe = false, want true: %#v", result.Preflight.Warnings)
			}
			documents := factsByKind(result.Envelopes, facts.DocumentationDocumentFactKind)
			if got, want := len(documents), 1; got != want {
				t.Fatalf("documentation_document count = %d, want %d", got, want)
			}
			if got := payloadString(documents[0].Payload, "title"); got != tc.wantHeading {
				t.Fatalf("title = %q, want %q", got, tc.wantHeading)
			}
			if got := sourceMetadataValue(documents[0].Payload, "source_system"); got != tc.wantMetadata {
				t.Fatalf("source_system metadata = %q, want %q", got, tc.wantMetadata)
			}
			sections := factsByKind(result.Envelopes, facts.DocumentationSectionFactKind)
			if len(sections) == 0 {
				t.Fatalf("documentation_section count = 0, want positive")
			}
			assertNoPayloadLeak(t, result.Envelopes, "private-scope", "issue-42", "jira-100", "message-1", "teams-thread-1")
		})
	}
}

func TestCollectFailsClosedWhenManifestPreflightWarns(t *testing.T) {
	t.Parallel()

	request := safeRequest(t, exportmanifestpreflight.SourceSystemSlack, "slack/threads/thread.json", map[string]any{
		"id":    "thread-1",
		"title": "Private thread",
		"messages": []map[string]any{{
			"id":   "message-1",
			"body": "must not emit",
		}},
	})
	request.Manifest = manifestJSON(t, exportmanifestpreflight.SourceSystemSlack, "slack/threads/thread.json", map[string]any{
		"private_channel": true,
	})

	result, err := Collect(context.Background(), request)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.Preflight.Safe {
		t.Fatal("preflight Safe = true, want false")
	}
	if len(result.Envelopes) != 0 {
		t.Fatalf("len(Envelopes) = %d, want 0", len(result.Envelopes))
	}
}

func TestCollectEmitsMetadataOnlyWarningsForBadRecords(t *testing.T) {
	t.Parallel()

	request := safeRequest(t, exportmanifestpreflight.SourceSystemGitHub, "github/issues/42.json", nil)
	request.Files["github/issues/42.json"] = []byte(`{"id":`)
	unsupportedPath := "github/issues/43.json"
	request.Manifest = manifestJSONWithFiles(t, exportmanifestpreflight.SourceSystemGitHub, []string{
		"github/issues/42.json",
		unsupportedPath,
	}, nil)
	request.Files[unsupportedPath] = []byte(`{"id":"record-43","title":"Unsupported"}`)

	result, err := Collect(context.Background(), request)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	documents := factsByKind(result.Envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	warnings := map[string]bool{}
	for _, document := range documents {
		warnings[sourceMetadataValue(document.Payload, "warning")] = true
	}
	for _, want := range []string{"malformed_json", "unsupported_export_shape"} {
		if !warnings[want] {
			t.Fatalf("metadata-only warning %q missing from %#v", want, warnings)
		}
	}
	if got := len(factsByKind(result.Envelopes, facts.DocumentationSectionFactKind)); got != 0 {
		t.Fatalf("documentation_section count = %d, want 0", got)
	}
}

func TestCollectRedactsTokenBearingLinks(t *testing.T) {
	t.Parallel()

	request := safeRequest(t, exportmanifestpreflight.SourceSystemGenericDocumentationExport, "docs/thread.json", map[string]any{
		"id":    "thread-1",
		"title": "Support packet",
		"body":  "See the private link.",
		"links": []map[string]any{{
			"id":         "download",
			"section_id": "message-1",
			"target":     "https://example.invalid/download?token=private-token",
			"anchor":     "private download",
		}},
	})

	result, err := Collect(context.Background(), request)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	links := factsByKind(result.Envelopes, facts.DocumentationLinkFactKind)
	if got, want := len(links), 1; got != want {
		t.Fatalf("documentation_link count = %d, want %d", got, want)
	}
	if got := payloadString(links[0].Payload, "target_uri"); !strings.HasPrefix(got, "redacted:target:") {
		t.Fatalf("target_uri = %q, want redacted target", got)
	}
	if got := sourceMetadataValue(links[0].Payload, "redaction_reason"); got != "token_bearing_url" {
		t.Fatalf("redaction_reason = %q, want token_bearing_url", got)
	}
	assertNoPayloadLeak(t, result.Envelopes, "private-token", "example.invalid", "message-1")
}

func TestCollectRedactsUnsafeLocalLinkTargets(t *testing.T) {
	t.Parallel()

	request := safeRequest(t, exportmanifestpreflight.SourceSystemGenericDocumentationExport, "docs/thread.json", map[string]any{
		"id":    "thread-1",
		"title": "Support packet",
		"body":  "See the attached files.",
		"links": []map[string]any{{
			"id":     "windows",
			"target": `C:\Users\private\runbook.md`,
			"anchor": "local runbook",
		}, {
			"id":     "credential",
			"target": "docs/secret.md",
			"anchor": "secret attachment",
		}},
	})

	result, err := Collect(context.Background(), request)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	links := factsByKind(result.Envelopes, facts.DocumentationLinkFactKind)
	if got, want := len(links), 2; got != want {
		t.Fatalf("documentation_link count = %d, want %d", got, want)
	}
	for _, link := range links {
		if got := payloadString(link.Payload, "target_uri"); !strings.HasPrefix(got, "redacted:path:") {
			t.Fatalf("target_uri = %q, want redacted path", got)
		}
	}
	assertNoPayloadLeak(t, result.Envelopes, "C:", "Users", "secret.md")
}

func TestCollectContentHashIncludesNestedSections(t *testing.T) {
	t.Parallel()

	first, err := Collect(context.Background(), safeRequest(t, exportmanifestpreflight.SourceSystemSlack, "slack/threads/thread.json", map[string]any{
		"id":    "thread-1",
		"title": "Deploy discussion",
		"messages": []map[string]any{{
			"id":   "message-1",
			"body": "Wait for queue zero before deploy.",
		}},
	}))
	if err != nil {
		t.Fatalf("Collect(first) error = %v, want nil", err)
	}
	second, err := Collect(context.Background(), safeRequest(t, exportmanifestpreflight.SourceSystemSlack, "slack/threads/thread.json", map[string]any{
		"id":    "thread-1",
		"title": "Deploy discussion",
		"messages": []map[string]any{{
			"id":   "message-1",
			"body": "Deploy can proceed after queue zero.",
		}},
	}))
	if err != nil {
		t.Fatalf("Collect(second) error = %v, want nil", err)
	}
	firstDoc := factsByKind(first.Envelopes, facts.DocumentationDocumentFactKind)[0]
	secondDoc := factsByKind(second.Envelopes, facts.DocumentationDocumentFactKind)[0]
	if got, wantDifferent := payloadString(firstDoc.Payload, "content_hash"), payloadString(secondDoc.Payload, "content_hash"); got == wantDifferent {
		t.Fatalf("content_hash = %q for both records, want nested message content to affect hash", got)
	}
}

func TestCollectFingerprintsUnknownSourceScopeKind(t *testing.T) {
	t.Parallel()

	request := safeRequest(t, exportmanifestpreflight.SourceSystemSlack, "slack/threads/thread.json", map[string]any{
		"id":    "thread-1",
		"title": "Deploy discussion",
		"body":  "Wait for queue zero before deploy.",
	})
	request.Manifest = manifestJSONWithScopeKind(t, exportmanifestpreflight.SourceSystemSlack, "slack/threads/thread.json", "private-channel-kind")

	result, err := Collect(context.Background(), request)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	sources := factsByKind(result.Envelopes, facts.DocumentationSourceFactKind)
	if got, want := len(sources), 1; got != want {
		t.Fatalf("documentation_source count = %d, want %d", got, want)
	}
	if got := sourceMetadataValue(sources[0].Payload, "source_scope_kind"); got == "private-channel-kind" {
		t.Fatalf("source_scope_kind leaked raw value %q", got)
	}
	if got := sourceMetadataValue(sources[0].Payload, "source_scope_kind_hash"); got == "" {
		t.Fatal("source_scope_kind_hash is empty, want fingerprint for unknown kind")
	}
	assertNoPayloadLeak(t, result.Envelopes, "private-channel-kind")
}

func TestCollectTruncatesSectionsOnUTF8Boundary(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("a", maxSectionBytes-1) + "étail"
	result, err := Collect(context.Background(), safeRequest(t, exportmanifestpreflight.SourceSystemSlack, "slack/threads/thread.json", map[string]any{
		"id":    "thread-1",
		"title": "Deploy discussion",
		"body":  content,
	}))
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	sections := factsByKind(result.Envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	gotContent := payloadString(sections[0].Payload, "content")
	if !utf8.ValidString(gotContent) {
		t.Fatalf("content is not valid UTF-8: %q", gotContent)
	}
	if len(gotContent) > maxSectionBytes {
		t.Fatalf("len(content) = %d, want <= %d", len(gotContent), maxSectionBytes)
	}
	if got, want := payloadString(sections[0].Payload, "text_hash"), safeFingerprint(gotContent); got != want {
		t.Fatalf("text_hash = %q, want %q", got, want)
	}
}

func safeRequest(t *testing.T, sourceSystem, filePath string, record map[string]any) Request {
	t.Helper()

	if record == nil {
		record = map[string]any{
			"id":    "record-1",
			"title": "Unsupported",
		}
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal(record) error = %v, want nil", err)
	}
	return Request{
		ScopeID:      "scope-export",
		GenerationID: "generation-export",
		ObservedAt:   time.Date(2026, time.June, 9, 10, 0, 0, 0, time.UTC),
		ManifestName: "manifest.json",
		Manifest:     manifestJSON(t, sourceSystem, filePath, nil),
		Files:        map[string][]byte{filePath: body},
	}
}

func manifestJSON(t *testing.T, sourceSystem, filePath string, metadata map[string]any) []byte {
	t.Helper()

	return manifestJSONWithFiles(t, sourceSystem, []string{filePath}, metadata)
}

func manifestJSONWithScopeKind(t *testing.T, sourceSystem, filePath string, scopeKind string) []byte {
	t.Helper()

	return manifestJSONWithFilesAndScopeKind(t, sourceSystem, []string{filePath}, nil, scopeKind)
}

func manifestJSONWithFiles(t *testing.T, sourceSystem string, filePaths []string, metadata map[string]any) []byte {
	t.Helper()

	return manifestJSONWithFilesAndScopeKind(t, sourceSystem, filePaths, metadata, "")
}

func manifestJSONWithFilesAndScopeKind(t *testing.T, sourceSystem string, filePaths []string, metadata map[string]any, scopeKind string) []byte {
	t.Helper()

	if metadata == nil {
		metadata = map[string]any{}
	}
	files := make([]map[string]any, 0, len(filePaths))
	for _, filePath := range filePaths {
		files = append(files, map[string]any{
			"path": filePath,
			"kind": "thread",
		})
	}
	body, err := json.Marshal(map[string]any{
		"source_system":     sourceSystem,
		"source_scope_id":   "private-scope",
		"source_scope_kind": scopeKind,
		"exported_at":       "2026-06-09T00:00:00Z",
		"source_cursor":     "private-cursor",
		"acl_policy":        exportmanifestpreflight.ACLPolicyEvaluated,
		"metadata":          metadata,
		"files":             files,
	})
	if err != nil {
		t.Fatalf("json.Marshal(manifest) error = %v, want nil", err)
	}
	return body
}

func factsByKind(envelopes []facts.Envelope, kind string) []facts.Envelope {
	out := []facts.Envelope{}
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			out = append(out, envelope)
		}
	}
	return out
}

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func sourceMetadataValue(payload map[string]any, key string) string {
	metadata, ok := payload["source_metadata"].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}

func assertNoPayloadLeak(t *testing.T, envelopes []facts.Envelope, disallowed ...string) {
	t.Helper()

	encoded, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("json.Marshal(envelopes) error = %v, want nil", err)
	}
	for _, text := range disallowed {
		if strings.Contains(string(encoded), text) {
			t.Fatalf("payload leaked %q: %s", text, encoded)
		}
	}
}
