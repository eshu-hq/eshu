package googleworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCollectEmitsMetadataOnlyFailureDocuments(t *testing.T) {
	t.Parallel()

	files := []File{
		testFile("missing-permission", FileKindDocument),
		testFile("deleted-file", FileKindDocument),
		testFile("trashed-file", FileKindSpreadsheet),
		testFile("stale-file", FileKindPresentation),
		testFile("rate-limited-file", FileKindDocument),
		testFile("quota-file", FileKindSpreadsheet),
		testFile("download-denied-file", FileKindPresentation),
	}
	files[1].Deleted = true
	files[2].Trashed = true
	client := newFakeClient(files)
	client.permissions["missing-permission"] = permissionResult{err: FailurePermissionDenied}
	client.exportErrors["rate-limited-file"] = FailureProviderRateLimited
	client.exportErrors["quota-file"] = FailureProviderQuotaExceeded
	client.exportErrors["download-denied-file"] = FailureDownloadNotAllowed

	req := testRequest(client, Allowlist{FileIDs: []string{
		"missing-permission",
		"deleted-file",
		"trashed-file",
		"stale-file",
		"rate-limited-file",
		"quota-file",
		"download-denied-file",
	}})
	req.ExpectedRevisions = map[string]string{"stale-file": "newer-revision"}
	result, err := Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil partial sync", err)
	}

	for fileID, wantClass := range map[string]FailureClass{
		"missing-permission":   FailurePermissionDenied,
		"deleted-file":         FailureSourceDeleted,
		"trashed-file":         FailureSourceTrashed,
		"stale-file":           FailureSourceRevisionStale,
		"rate-limited-file":    FailureProviderRateLimited,
		"quota-file":           FailureProviderQuotaExceeded,
		"download-denied-file": FailureDownloadNotAllowed,
	} {
		documentID := "doc:google_workspace:" + safeFingerprint(fileID)
		document := payloadByKindAndID(t, result.Envelopes, facts.DocumentationDocumentFactKind, "document_id", documentID)
		metadata := stringMapValue(t, document, "source_metadata")
		if got := metadata["failure_class"]; got != string(wantClass) {
			t.Fatalf("file %q failure_class = %q, want %q", fileID, got, wantClass)
		}
		if got := countSectionsForDocument(result.Envelopes, documentID); got != 0 {
			t.Fatalf("file %q section count = %d, want 0", fileID, got)
		}
	}

	source := payloadByKind(t, result.Envelopes, facts.DocumentationSourceFactKind)
	metadata := stringMapValue(t, source, "source_metadata")
	if got, want := metadata["sync_status"], "partial"; got != want {
		t.Fatalf("sync_status = %q, want %q", got, want)
	}
	if got, want := metadata["failure_count"], "7"; got != want {
		t.Fatalf("failure_count = %q, want %q", got, want)
	}
}

func TestCollectPreservesACLPartialAsBoundedMetadata(t *testing.T) {
	t.Parallel()

	client := newFakeClient([]File{testFile("acl-partial-file", FileKindSpreadsheet)})
	client.permissions["acl-partial-file"] = permissionResult{summary: PermissionSummary{
		Visibility:    "restricted",
		ReaderGroups:  []string{"group:spreadsheet-readers"},
		ReaderUsers:   []string{"person@example.invalid"},
		IsPartial:     true,
		PartialReason: string(FailureACLPartial),
	}}
	client.exports["acl-partial-file"] = Export{Bytes: []byte("xlsx-bytes"), Sections: []Section{{
		ID:      "visible-sheet",
		Heading: "Visible sheet",
		Content: "Synthetic visible rows",
	}}}

	result, err := Collect(context.Background(), testRequest(client, Allowlist{FileIDs: []string{"acl-partial-file"}}))
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	document := payloadByKindAndID(
		t,
		result.Envelopes,
		facts.DocumentationDocumentFactKind,
		"document_id",
		"doc:google_workspace:"+safeFingerprint("acl-partial-file"),
	)
	acl := mapValue(t, document, "acl_summary")
	if got, want := acl["visibility"], "restricted"; got != want {
		t.Fatalf("acl visibility = %#v, want %#v", got, want)
	}
	if got, want := acl["is_partial"], true; got != want {
		t.Fatalf("acl is_partial = %#v, want %#v", got, want)
	}
	readerUsers := stringSliceValue(t, acl, "reader_users")
	for _, user := range readerUsers {
		if strings.Contains(user, "@") || strings.Contains(user, "person") {
			t.Fatalf("reader user leaked raw principal: %#v", readerUsers)
		}
	}
}

