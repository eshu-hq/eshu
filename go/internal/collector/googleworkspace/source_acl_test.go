package googleworkspace

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourceACLStateMapsObservedPosture(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		permission PermissionSummary
		failure    FailureClass
		want       string
	}{
		{name: "permission denied", failure: FailurePermissionDenied, want: facts.SourceACLStateDenied},
		{name: "download not allowed", failure: FailureDownloadNotAllowed, want: facts.SourceACLStateDenied},
		{name: "source deleted", failure: FailureSourceDeleted, want: facts.SourceACLStateMissing},
		{name: "source trashed", failure: FailureSourceTrashed, want: facts.SourceACLStateMissing},
		{name: "revision stale", failure: FailureSourceRevisionStale, want: facts.SourceACLStateStale},
		{name: "acl partial failure", failure: FailureACLPartial, want: facts.SourceACLStatePartial},
		{name: "permission partial", permission: PermissionSummary{IsPartial: true}, want: facts.SourceACLStatePartial},
		{name: "clean read omits state", permission: PermissionSummary{Visibility: "restricted"}, want: ""},
		{name: "unrelated failure omits state", failure: FailureProviderRateLimited, want: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sourceACLState(tc.permission, tc.failure)
			if got != tc.want {
				t.Fatalf("sourceACLState = %q, want %q", got, tc.want)
			}
			// Fail-closed: never assert allowed from this provider.
			if got == facts.SourceACLStateAllowed {
				t.Fatalf("sourceACLState must never upgrade to allowed")
			}
		})
	}
}

func TestCollectEmitsBoundedSourceACLStateOnFailureDocuments(t *testing.T) {
	t.Parallel()

	files := []File{
		testFile("denied-file", FileKindDocument),
		testFile("deleted-file", FileKindDocument),
		testFile("stale-file", FileKindPresentation),
		testFile("clean-file", FileKindSpreadsheet),
	}
	files[1].Deleted = true
	client := newFakeClient(files)
	client.permissions["denied-file"] = permissionResult{err: FailurePermissionDenied}
	client.exports["clean-file"] = Export{Bytes: []byte("xlsx"), Sections: []Section{{ID: "s", Heading: "h", Content: "c"}}}

	req := testRequest(client, Allowlist{FileIDs: []string{"denied-file", "deleted-file", "stale-file", "clean-file"}})
	req.ExpectedRevisions = map[string]string{"stale-file": "newer-revision"}
	result, err := Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	want := map[string]string{
		"denied-file":  facts.SourceACLStateDenied,
		"deleted-file": facts.SourceACLStateMissing,
		"stale-file":   facts.SourceACLStateStale,
	}
	for fileID, wantState := range want {
		documentID := "doc:google_workspace:" + safeFingerprint(fileID)
		document := payloadByKindAndID(t, result.Envelopes, facts.DocumentationDocumentFactKind, "document_id", documentID)
		acl := mapValue(t, document, "acl_summary")
		if got := acl["source_acl_state"]; got != wantState {
			t.Fatalf("%s source_acl_state = %#v, want %q", fileID, got, wantState)
		}
	}

	// A clean read of an ACL'd source whose restrictions are not collected must
	// omit the field entirely (no policy guess, never upgraded to allowed).
	cleanDoc := payloadByKindAndID(t, result.Envelopes, facts.DocumentationDocumentFactKind, "document_id", "doc:google_workspace:"+safeFingerprint("clean-file"))
	cleanACL := mapValue(t, cleanDoc, "acl_summary")
	if _, present := cleanACL["source_acl_state"]; present {
		t.Fatalf("clean read must omit source_acl_state, got %#v", cleanACL["source_acl_state"])
	}
}
