package googleworkspace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCollectExportsDocsSheetsSlidesFromExplicitFileAllowlist(t *testing.T) {
	t.Parallel()

	client := newFakeClient([]File{
		testFile("doc-1", FileKindDocument),
		testFile("sheet-1", FileKindSpreadsheet),
		testFile("slide-1", FileKindPresentation),
	})
	client.exports["doc-1"] = Export{Bytes: []byte("docx-bytes"), Sections: []Section{{ID: "body", Content: "Synthetic doc section"}}}
	client.exports["sheet-1"] = Export{Bytes: []byte("xlsx-bytes"), Sections: []Section{{ID: "sheet-a", Content: "Synthetic sheet row"}}}
	client.exports["slide-1"] = Export{Bytes: []byte("pptx-bytes"), Sections: []Section{{ID: "slide-1", Content: "Synthetic slide text"}}}

	result, err := Collect(context.Background(), testRequest(client, Allowlist{
		FileIDs: []string{"doc-1", "sheet-1", "slide-1"},
	}))
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got, want := len(client.listRequests), 1; got != want {
		t.Fatalf("list request count = %d, want %d", got, want)
	}
	if got, want := client.listRequests[0].FileIDs, []string{"doc-1", "sheet-1", "slide-1"}; !equalStrings(got, want) {
		t.Fatalf("FileIDs = %#v, want %#v", got, want)
	}
	for _, want := range []string{
		ExportMIMEDOCX,
		ExportMIMEXLSX,
		ExportMIMEPPTX,
	} {
		if !containsString(client.exportMIMEs, want) {
			t.Fatalf("export MIME calls = %#v, want %q", client.exportMIMEs, want)
		}
	}
	assertFactKindCount(t, result.Envelopes, facts.DocumentationSourceFactKind, 1)
	assertFactKindCount(t, result.Envelopes, facts.DocumentationDocumentFactKind, 3)
	assertFactKindCount(t, result.Envelopes, facts.DocumentationSectionFactKind, 3)
	for _, envelope := range result.Envelopes {
		switch envelope.FactKind {
		case facts.DocumentationEntityMentionFactKind, facts.DocumentationClaimCandidateFactKind:
			t.Fatalf("unexpected truth fact from Google Workspace evidence: %s", envelope.FactKind)
		}
	}

	document := payloadByKindAndID(t, result.Envelopes, facts.DocumentationDocumentFactKind, "document_id", "doc:google_workspace:"+safeFingerprint("doc-1"))
	metadata := stringMapValue(t, document, "source_metadata")
	if got, want := metadata["export_mime"], ExportMIMEDOCX; got != want {
		t.Fatalf("document export_mime = %q, want %q", got, want)
	}
	if got, want := document["external_id"], "gws-file:"+safeFingerprint("doc-1"); got != want {
		t.Fatalf("external_id = %#v, want %#v", got, want)
	}
	section := payloadByKindAndID(t, result.Envelopes, facts.DocumentationSectionFactKind, "section_id", "export:body")
	if got, want := section["content"], "Synthetic doc section"; got != want {
		t.Fatalf("section.content = %#v, want %#v", got, want)
	}
}

func TestCollectSupportsFolderAndSharedDriveAllowlists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		allowlist     Allowlist
		wantKind      string
		wantFolderIDs []string
		wantDriveIDs  []string
	}{
		{
			name:          "folder_allowlist",
			allowlist:     Allowlist{FolderIDs: []string{"folder-1"}},
			wantKind:      "folder",
			wantFolderIDs: []string{"folder-1"},
		},
		{
			name:         "shared_drive_allowlist",
			allowlist:    Allowlist{SharedDriveIDs: []string{"drive-1"}, SharedDriveQuery: "mimeType=documentation"},
			wantKind:     "shared_drive",
			wantDriveIDs: []string{"drive-1"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newFakeClient([]File{testFile("doc-1", FileKindDocument)})
			client.exports["doc-1"] = Export{Bytes: []byte("docx-bytes"), Sections: []Section{{ID: "body", Content: "Synthetic text"}}}
			result, err := Collect(context.Background(), testRequest(client, tt.allowlist))
			if err != nil {
				t.Fatalf("Collect() error = %v, want nil", err)
			}
			if got := client.listRequests[0].FolderIDs; !equalStrings(got, tt.wantFolderIDs) {
				t.Fatalf("FolderIDs = %#v, want %#v", got, tt.wantFolderIDs)
			}
			if got := client.listRequests[0].SharedDriveIDs; !equalStrings(got, tt.wantDriveIDs) {
				t.Fatalf("SharedDriveIDs = %#v, want %#v", got, tt.wantDriveIDs)
			}
			source := payloadByKind(t, result.Envelopes, facts.DocumentationSourceFactKind)
			metadata := stringMapValue(t, source, "source_metadata")
			if got := metadata["allowlist_kind"]; got != tt.wantKind {
				t.Fatalf("allowlist_kind = %q, want %q", got, tt.wantKind)
			}
		})
	}
}

func TestCollectRejectsBlankOrUnboundedAllowlist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		allowlist Allowlist
		wantClass FailureClass
	}{
		{name: "blank", wantClass: FailureAllowlistEmpty},
		{name: "all_drive", allowlist: Allowlist{AllowAllDrive: true}, wantClass: FailureAllowlistUnsupportedScope},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newFakeClient(nil)
			result, err := Collect(context.Background(), testRequest(client, tt.allowlist))
			if !errors.Is(err, tt.wantClass) {
				t.Fatalf("Collect() error = %v, want class %s", err, tt.wantClass)
			}
			if len(result.Envelopes) != 0 {
				t.Fatalf("envelopes = %#v, want none for rejected allowlist", result.Envelopes)
			}
			if len(client.listRequests) != 0 {
				t.Fatalf("client list calls = %d, want 0", len(client.listRequests))
			}
		})
	}
}

func testRequest(client Client, allowlist Allowlist) Request {
	return Request{
		ScopeID:        "doc-source:google_workspace:synthetic",
		GenerationID:   "gws-gen-1",
		ObservedAt:     time.Date(2026, time.June, 9, 13, 0, 0, 0, time.UTC),
		SourceName:     "Synthetic Workspace",
		Allowlist:      allowlist,
		Client:         client,
		MaxExportBytes: 64,
	}
}
