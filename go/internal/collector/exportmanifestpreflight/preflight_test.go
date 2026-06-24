// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exportmanifestpreflight

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPreflightAcceptsSafeOfflineExportManifests(t *testing.T) {
	t.Parallel()

	for _, sourceSystem := range []string{
		SourceSystemGitHub,
		SourceSystemJira,
		SourceSystemSlack,
		SourceSystemTeams,
		SourceSystemGoogleWorkspaceExport,
		SourceSystemGenericDocumentationExport,
	} {
		sourceSystem := sourceSystem
		t.Run(sourceSystem, func(t *testing.T) {
			t.Parallel()

			manifest := manifestJSON(t, map[string]any{
				"source_system":   sourceSystem,
				"source_scope_id": "private-scope-id",
				"exported_at":     "2026-06-09T00:00:00Z",
				"source_cursor":   "private-cursor",
				"acl_policy":      ACLPolicyEvaluated,
				"files": []map[string]any{{
					"path": "threads/thread-1.json",
					"kind": "thread",
				}},
			})

			result, err := Preflight(context.Background(), "private-manifest.json", bytes.NewReader(manifest), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			if !result.Safe {
				t.Fatalf("Safe = false, want true; warnings=%#v", result.Warnings)
			}
			if result.SourceSystem != sourceSystem {
				t.Fatalf("SourceSystem = %q, want %q", result.SourceSystem, sourceSystem)
			}
			if result.FileCount != 1 {
				t.Fatalf("FileCount = %d, want 1", result.FileCount)
			}
			assertNoResultLeak(t, result, "private-scope-id", "private-cursor", "thread-1", "private-manifest")
		})
	}
}

func TestPreflightClassifiesManifestShapeFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		manifest  []byte
		options   Options
		wantClass WarningClass
	}{
		{
			name:      "malformed_json",
			manifest:  []byte(`{"source_system":`),
			wantClass: WarningExportManifestInvalid,
		},
		{
			name: "missing_file_allowlist",
			manifest: manifestJSON(t, map[string]any{
				"source_system":   SourceSystemGitHub,
				"source_scope_id": "scope",
				"exported_at":     "2026-06-09T00:00:00Z",
				"acl_policy":      ACLPolicyEvaluated,
				"files":           []map[string]any{},
			}),
			wantClass: WarningAllowlistRequired,
		},
		{
			name: "missing_revision_metadata",
			manifest: manifestJSON(t, map[string]any{
				"source_system":   SourceSystemGitHub,
				"source_scope_id": "scope",
				"acl_policy":      ACLPolicyEvaluated,
				"files":           []map[string]any{{"path": "issues/1.json"}},
			}),
			wantClass: WarningExportManifestInvalid,
		},
		{
			name: "oversized_manifest",
			manifest: manifestJSON(t, map[string]any{
				"source_system":   SourceSystemGitHub,
				"source_scope_id": "scope",
				"exported_at":     "2026-06-09T00:00:00Z",
				"acl_policy":      ACLPolicyEvaluated,
				"files":           []map[string]any{{"path": strings.Repeat("a", 256) + ".json"}},
			}),
			options:   Options{MaxSourceBytes: 64},
			wantClass: WarningResourceLimitExceeded,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(tt.manifest), tt.options)
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			if result.Safe {
				t.Fatal("Safe = true, want false for invalid manifest")
			}
			assertWarning(t, result, tt.wantClass)
		})
	}
}

func TestPreflightClassifiesScopeACLAndUnsupportedSources(t *testing.T) {
	t.Parallel()

	manifest := manifestJSON(t, map[string]any{
		"source_system":     "private-provider",
		"source_scope_id":   "*",
		"source_scope_kind": "all",
		"exported_at":       "2026-06-09T00:00:00Z",
		"acl_policy":        ACLPolicyPartial,
		"files":             []map[string]any{{"path": "issues/1.json"}},
	})

	result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(manifest), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningExportFormatUnsupported)
	assertWarning(t, result, WarningAllowlistUnsupportedScope)
	assertWarning(t, result, WarningACLPartial)
	if result.UnsupportedSourceSystemCount != 1 || result.BroadScopeCount != 1 ||
		result.ACLPartialCount != 1 {
		t.Fatalf("unexpected unsupported/broad/ACL counts: %#v", result)
	}
	assertNoResultLeak(t, result, "private-provider")
}

