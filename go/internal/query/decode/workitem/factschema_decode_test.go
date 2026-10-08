// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/decode"
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
)

// TestDecodeWrappersDecodeValidRows pins the shared decode contract: every
// wrapper decodes a minimal valid payload into the typed struct. Moved here
// with the wrappers for #6623; it fails if any wrapper's success mapping
// mutates.
func TestDecodeWrappersDecodeValidRows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		decode  func(DecodeInput) (any, error)
		payload map[string]any
		check   func(t *testing.T, decoded any)
	}{
		{
			name: "record",
			decode: func(in DecodeInput) (any, error) {
				return DecodeRecord(in)
			},
			payload: map[string]any{
				"provider":              "jira_cloud",
				"provider_work_item_id": "10001",
				"work_item_key":         "OPS-123",
				"summary":               "Fix it",
			},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				record := decoded.(workitemv1.WorkItemRecord)
				if record.WorkItemKey != "OPS-123" || record.ProviderWorkItemID != "10001" {
					t.Fatalf("record identity = %q/%q, want OPS-123/10001", record.WorkItemKey, record.ProviderWorkItemID)
				}
				if record.Summary == nil || *record.Summary != "Fix it" {
					t.Fatalf("record summary = %+v, want Fix it", record.Summary)
				}
			},
		},
		{
			name: "transition",
			decode: func(in DecodeInput) (any, error) {
				return DecodeTransition(in)
			},
			payload: map[string]any{
				"provider":              "jira_cloud",
				"provider_changelog_id": "c1",
			},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				transition := decoded.(workitemv1.WorkItemTransition)
				if transition.ProviderChangelogID != "c1" {
					t.Fatalf("transition changelog = %q, want c1", transition.ProviderChangelogID)
				}
			},
		},
		{
			name: "external_link",
			decode: func(in DecodeInput) (any, error) {
				return DecodeExternalLink(in)
			},
			payload: map[string]any{"provider": "jira_cloud"},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				link := decoded.(workitemv1.WorkItemExternalLink)
				if link.Provider != "jira_cloud" {
					t.Fatalf("link provider = %q, want jira_cloud", link.Provider)
				}
			},
		},
		{
			name: "project_metadata",
			decode: func(in DecodeInput) (any, error) {
				return DecodeProjectMetadata(in)
			},
			payload: map[string]any{
				"provider":   "jira_cloud",
				"project_id": "p1",
			},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				metadata := decoded.(workitemv1.WorkItemProjectMetadata)
				if metadata.ProjectID == nil || *metadata.ProjectID != "p1" {
					t.Fatalf("project id = %+v, want p1", metadata.ProjectID)
				}
			},
		},
		{
			name: "issue_type_metadata",
			decode: func(in DecodeInput) (any, error) {
				return DecodeIssueTypeMetadata(in)
			},
			payload: map[string]any{
				"provider":      "jira_cloud",
				"issue_type_id": "10001",
			},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				metadata := decoded.(workitemv1.WorkItemIssueTypeMetadata)
				if metadata.IssueTypeID != "10001" {
					t.Fatalf("issue type id = %q, want 10001", metadata.IssueTypeID)
				}
			},
		},
		{
			name: "status_metadata",
			decode: func(in DecodeInput) (any, error) {
				return DecodeStatusMetadata(in)
			},
			payload: map[string]any{
				"provider":  "jira_cloud",
				"status_id": "s1",
			},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				metadata := decoded.(workitemv1.WorkItemStatusMetadata)
				if metadata.StatusID != "s1" {
					t.Fatalf("status id = %q, want s1", metadata.StatusID)
				}
			},
		},
		{
			name: "workflow_metadata",
			decode: func(in DecodeInput) (any, error) {
				return DecodeWorkflowMetadata(in)
			},
			payload: map[string]any{
				"provider":    "jira_cloud",
				"workflow_id": "w1",
			},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				metadata := decoded.(workitemv1.WorkItemWorkflowMetadata)
				if metadata.WorkflowID != "w1" {
					t.Fatalf("workflow id = %q, want w1", metadata.WorkflowID)
				}
			},
		},
		{
			name: "field_metadata",
			decode: func(in DecodeInput) (any, error) {
				return DecodeFieldMetadata(in)
			},
			payload: map[string]any{"provider": "jira_cloud"},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				metadata := decoded.(workitemv1.WorkItemFieldMetadata)
				if metadata.Provider != "jira_cloud" {
					t.Fatalf("field provider = %q, want jira_cloud", metadata.Provider)
				}
			},
		},
		{
			name: "metadata_warning",
			decode: func(in DecodeInput) (any, error) {
				return DecodeMetadataWarning(in)
			},
			payload: map[string]any{
				"provider":      "jira_cloud",
				"metadata_type": "issue_type",
				"reason":        "unknown member",
			},
			check: func(t *testing.T, decoded any) {
				t.Helper()
				warning := decoded.(workitemv1.WorkItemMetadataWarning)
				if warning.MetadataType != "issue_type" || warning.Reason != "unknown member" {
					t.Fatalf("warning = %+v, want issue_type/unknown member", warning)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decoded, err := tc.decode(DecodeInput{FactID: "fact-1", SchemaVersion: "1.0.0", Payload: tc.payload})
			if err != nil {
				t.Fatalf("decode valid %s row: unexpected error: %v", tc.name, err)
			}
			tc.check(t, decoded)
		})
	}
}

