package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingSBOMAttestationAttachmentStore struct {
	rows       []SBOMAttestationAttachmentRow
	lastFilter SBOMAttestationAttachmentFilter
}

func (s *recordingSBOMAttestationAttachmentStore) ListSBOMAttestationAttachments(
	_ context.Context,
	filter SBOMAttestationAttachmentFilter,
) ([]SBOMAttestationAttachmentRow, error) {
	s.lastFilter = filter
	return append([]SBOMAttestationAttachmentRow(nil), s.rows...), nil
}

func TestSupplyChainListSBOMAttestationAttachmentsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SBOMAttachments: &recordingSBOMAttestationAttachmentStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/sbom-attestations/attachments?limit=10",
		"/api/v0/supply-chain/sbom-attestations/attachments?subject_digest=sha256:abc",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestSupplyChainListSBOMAttestationAttachmentsUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingSBOMAttestationAttachmentStore{
		rows: []SBOMAttestationAttachmentRow{
			{
				AttachmentID:       "attachment-1",
				SubjectDigest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				DocumentID:         "doc-1",
				DocumentDigest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				AttachmentStatus:   "attached_verified",
				ParseStatus:        "parsed",
				VerificationStatus: "passed",
				VerificationPolicy: "policy://prod",
				ArtifactKind:       "sbom",
				Format:             "cyclonedx",
				SpecVersion:        "1.6",
				ComponentCount:     3,
				ComponentEvidence:  []ComponentEvidenceRow{{ComponentID: "pkg:npm/example@1.0.0", PURL: "pkg:npm/example@1.0.0"}},
				WarningSummaries:   []string{"none"},
				CanonicalWrites:    1,
				EvidenceFactIDs:    []string{"doc-fact", "referrer-fact"},
				SourceFreshness:    "active",
				SourceConfidence:   "inferred",
			},
			{AttachmentID: "attachment-2", AttachmentStatus: "attached_parse_only"},
		},
	}
	handler := &SupplyChainHandler{SBOMAttachments: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments?subject_digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.SubjectDigest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("SubjectDigest = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Attachments []SBOMAttestationAttachmentResult `json:"attachments"`
		Count       int                               `json:"count"`
		Limit       int                               `json:"limit"`
		Truncated   bool                              `json:"truncated"`
		NextCursor  map[string]string                 `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Attachments), 1; got != want {
		t.Fatalf("len(attachments) = %d, want %d", got, want)
	}
	if got, want := resp.Attachments[0].VerificationStatus, "passed"; got != want {
		t.Fatalf("VerificationStatus = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_attachment_id"], "attachment-1"; got != want {
		t.Fatalf("next_cursor.after_attachment_id = %q, want %q", got, want)
	}
}

func TestSBOMAttestationAttachmentQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"scope.active_generation_id = fact.generation_id",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'subject_digest' = $2",
		"fact.payload->>'document_id' = $3",
		"fact.payload->>'attachment_status' = $5",
	} {
		if !strings.Contains(listSBOMAttestationAttachmentsQuery, want) {
			t.Fatalf("listSBOMAttestationAttachmentsQuery missing %q:\n%s", want, listSBOMAttestationAttachmentsQuery)
		}
	}
}