func TestPreflightClassifiesUnsafeFilesAndMetadataOnlyContent(t *testing.T) {
	t.Parallel()

	manifest := manifestJSON(t, map[string]any{
		"source_system":   SourceSystemSlack,
		"source_scope_id": "private-workspace-channel",
		"exported_at":     "2026-06-09T00:00:00Z",
		"acl_policy":      ACLPolicyEvaluated,
		"metadata": map[string]any{
			"private_channel": true,
		},
		"files": []map[string]any{
			{"path": "../escape.json"},
			{"path": "archives/nested.zip"},
			{"path": "secrets/private_key.pem"},
			{"path": "attachments/design.png", "kind": "attachment", "content_type": "image/png"},
			{"path": "threads/edited.json", "edited": true},
			{"path": "threads/deleted.json", "deleted": true},
		},
	})

	result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(manifest), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningExportPathEscape)
	assertWarning(t, result, WarningExportArchiveMalformed)
	assertWarning(t, result, WarningSensitiveValueRedacted)
	assertWarning(t, result, WarningAttachmentMetadataOnly)
	assertWarning(t, result, WarningPrivateChannelMetadataOnly)
	if result.PathEscapeCount != 1 || result.NestedArchiveCount != 1 ||
		result.SensitiveValueCount != 1 || result.AttachmentReferenceCount != 1 ||
		result.PrivateMetadataCount != 1 || result.EditedItemCount != 1 ||
		result.DeletedItemCount != 1 {
		t.Fatalf("unexpected unsafe file counts: %#v", result)
	}
	assertNoResultLeak(t, result, "private-workspace-channel", "../escape.json", "private_key", "design.png", "edited.json", "deleted.json")
}

func TestPreflightClassifiesACLMissingAsUnavailable(t *testing.T) {
	t.Parallel()

	manifest := manifestJSON(t, map[string]any{
		"source_system":   SourceSystemTeams,
		"source_scope_id": "scope",
		"exported_at":     "2026-06-09T00:00:00Z",
		"files":           []map[string]any{{"path": "threads/thread.json"}},
	})

	result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(manifest), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningACLUnavailable)
	if result.ACLUnavailableCount != 1 {
		t.Fatalf("ACLUnavailableCount = %d, want 1", result.ACLUnavailableCount)
	}
}

func TestPreflightClassifiesAdditionalOfflineManifestRisks(t *testing.T) {
	t.Parallel()

	manifest := manifestJSON(t, map[string]any{
		"source_system":     SourceSystemJira,
		"source_scope_id":   "private-project",
		"source_scope_kind": "domain",
		"exported_at":       "2026-06-09T00:00:00Z",
		"acl_policy":        ACLPolicyEvaluated,
		"files": []map[string]any{
			{
				"path":           "tickets/1.json",
				"source_item_id": "private-ticket-1",
			},
			{
				"path":           "tickets/1-copy.json",
				"source_item_id": "private-ticket-1",
				"url":            "https://example.invalid/download?token=private-token",
			},
		},
	})

	result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(manifest), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningAllowlistUnsupportedScope)
	assertWarning(t, result, WarningDuplicateSourceItem)
	assertWarning(t, result, WarningSensitiveValueRedacted)
	if result.DuplicateSourceItemCount != 1 || result.SensitiveValueCount != 1 ||
		result.BroadScopeCount != 1 {
		t.Fatalf("unexpected duplicate/sensitive/broad counts: %#v", result)
	}
	assertNoResultLeak(t, result, "private-project", "private-ticket-1", "private-token", "example.invalid")
}

