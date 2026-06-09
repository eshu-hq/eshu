package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/googleworkspace"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFactStoreRoundTripsGoogleWorkspaceDocumentationFacts(t *testing.T) {
	t.Parallel()

	envelopes := collectGoogleWorkspaceReadbackFacts(t)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: googleWorkspaceFactRows(t, envelopes)}},
	}
	store := NewFactStore(db)

	if err := store.UpsertFacts(context.Background(), envelopes); err != nil {
		t.Fatalf("UpsertFacts() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), len(envelopes)*columnsPerFactRow; got != want {
		t.Fatalf("upsert arg count = %d, want %d", got, want)
	}
	if encoded := fmt.Sprint(db.execs[0].args); strings.Contains(encoded, "gws-doc-readback") ||
		strings.Contains(encoded, "synthetic-secret") {
		t.Fatalf("upsert args leaked provider identifiers: %s", encoded)
	}

	loaded, err := store.ListFactsByKind(
		context.Background(),
		"doc-source:google_workspace:synthetic",
		"gws-gen-readback",
		[]string{
			facts.DocumentationSourceFactKind,
			facts.DocumentationDocumentFactKind,
			facts.DocumentationSectionFactKind,
			facts.DocumentationLinkFactKind,
		},
	)
	if err != nil {
		t.Fatalf("ListFactsByKind() error = %v, want nil", err)
	}
	assertGoogleWorkspaceFactKinds(t, loaded, map[string]int{
		facts.DocumentationSourceFactKind:   1,
		facts.DocumentationDocumentFactKind: 1,
		facts.DocumentationSectionFactKind:  1,
		facts.DocumentationLinkFactKind:     1,
	})
	section := googleWorkspaceLoadedPayload(t, loaded, facts.DocumentationSectionFactKind)
	if got, want := section["content"], "Synthetic workspace runbook"; got != want {
		t.Fatalf("section content = %#v, want %#v", got, want)
	}
	document := googleWorkspaceLoadedPayload(t, loaded, facts.DocumentationDocumentFactKind)
	metadata := document["source_metadata"].(map[string]any)
	if got, want := metadata["export_mime"], googleworkspace.ExportMIMEDOCX; got != want {
		t.Fatalf("document export_mime = %#v, want %#v", got, want)
	}
}

func collectGoogleWorkspaceReadbackFacts(t *testing.T) []facts.Envelope {
	t.Helper()

	result, err := googleworkspace.Collect(context.Background(), googleworkspace.Request{
		ScopeID:      "doc-source:google_workspace:synthetic",
		GenerationID: "gws-gen-readback",
		ObservedAt:   time.Date(2026, time.June, 9, 14, 0, 0, 0, time.UTC),
		Allowlist: googleworkspace.Allowlist{
			FileIDs: []string{"gws-doc-readback"},
		},
		Client:         googleWorkspaceReadbackClient{},
		MaxExportBytes: 1024,
	})
	if err != nil {
		t.Fatalf("googleworkspace.Collect() error = %v, want nil", err)
	}
	return result.Envelopes
}

type googleWorkspaceReadbackClient struct{}

func (googleWorkspaceReadbackClient) ListFiles(context.Context, googleworkspace.Allowlist) ([]googleworkspace.File, error) {
	return []googleworkspace.File{{
		ID:         "gws-doc-readback",
		Kind:       googleworkspace.FileKindDocument,
		RevisionID: "rev-readback",
		WebURL:     "gws://file/gws-doc-readback?token=synthetic-secret",
	}}, nil
}

func (googleWorkspaceReadbackClient) PermissionSummary(context.Context, string) (googleworkspace.PermissionSummary, error) {
	return googleworkspace.PermissionSummary{
		Visibility:   "restricted",
		ReaderGroups: []string{"group:synthetic-readers"},
	}, nil
}

func (googleWorkspaceReadbackClient) Export(context.Context, string, string) (googleworkspace.Export, error) {
	return googleworkspace.Export{
		Bytes: []byte("docx-bytes"),
		Sections: []googleworkspace.Section{{
			ID:      "body",
			Heading: "Runbook",
			Content: "Synthetic workspace runbook",
		}},
		Links: []googleworkspace.Link{{
			ID:        "service-link",
			SectionID: "body",
			TargetURI: "service://synthetic-workload",
			Anchor:    "synthetic workload",
		}},
	}, nil
}

func googleWorkspaceFactRows(t *testing.T, envelopes []facts.Envelope) [][]any {
	t.Helper()

	rows := make([][]any, 0, len(envelopes))
	for _, envelope := range envelopes {
		payload, err := marshalPayload(envelope.Payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		rows = append(rows, []any{
			envelope.FactID,
			envelope.ScopeID,
			envelope.GenerationID,
			envelope.FactKind,
			envelope.StableFactKey,
			envelope.SchemaVersion,
			envelope.CollectorKind,
			envelope.FencingToken,
			envelope.SourceConfidence,
			envelope.SourceRef.SourceSystem,
			envelope.SourceRef.FactKey,
			envelope.SourceRef.SourceURI,
			envelope.SourceRef.SourceRecordID,
			envelope.ObservedAt,
			false,
			payload,
		})
	}
	return rows
}

func assertGoogleWorkspaceFactKinds(t *testing.T, envelopes []facts.Envelope, want map[string]int) {
	t.Helper()

	got := map[string]int{}
	for _, envelope := range envelopes {
		got[envelope.FactKind]++
	}
	for kind, count := range want {
		if got[kind] != count {
			t.Fatalf("fact kind %q count = %d, want %d", kind, got[kind], count)
		}
	}
	for _, forbidden := range []string{
		facts.DocumentationEntityMentionFactKind,
		facts.DocumentationClaimCandidateFactKind,
	} {
		if got[forbidden] != 0 {
			t.Fatalf("fact kind %q count = %d, want 0", forbidden, got[forbidden])
		}
	}
}

func googleWorkspaceLoadedPayload(t *testing.T, envelopes []facts.Envelope, kind string) map[string]any {
	t.Helper()

	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			return envelope.Payload
		}
	}
	t.Fatalf("missing loaded fact kind %q", kind)
	return nil
}
