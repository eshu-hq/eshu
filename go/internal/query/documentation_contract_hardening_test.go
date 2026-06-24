// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestContentReaderDocumentationFindingsDisclosesDeniedVisibility proves the
// approved disclosure policy (#2164): a finding the caller cannot read is no
// longer silently dropped. It is returned with an access-denied disposition,
// permission_denied + content_withheld set, and its protected content
// (summary/permissions denied_reason) stripped — while a readable finding is
// returned intact alongside it. A reader can now distinguish "no evidence" from
// "evidence exists but is denied."
func TestContentReaderDocumentationFindingsDisclosesDeniedVisibility(t *testing.T) {
	t.Parallel()

	hidden := []byte(`{
		"finding_id": "finding:hidden",
		"finding_type": "service_deployment_drift",
		"status": "conflict",
		"source_id": "doc-source:secret",
		"document_id": "doc:secret:1",
		"summary": "private deployment drift",
		"permissions": {
			"viewer_can_read_source": false,
			"denied_reason": "caller cannot read source document"
		}
	}`)
	visible := []byte(`{
		"finding_id": "finding:visible",
		"finding_type": "service_deployment_drift",
		"status": "conflict",
		"source_id": "doc-source:public",
		"document_id": "doc:public:1",
		"summary": "public deployment drift",
		"permissions": {
			"viewer_can_read_source": true
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{hidden}, {visible}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationFindings(t.Context(), documentationFindingFilter{
		FindingType: "service_deployment_drift",
		Limit:       50,
	})
	if err != nil {
		t.Fatalf("documentationFindings() error = %v, want nil", err)
	}
	if gotLen, want := len(got.Findings), 2; gotLen != want {
		t.Fatalf("len(Findings) = %d, want %d (denied row disclosed, not dropped); findings = %#v", gotLen, want, got.Findings)
	}
	denied := got.Findings[0]
	if denied["finding_id"] != "finding:hidden" {
		t.Fatalf("findings[0].finding_id = %#v, want denied row 'finding:hidden'", denied["finding_id"])
	}
	if denied[accessDispositionResponseKey] != accessDispositionDenied {
		t.Fatalf("denied access_disposition = %#v, want %q", denied[accessDispositionResponseKey], accessDispositionDenied)
	}
	if denied[permissionDeniedResponseKey] != true {
		t.Fatalf("denied permission_denied = %#v, want true", denied[permissionDeniedResponseKey])
	}
	if denied[contentWithheldResponseKey] != true {
		t.Fatalf("denied content_withheld = %#v, want true", denied[contentWithheldResponseKey])
	}
	if _, leaked := denied["summary"]; leaked {
		t.Fatalf("denied row leaked content 'summary': %#v", denied)
	}
	if _, leaked := denied["permissions"]; leaked {
		t.Fatalf("denied row leaked 'permissions' object (may contain denied_reason): %#v", denied)
	}
	if _, leaked := denied["evidence_packet_url"]; leaked {
		t.Fatalf("denied row leaked evidence_packet_url to protected packet: %#v", denied)
	}
	if got.Findings[1]["finding_id"] != "finding:visible" {
		t.Fatalf("findings[1].finding_id = %#v, want 'finding:visible'", got.Findings[1]["finding_id"])
	}
	if got.Findings[1]["summary"] != "public deployment drift" {
		t.Fatalf("visible row lost its content: %#v", got.Findings[1])
	}
}

// TestContentReaderDocumentationFindingsDisclosesUnknownVisibility proves a
// finding whose read visibility was never evaluated (unknown) fails closed: it
// is disclosed as access-denied with content withheld, not silently dropped and
// not surfaced with content.
func TestContentReaderDocumentationFindingsDisclosesUnknownVisibility(t *testing.T) {
	t.Parallel()

	unknown := []byte(`{
		"finding_id": "finding:unknown",
		"finding_type": "service_deployment_drift",
		"status": "conflict",
		"source_id": "doc-source:unknown",
		"document_id": "doc:unknown:1",
		"summary": "visibility was not evaluated"
	}`)
	visible := []byte(`{
		"finding_id": "finding:visible",
		"finding_type": "service_deployment_drift",
		"status": "conflict",
		"source_id": "doc-source:public",
		"document_id": "doc:public:1",
		"summary": "public deployment drift",
		"permissions": {
			"viewer_can_read_source": true
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{unknown}, {visible}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationFindings(t.Context(), documentationFindingFilter{Limit: 50})
	if err != nil {
		t.Fatalf("documentationFindings() error = %v, want nil", err)
	}
	if gotLen, want := len(got.Findings), 2; gotLen != want {
		t.Fatalf("len(Findings) = %d, want %d (unknown row disclosed, not dropped); findings = %#v", gotLen, want, got.Findings)
	}
	unknownRow := got.Findings[0]
	if unknownRow["finding_id"] != "finding:unknown" {
		t.Fatalf("findings[0].finding_id = %#v, want 'finding:unknown'", unknownRow["finding_id"])
	}
	if unknownRow[accessDispositionResponseKey] != accessDispositionDenied {
		t.Fatalf("unknown access_disposition = %#v, want %q (fails closed)", unknownRow[accessDispositionResponseKey], accessDispositionDenied)
	}
	if _, leaked := unknownRow["summary"]; leaked {
		t.Fatalf("unknown-visibility row leaked content: %#v", unknownRow)
	}
}

// TestBuildDocumentationFindingsSQLDropsSilentVisibilityFilters proves the
// per-caller content-visibility predicates are NO LONGER applied as a silent SQL
// drop (#2164 USER-APPROVED policy). Those rows must reach Go so they can be
// disclosed honestly with content withheld rather than filtered to "nothing
// found." The cross-tenant authorization clause is a distinct boundary and is
// asserted elsewhere.
func TestBuildDocumentationFindingsSQLDropsSilentVisibilityFilters(t *testing.T) {
	t.Parallel()

	query, _ := buildDocumentationFindingsSQL(documentationFindingFilter{Limit: 50})
	for _, banned := range []string{
		"LOWER(COALESCE(fact_records.payload->'states'->>'permission_decision', '')) <> 'denied'",
		"(fact_records.payload->'permissions'->>'viewer_can_read_source') = 'true'",
		"LOWER(COALESCE(fact_records.payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'",
	} {
		if strings.Contains(query, banned) {
			t.Fatalf("documentation findings SQL must not silently drop on visibility predicate %q; disclosure is enforced in Go: %s", banned, query)
		}
	}
}

func TestDocumentationHandlerRejectsInvalidPagination(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		url  string
	}{
		{
			name: "non numeric limit",
			url:  "/api/v0/documentation/findings?limit=many",
		},
		{
			name: "limit below one",
			url:  "/api/v0/documentation/findings?limit=0",
		},
		{
			name: "limit above maximum",
			url:  "/api/v0/documentation/findings?limit=201",
		},
		{
			name: "non numeric cursor",
			url:  "/api/v0/documentation/findings?cursor=first",
		},
		{
			name: "negative cursor",
			url:  "/api/v0/documentation/findings?cursor=-1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := &DocumentationHandler{
				Content: fakePortContentStore{},
				Profile: ProfileProduction,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			assertDocumentationError(t, w.Body.Bytes(), "invalid_argument")
		})
	}
}

func TestDocumentationHandlerDoesNotExposeInternalReadErrors(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFindingsErr: errors.New("pq: password authentication failed for user eshu"),
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings", nil)
	req.Header.Set("X-Correlation-ID", "corr-doc-123")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["error_code"], "internal_error"; got != want {
		t.Fatalf("error_code = %#v, want %#v", got, want)
	}
	if got, want := resp["message"], "documentation evidence request failed"; got != want {
		t.Fatalf("message = %#v, want %#v", got, want)
	}
	if got, want := resp["correlation_id"], "corr-doc-123"; got != want {
		t.Fatalf("correlation_id = %#v, want %#v", got, want)
	}
	if _, leaked := resp["detail"]; leaked {
		t.Fatalf("detail leaked internal error: %#v", resp)
	}
}

func TestDocumentationHandlerComparesSavedPacketVersion(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFreshnessModel: documentationEvidencePacketFreshnessReadModel{
				Available:           true,
				PacketID:            "doc-packet:service-deployment:1",
				PacketVersion:       "1",
				FreshnessState:      "stale",
				LatestPacketVersion: "2",
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/evidence-packets/doc-packet:service-deployment:1/freshness?packet_version=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["packet_version"], "1"; got != want {
		t.Fatalf("packet_version = %#v, want %#v", got, want)
	}
	if got, want := resp["latest_packet_version"], "2"; got != want {
		t.Fatalf("latest_packet_version = %#v, want %#v", got, want)
	}
	if got, want := resp["freshness_state"], "stale"; got != want {
		t.Fatalf("freshness_state = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationEvidencePacketFreshnessComparesSavedVersion(t *testing.T) {
	t.Parallel()

	packet := []byte(`{
		"packet_id": "doc-packet:service-deployment:1",
		"packet_version": "2",
		"permissions": {
			"viewer_can_read_source": true
		},
		"states": {
			"freshness_state": "fresh"
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationEvidencePacketFreshness(
		t.Context(),
		"doc-packet:service-deployment:1",
		"1",
	)
	if err != nil {
		t.Fatalf("documentationEvidencePacketFreshness() error = %v, want nil", err)
	}
	if got, want := got.PacketVersion, "1"; got != want {
		t.Fatalf("PacketVersion = %#v, want %#v", got, want)
	}
	if got, want := got.LatestPacketVersion, "2"; got != want {
		t.Fatalf("LatestPacketVersion = %#v, want %#v", got, want)
	}
	if got, want := got.FreshnessState, "stale"; got != want {
		t.Fatalf("FreshnessState = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationEvidencePacketDeniesUnknownVisibility(t *testing.T) {
	t.Parallel()

	packet := []byte(`{
		"packet_id": "doc-packet:service-deployment:1",
		"packet_version": "1",
		"finding_id": "finding:service-deployment:1",
		"bounded_excerpt": {
			"text": "private deployment text",
			"text_hash": "sha256:excerpt"
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationEvidencePacket(t.Context(), "finding:service-deployment:1")
	if err != nil {
		t.Fatalf("documentationEvidencePacket() error = %v, want nil", err)
	}
	if !got.Denied {
		t.Fatal("Denied = false, want true for packet without explicit visibility")
	}
	if got, want := got.DeniedReason, "documentation evidence visibility is unknown"; got != want {
		t.Fatalf("DeniedReason = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationEvidencePacketFreshnessDeniesUnknownVisibility(t *testing.T) {
	t.Parallel()

	packet := []byte(`{
		"packet_id": "doc-packet:service-deployment:1",
		"packet_version": "1",
		"states": {
			"freshness_state": "fresh"
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationEvidencePacketFreshness(
		t.Context(),
		"doc-packet:service-deployment:1",
		"1",
	)
	if err != nil {
		t.Fatalf("documentationEvidencePacketFreshness() error = %v, want nil", err)
	}
	if !got.Denied {
		t.Fatal("Denied = false, want true for freshness packet without explicit visibility")
	}
	if got, want := got.DeniedReason, "documentation evidence visibility is unknown"; got != want {
		t.Fatalf("DeniedReason = %#v, want %#v", got, want)
	}
}