func TestPreflightIgnoresBenignURLQueryKeys(t *testing.T) {
	t.Parallel()

	manifest := manifestJSON(t, map[string]any{
		"source_system":   SourceSystemGenericDocumentationExport,
		"source_scope_id": "scope",
		"exported_at":     "2026-06-09T00:00:00Z",
		"acl_policy":      ACLPolicyEvaluated,
		"files": []map[string]any{{
			"path": "docs/readme.json",
			"url":  "https://example.invalid/download?monkey=value",
		}},
	})

	result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(manifest), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if !result.Safe {
		t.Fatalf("Safe = false, want true for benign query key; warnings=%#v", result.Warnings)
	}
}

func TestPreflightClassifiesEmptyAllowlist(t *testing.T) {
	t.Parallel()

	manifest := manifestJSON(t, map[string]any{
		"source_system":   SourceSystemGitHub,
		"source_scope_id": "scope",
		"exported_at":     "2026-06-09T00:00:00Z",
		"acl_policy":      ACLPolicyEvaluated,
		"files":           []map[string]any{},
	})

	result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(manifest), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningAllowlistRequired)
	assertWarning(t, result, WarningAllowlistEmpty)
	if result.AllowlistEmptyCount != 1 {
		t.Fatalf("AllowlistEmptyCount = %d, want 1", result.AllowlistEmptyCount)
	}
}

func TestPreflightClassifiesUnsafePathVariants(t *testing.T) {
	t.Parallel()

	for _, unsafePath := range []string{
		"/private/absolute.json",
		"C:/private/drive.json",
		"threads\\thread.json",
		"threads//thread.json",
		"threads/\x00bad.json",
		".",
		"threads/../bad.json",
	} {
		unsafePath := unsafePath
		t.Run(unsafePath, func(t *testing.T) {
			t.Parallel()

			manifest := manifestJSON(t, map[string]any{
				"source_system":   SourceSystemTeams,
				"source_scope_id": "scope",
				"exported_at":     "2026-06-09T00:00:00Z",
				"acl_policy":      ACLPolicyEvaluated,
				"files": []map[string]any{{
					"path": unsafePath,
				}},
			})

			result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(manifest), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			assertWarning(t, result, WarningExportPathEscape)
			if result.PathEscapeCount != 1 {
				t.Fatalf("PathEscapeCount = %d, want 1", result.PathEscapeCount)
			}
			assertNoResultLeak(t, result, unsafePath)
		})
	}
}

func TestPreflightClassifiesInvalidACLEnum(t *testing.T) {
	t.Parallel()

	manifest := manifestJSON(t, map[string]any{
		"source_system":   SourceSystemGitHub,
		"source_scope_id": "scope",
		"exported_at":     "2026-06-09T00:00:00Z",
		"acl_policy":      "private-acl-policy",
		"files":           []map[string]any{{"path": "issues/1.json"}},
	})

	result, err := Preflight(context.Background(), "manifest.json", bytes.NewReader(manifest), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningExportManifestInvalid)
	assertNoResultLeak(t, result, "private-acl-policy")
}

func TestPreflightClassifiesCanceledContextAsTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Preflight(ctx, "manifest.json", strings.NewReader(`{}`), Options{})
	if err == nil {
		t.Fatal("Preflight() error = nil, want canceled context error")
	}
	assertWarning(t, result, WarningTimeout)
}

func manifestJSON(t *testing.T, value map[string]any) []byte {
	t.Helper()

	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	return body
}

func assertWarning(t *testing.T, result Result, class WarningClass) {
	t.Helper()

	for _, warning := range result.Warnings {
		if warning.Class == class {
			if warning.Count == 0 {
				t.Fatalf("warning %q count = 0, want positive", class)
			}
			return
		}
	}
	t.Fatalf("missing warning %q in %#v", class, result.Warnings)
}

func assertNoResultLeak(t *testing.T, result Result, disallowed ...string) {
	t.Helper()

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v, want nil", err)
	}
	for _, text := range disallowed {
		if strings.Contains(string(encoded), text) {
			t.Fatalf("result leaked %q: %s", text, encoded)
		}
	}
}