// TestDecodeWrappersClassifyMissingFields pins the classified-error
// contract: an empty payload yields a *decode.Error attributed to the input
// fact for every wrapper, never a silent zero struct.
func TestDecodeWrappersClassifyMissingFields(t *testing.T) {
	t.Parallel()

	wrappers := map[string]func(DecodeInput) (any, error){
		"record":           func(in DecodeInput) (any, error) { return DecodeRecord(in) },
		"transition":       func(in DecodeInput) (any, error) { return DecodeTransition(in) },
		"external_link":    func(in DecodeInput) (any, error) { return DecodeExternalLink(in) },
		"project_metadata": func(in DecodeInput) (any, error) { return DecodeProjectMetadata(in) },
		"issue_type":       func(in DecodeInput) (any, error) { return DecodeIssueTypeMetadata(in) },
		"status_metadata":  func(in DecodeInput) (any, error) { return DecodeStatusMetadata(in) },
		"workflow":         func(in DecodeInput) (any, error) { return DecodeWorkflowMetadata(in) },
		"field_metadata":   func(in DecodeInput) (any, error) { return DecodeFieldMetadata(in) },
		"warning":          func(in DecodeInput) (any, error) { return DecodeMetadataWarning(in) },
	}

	for name, wrapper := range wrappers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := wrapper(DecodeInput{FactID: "fact-1", SchemaVersion: "1.0.0", Payload: map[string]any{}})
			if err == nil {
				t.Fatalf("decode empty %s payload: got nil error, want *decode.Error", name)
			}
			var decodeErr *decode.Error
			if !errors.As(err, &decodeErr) {
				t.Fatalf("decode empty %s payload: error type %T, want *decode.Error", name, err)
			}
			if decodeErr.FactID != "fact-1" {
				t.Fatalf("decode empty %s payload: fact id %q, want fact-1", name, decodeErr.FactID)
			}
		})
	}
}

// TestSchemaEnvelopeNormalizesVersion pins the version contract: a
// version-less row normalizes to the shared default major, a stamped row
// keeps its version.
func TestSchemaEnvelopeNormalizesVersion(t *testing.T) {
	t.Parallel()

	envelope := SchemaEnvelope("work_item.record", "", map[string]any{})
	if envelope.SchemaVersion != decode.DefaultSchemaMajorVersion {
		t.Fatalf("empty version normalized to %q, want %q", envelope.SchemaVersion, decode.DefaultSchemaMajorVersion)
	}
	envelope = SchemaEnvelope("work_item.record", "2.0.0", map[string]any{})
	if envelope.SchemaVersion != "2.0.0" {
		t.Fatalf("stamped version = %q, want 2.0.0", envelope.SchemaVersion)
	}
}

// TestDerefBool pins the nil-safe *bool deref both read paths share.
func TestDerefBool(t *testing.T) {
	t.Parallel()

	truth, falsehood := true, false
	cases := []struct {
		name  string
		value *bool
		want  bool
	}{
		{"nil", nil, false},
		{"true", &truth, true},
		{"false", &falsehood, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DerefBool(tc.value); got != tc.want {
				t.Fatalf("DerefBool = %v, want %v", got, tc.want)
			}
		})
	}
}