func TestCollectClassifiesExportOverLimit(t *testing.T) {
	t.Parallel()

	client := newFakeClient([]File{testFile("oversize-file", FileKindDocument)})
	client.exports["oversize-file"] = Export{
		Bytes:    []byte("this export is larger than the configured byte limit"),
		Sections: []Section{{ID: "body", Content: "Synthetic body"}},
	}
	req := testRequest(client, Allowlist{FileIDs: []string{"oversize-file"}})
	req.MaxExportBytes = 8

	result, err := Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil partial sync", err)
	}
	document := payloadByKindAndID(
		t,
		result.Envelopes,
		facts.DocumentationDocumentFactKind,
		"document_id",
		"doc:google_workspace:"+safeFingerprint("oversize-file"),
	)
	metadata := stringMapValue(t, document, "source_metadata")
	if got := metadata["failure_class"]; got != string(FailureResourceLimitExceeded) {
		t.Fatalf("failure_class = %q, want %q", got, FailureResourceLimitExceeded)
	}
	if got := countKind(result.Envelopes, facts.DocumentationSectionFactKind); got != 0 {
		t.Fatalf("section count = %d, want 0 for over-limit export", got)
	}
}

func TestCollectPreservesExportFormatFailureClass(t *testing.T) {
	t.Parallel()

	client := newFakeClient([]File{testFile("unsupported-export-file", FileKindDocument)})
	client.exportErrors["unsupported-export-file"] = FailureExportFormatUnsupported

	result, err := Collect(context.Background(), testRequest(client, Allowlist{FileIDs: []string{"unsupported-export-file"}}))
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil partial sync", err)
	}
	document := payloadByKindAndID(
		t,
		result.Envelopes,
		facts.DocumentationDocumentFactKind,
		"document_id",
		"doc:google_workspace:"+safeFingerprint("unsupported-export-file"),
	)
	metadata := stringMapValue(t, document, "source_metadata")
	if got := metadata["failure_class"]; got != string(FailureExportFormatUnsupported) {
		t.Fatalf("failure_class = %q, want %q", got, FailureExportFormatUnsupported)
	}
}

func TestCollectRedactsProviderIdentifiersAndPrivateURLs(t *testing.T) {
	t.Parallel()

	client := newFakeClient([]File{{
		ID:         "raw-drive-file-id",
		Kind:       FileKindDocument,
		Name:       "Synthetic Private Document",
		RevisionID: "rev-private",
		WebURL:     "https://tenant.example.invalid/doc/raw-drive-file-id?token=secret-marker",
	}})
	client.permissions["raw-drive-file-id"] = permissionResult{summary: PermissionSummary{
		Visibility:    "restricted",
		ReaderGroups:  []string{"tenant.example.invalid"},
		ReaderUsers:   []string{"person@example.invalid"},
		PartialReason: "https://tenant.example.invalid/private?token=secret-marker",
	}}
	client.exports["raw-drive-file-id"] = Export{
		Bytes: []byte("docx-bytes"),
		Links: []Link{{
			ID:        "private-link",
			SectionID: "body",
			TargetURI: "https://tenant.example.invalid/private?token=secret-marker",
			Anchor:    "private",
		}},
		Sections: []Section{{ID: "body", Content: "Synthetic body"}},
	}

	req := testRequest(client, Allowlist{FileIDs: []string{"raw-drive-file-id"}})
	req.SourceName = "tenant.example.invalid workspace"
	result, err := Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	encoded, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	for _, disallowed := range []string{"raw-drive-file-id", "person@example.invalid", "tenant.example.invalid", "token=secret-marker"} {
		if strings.Contains(string(encoded), disallowed) {
			t.Fatalf("envelopes leaked %q: %s", disallowed, encoded)
		}
	}
}

func TestCollectReturnsListFailureClass(t *testing.T) {
	t.Parallel()

	client := newFakeClient(nil)
	client.listErr = FailureProviderRateLimited
	_, err := Collect(context.Background(), testRequest(client, Allowlist{FileIDs: []string{"rate-limited-list"}}))
	if !errors.Is(err, FailureProviderRateLimited) {
		t.Fatalf("Collect() error = %v, want %s", err, FailureProviderRateLimited)
	}
}
