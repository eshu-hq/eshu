// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingSBOMAttestationAttachmentStore struct {
	page       SBOMAttestationAttachmentPage
	lastFilter SBOMAttestationAttachmentFilter
	calls      int
}

func (s *recordingSBOMAttestationAttachmentStore) ListSBOMAttestationAttachments(
	_ context.Context,
	filter SBOMAttestationAttachmentFilter,
) (SBOMAttestationAttachmentPage, error) {
	s.calls++
	s.lastFilter = filter
	page := s.page
	page.Attachments = append([]SBOMAttestationAttachmentRow(nil), s.page.Attachments...)
	page.MissingEvidence = append([]string(nil), s.page.MissingEvidence...)
	return page, nil
}

func TestSupplyChainListSBOMAttestationAttachmentsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SBOMAttachments: &recordingSBOMAttestationAttachmentStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/sbom-attestations/attachments?limit=10",
		"/api/v0/supply-chain/sbom-attestations/attachments?subject_digest=sha256:abc",
		"/api/v0/supply-chain/sbom-attestations/attachments?image_digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=10",
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
		page: SBOMAttestationAttachmentPage{
			Attachments: []SBOMAttestationAttachmentRow{
				{
					AttachmentID:               "attachment-1",
					SubjectDigest:              "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					DocumentID:                 "doc-1",
					DocumentDigest:             "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					AttachmentStatus:           "attached_verified",
					ParseStatus:                "parsed",
					VerificationStatus:         "passed",
					VerificationPolicy:         "policy://prod",
					ArtifactKind:               "sbom",
					Format:                     "cyclonedx",
					SpecVersion:                "1.6",
					AttachmentScope:            "image_subject",
					ComponentCount:             3,
					ComponentEvidence:          []ComponentEvidenceRow{{ComponentID: "pkg:npm/example@1.0.0", PURL: "pkg:npm/example@1.0.0"}},
					ComponentEvidenceTruncated: true,
					WarningSummaries:           []string{"none"},
					CanonicalWrites:            1,
					EvidenceFactIDs:            []string{"doc-fact", "referrer-fact"},
					RepositoryIDs:              []string{"repo://example/api"},
					WorkloadIDs:                []string{"workload:example-api"},
					ServiceIDs:                 []string{"service:example-api"},
					SourceFreshness:            "active",
					SourceConfidence:           "inferred",
				},
				{AttachmentID: "attachment-2", AttachmentStatus: "attached_parse_only"},
			},
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
	if got, want := resp.Attachments[0].AttachmentScope, "image_subject"; got != want {
		t.Fatalf("AttachmentScope = %q, want %q", got, want)
	}
	if got, want := resp.Attachments[0].CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if !resp.Attachments[0].ComponentEvidenceTruncated {
		t.Fatal("component_evidence_truncated = false, want true")
	}
	if got, want := resp.Attachments[0].RepositoryIDs[0], "repo://example/api"; got != want {
		t.Fatalf("RepositoryIDs[0] = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_attachment_id"], "attachment-1"; got != want {
		t.Fatalf("next_cursor.after_attachment_id = %q, want %q", got, want)
	}
}

func TestSupplyChainListSBOMAttestationAttachmentsAcceptsWorkloadServiceAnchors(t *testing.T) {
	t.Parallel()

	store := &recordingSBOMAttestationAttachmentStore{
		page: SBOMAttestationAttachmentPage{
			Attachments:     []SBOMAttestationAttachmentRow{{AttachmentID: "attachment-1", AttachmentStatus: "attached_parse_only"}},
			MissingEvidence: []string{"image_to_sbom_evidence_missing"},
		},
	}

	handler := &SupplyChainHandler{SBOMAttachments: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments?workload_id=workload:example-api&service_id=service:example-api&digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.WorkloadID, "workload:example-api"; got != want {
		t.Fatalf("WorkloadID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ServiceID, "service:example-api"; got != want {
		t.Fatalf("ServiceID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.SubjectDigest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("SubjectDigest = %q, want %q", got, want)
	}

	var resp struct {
		MissingEvidence []string `json:"missing_evidence"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.MissingEvidence[0], "image_to_sbom_evidence_missing"; got != want {
		t.Fatalf("missing_evidence[0] = %q, want %q", got, want)
	}
}

func TestSupplyChainListSBOMAttestationAttachmentsAcceptsRepositoryScope(t *testing.T) {
	t.Parallel()

	store := &recordingSBOMAttestationAttachmentStore{
		page: SBOMAttestationAttachmentPage{
			Attachments:     []SBOMAttestationAttachmentRow{{AttachmentID: "attachment-1", AttachmentStatus: "attached_verified"}},
			MissingEvidence: []string{"repository_to_image_evidence_missing"},
		},
	}
	handler := &SupplyChainHandler{SBOMAttachments: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	var resp struct {
		Attachments     []SBOMAttestationAttachmentResult `json:"attachments"`
		MissingEvidence []string                          `json:"missing_evidence"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Attachments), 1; got != want {
		t.Fatalf("len(attachments) = %d, want %d", got, want)
	}
	if got, want := resp.MissingEvidence[0], "repository_to_image_evidence_missing"; got != want {
		t.Fatalf("missing_evidence[0] = %q, want %q", got, want)
	}
}

func TestSupplyChainListSBOMAttestationAttachmentsBoundsWarningSummaryPreview(t *testing.T) {
	t.Parallel()

	warnings := repeatedSBOMAttachmentWarnings(256, "lockfile parse warning")
	store := &recordingSBOMAttestationAttachmentStore{
		page: SBOMAttestationAttachmentPage{
			Attachments: []SBOMAttestationAttachmentRow{
				{
					AttachmentID:       "attachment-many-warnings",
					DocumentID:         "doc-many-warnings",
					AttachmentStatus:   "unparseable",
					ParseStatus:        "parse_failed",
					ArtifactKind:       "sbom",
					WarningSummaries:   warnings,
					SourceFreshness:    "active",
					SourceConfidence:   "reported",
					EvidenceFactIDs:    []string{"warning-fact"},
					MissingEvidence:    []string{"parseable_document"},
					AttachmentScope:    "parse_only_unanchored",
					CanonicalWrites:    0,
					ComponentCount:     0,
					DocumentDigest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					SubjectDigest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					VerificationPolicy: "not_configured",
				},
			},
		},
	}
	handler := &SupplyChainHandler{SBOMAttachments: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments?document_id=doc-many-warnings&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Attachments []struct {
			WarningSummaries          []string `json:"warning_summaries"`
			WarningSummaryCount       int      `json:"warning_summary_count"`
			WarningSummariesTruncated bool     `json:"warning_summaries_truncated"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Attachments), 1; got != want {
		t.Fatalf("len(attachments) = %d, want %d", got, want)
	}
	row := resp.Attachments[0]
	if got, want := row.WarningSummaryCount, len(warnings); got != want {
		t.Fatalf("warning_summary_count = %d, want %d", got, want)
	}
	if !row.WarningSummariesTruncated {
		t.Fatal("warning_summaries_truncated = false, want true")
	}
	if got, want := strings.Join(row.WarningSummaries, ","), "lockfile parse warning"; got != want {
		t.Fatalf("warning_summaries preview = %q, want %q", got, want)
	}
	if got, want := strings.Count(w.Body.String(), "lockfile parse warning"), 1; got != want {
		t.Fatalf("response warning occurrence count = %d, want %d; body = %s", got, want, w.Body.String())
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
		"fact.payload->'repository_ids' ? $7",
		"fact.payload->'workload_ids' ? $8",
		"fact.payload->'service_ids' ? $9",
	} {
		if !strings.Contains(listSBOMAttestationAttachmentsQuery, want) {
			t.Fatalf("listSBOMAttestationAttachmentsQuery missing %q:\n%s", want, listSBOMAttestationAttachmentsQuery)
		}
	}
}

func TestDecodeSBOMAttestationAttachmentRowPreservesAnchorTruth(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"document_id":         "doc-parse-only",
		"subject_digest":      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"attachment_status":   "attached_parse_only",
		"attachment_scope":    "parse_only_unanchored",
		"canonical_writes":    0,
		"missing_evidence":    []string{"image_referrer_evidence", "repository_attachment_evidence"},
		"component_count":     1,
		"verification_status": "not_configured",
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSBOMAttestationAttachmentRow("attachment-parse-only", "inferred", payloadBytes)
	if err != nil {
		t.Fatalf("decodeSBOMAttestationAttachmentRow() error = %v", err)
	}
	if got, want := row.AttachmentScope, "parse_only_unanchored"; got != want {
		t.Fatalf("AttachmentScope = %q, want %q", got, want)
	}
	if got, want := strings.Join(row.MissingEvidence, ","), "image_referrer_evidence,repository_attachment_evidence"; got != want {
		t.Fatalf("MissingEvidence = %q, want %q", got, want)
	}
	if got, want := row.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
}

func TestDecodeSBOMAttestationAttachmentRowUsesPersistedWarningSummaryCount(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"document_id":           "doc-aggregated-warnings",
		"attachment_status":     "attached_parse_only",
		"warning_summaries":     []string{"25 components missing purl and name+version identity (samples: component[1], component[2])"},
		"warning_summary_count": 25,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSBOMAttestationAttachmentRow("attachment-aggregated-warnings", "reported", payloadBytes)
	if err != nil {
		t.Fatalf("decodeSBOMAttestationAttachmentRow() error = %v", err)
	}
	if got, want := row.WarningSummaryCount, 25; got != want {
		t.Fatalf("WarningSummaryCount = %d, want %d", got, want)
	}
	if !row.WarningSummariesTruncated {
		t.Fatal("WarningSummariesTruncated = false, want true for aggregated occurrence count")
	}
	if got, want := strings.Join(row.WarningSummaries, ","), "25 components missing purl and name+version identity (samples: component[1], component[2])"; got != want {
		t.Fatalf("WarningSummaries = %q, want %q", got, want)
	}
}

func TestDecodeSBOMAttestationAttachmentRowSurfacesComponentEvidenceTruncation(t *testing.T) {
	t.Parallel()

	components := make([]any, 0, 100)
	for i := 0; i < 100; i++ {
		components = append(components, map[string]any{
			"component_id": fmt.Sprintf("component-%03d", i),
			"fact_id":      fmt.Sprintf("fact-%03d", i),
		})
	}
	payloadBytes, err := json.Marshal(map[string]any{
		"document_id":        "doc-bounded-components",
		"attachment_status":  "attached_parse_only",
		"component_count":    101,
		"component_evidence": components,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSBOMAttestationAttachmentRow("attachment-bounded-components", "reported", payloadBytes)
	if err != nil {
		t.Fatalf("decodeSBOMAttestationAttachmentRow() error = %v", err)
	}
	if got, want := len(row.ComponentEvidence), 100; got != want {
		t.Fatalf("ComponentEvidence len = %d, want %d", got, want)
	}
	if !row.ComponentEvidenceTruncated {
		t.Fatal("ComponentEvidenceTruncated = false, want true when full count exceeds persisted rows")
	}
}

func TestSBOMAttestationAttachmentMissingEvidenceQueryExplainsScopedGaps(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"reducer_container_image_identity",
		"source_repository_ids",
		"fact.payload->>'outcome' IN ('exact_digest', 'tag_resolved')",
		"missing_image",
		"missing_attachment",
	} {
		if !strings.Contains(sbomAttestationAttachmentMissingEvidenceQuery, want) {
			t.Fatalf("sbomAttestationAttachmentMissingEvidenceQuery missing %q:\n%s", want, sbomAttestationAttachmentMissingEvidenceQuery)
		}
	}
}

func repeatedSBOMAttachmentWarnings(count int, summary string) []string {
	warnings := make([]string, count)
	for i := range warnings {
		warnings[i] = summary
	}
	return warnings
}
