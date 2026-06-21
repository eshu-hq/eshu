package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubSBOMAttestationAttachmentAggregateStore struct {
	count         SBOMAttestationAttachmentAggregateCount
	countErr      error
	inventory     []SBOMAttestationAttachmentInventoryRow
	inventoryErr  error
	lastFilter    SBOMAttestationAttachmentAggregateFilter
	lastDimension SBOMAttestationAttachmentInventoryDimension
	lastLimit     int
	lastOffset    int
	countCalls    int
	invCalls      int
}

func (s *stubSBOMAttestationAttachmentAggregateStore) CountSBOMAttestationAttachments(
	_ context.Context,
	filter SBOMAttestationAttachmentAggregateFilter,
) (SBOMAttestationAttachmentAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	if s.countErr != nil {
		return SBOMAttestationAttachmentAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubSBOMAttestationAttachmentAggregateStore) SBOMAttestationAttachmentInventory(
	_ context.Context,
	filter SBOMAttestationAttachmentAggregateFilter,
	dim SBOMAttestationAttachmentInventoryDimension,
	limit int,
	offset int,
) ([]SBOMAttestationAttachmentInventoryRow, error) {
	s.invCalls++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]SBOMAttestationAttachmentInventoryRow(nil), s.inventory...), nil
}

func TestSBOMAttestationAttachmentAggregateRoutesReturn503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/sbom-attestations/attachments/count",
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusServiceUnavailable; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestSBOMAttestationAttachmentAggregateCountReturnsRollups(t *testing.T) {
	t.Parallel()

	store := &stubSBOMAttestationAttachmentAggregateStore{
		count: SBOMAttestationAttachmentAggregateCount{
			TotalAttachments: 18,
			ByAttachmentStatus: map[string]int{
				"attached_verified":   10,
				"attached_unverified": 5,
				"subject_mismatch":    2,
				"unparseable":         1,
			},
			ByArtifactKind: map[string]int{"sbom": 12, "attestation": 6},
		},
	}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/count?subject_digest=sha256:abc", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.countCalls != 1 {
		t.Fatalf("Count called %d times, want 1", store.countCalls)
	}
	if got, want := store.lastFilter.SubjectDigest, "sha256:abc"; got != want {
		t.Fatalf("SubjectDigest = %q, want %q", got, want)
	}
	var body struct {
		TotalAttachments   int            `json:"total_attachments"`
		ByAttachmentStatus map[string]int `json:"by_attachment_status"`
		ByArtifactKind     map[string]int `json:"by_artifact_kind"`
		Scope              map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalAttachments != 18 {
		t.Fatalf("total_attachments = %d, want 18", body.TotalAttachments)
	}
	if body.ByAttachmentStatus["attached_verified"] != 10 {
		t.Fatalf("by_attachment_status[attached_verified] = %d, want 10", body.ByAttachmentStatus["attached_verified"])
	}
	if body.ByArtifactKind["sbom"] != 12 {
		t.Fatalf("by_artifact_kind[sbom] = %d, want 12", body.ByArtifactKind["sbom"])
	}
	if body.Scope["subject_digest"] != "sha256:abc" {
		t.Fatalf("scope.subject_digest = %v, want sha256:abc", body.Scope["subject_digest"])
	}
}

func TestSBOMAttestationAttachmentAggregateRoutesForwardSourceScopes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		target     string
		wantFilter SBOMAttestationAttachmentAggregateFilter
	}{
		{
			name:   "repository count",
			target: "/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo://example/api",
			wantFilter: SBOMAttestationAttachmentAggregateFilter{
				RepositoryID: "repo://example/api",
			},
		},
		{
			name:   "service inventory",
			target: "/api/v0/supply-chain/sbom-attestations/attachments/inventory?service_id=service:example-api&limit=10",
			wantFilter: SBOMAttestationAttachmentAggregateFilter{
				ServiceID: "service:example-api",
			},
		},
		{
			name:   "workload inventory",
			target: "/api/v0/supply-chain/sbom-attestations/attachments/inventory?workload_id=workload:example-api&limit=10",
			wantFilter: SBOMAttestationAttachmentAggregateFilter{
				WorkloadID: "workload:example-api",
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			store := &stubSBOMAttestationAttachmentAggregateStore{
				count: SBOMAttestationAttachmentAggregateCount{
					TotalAttachments:   0,
					ByAttachmentStatus: map[string]int{},
					ByArtifactKind:     map[string]int{},
				},
				inventory: []SBOMAttestationAttachmentInventoryRow{},
			}
			handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if store.lastFilter.RepositoryID != tc.wantFilter.RepositoryID ||
				store.lastFilter.WorkloadID != tc.wantFilter.WorkloadID ||
				store.lastFilter.ServiceID != tc.wantFilter.ServiceID {
				t.Fatalf("filter = %+v, want source scope %+v", store.lastFilter, tc.wantFilter)
			}
			var body struct {
				TotalAttachments int               `json:"total_attachments"`
				Scope            map[string]string `json:"scope"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v; body = %s", err, w.Body.String())
			}
			wantScope := sbomAttestationAttachmentAggregateScope(tc.wantFilter)
			for key, wantValue := range wantScope {
				if got := body.Scope[key]; got != wantValue {
					t.Fatalf("scope[%s] = %q, want %q; body = %s", key, got, wantValue, w.Body.String())
				}
			}
		})
	}
}

// TestSBOMAttestationAttachmentAggregateQueriesKeepActiveScanAnchor pins the
// bounded scan shape that #3389 relies on. The count, group, and inventory
// queries each run COUNT(*) / GROUP BY over one fact_kind's active tuples with
// no payload anchor in the common case, so each must keep its single-fact_kind
// predicate, `is_tombstone = FALSE`, and the active-generation join. Those are
// exactly the columns the partial index
// fact_records_sbom_attestation_attachments_active_scan_idx is built on (index
// presence pinned in
// go/internal/storage/postgres/schema_fact_records_sbom_test.go). If a later
// edit drops the active filter or broadens the fact_kind, the planner can no
// longer bound the scan to one kind's active rows and the whole-table scan
// regression from #3389 returns.
func TestSBOMAttestationAttachmentAggregateQueriesKeepActiveScanAnchor(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"total":     sbomAttestationAttachmentAggregateTotalQuery,
		"group":     sbomAttestationAttachmentAggregateGroupQueryTemplate,
		"inventory": sbomAttestationAttachmentInventoryQueryTemplate,
	} {
		for _, want := range []string{
			"WHERE fact.fact_kind = 'reducer_sbom_attestation_attachment'",
			"AND fact.is_tombstone = FALSE",
			"ON scope.scope_id = fact.scope_id\n AND scope.active_generation_id = fact.generation_id",
			"AND generation.status = 'active'",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("%s aggregate query missing #3389 bounded-scan anchor %q:\n%s", name, want, query)
			}
		}
	}
}

func TestSBOMAttestationAttachmentAggregateQueriesFilterSourceScopes(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.payload->'repository_ids' ? $6",
		"fact.payload->'workload_ids' ? $7",
		"fact.payload->'service_ids' ? $8",
	} {
		if !strings.Contains(sbomAttestationAttachmentAggregateTotalQuery, want) {
			t.Fatalf("total query missing source-scope predicate %q:\n%s", want, sbomAttestationAttachmentAggregateTotalQuery)
		}
		if !strings.Contains(sbomAttestationAttachmentAggregateGroupQueryTemplate, want) {
			t.Fatalf("group query missing source-scope predicate %q:\n%s", want, sbomAttestationAttachmentAggregateGroupQueryTemplate)
		}
		if !strings.Contains(sbomAttestationAttachmentInventoryQueryTemplate, want) {
			t.Fatalf("inventory query missing source-scope predicate %q:\n%s", want, sbomAttestationAttachmentInventoryQueryTemplate)
		}
	}
}

func TestSBOMAttestationAttachmentAggregateRoutesDoNotDropServiceScope(t *testing.T) {
	t.Parallel()

	store := &stubSBOMAttestationAttachmentAggregateStore{
		count: SBOMAttestationAttachmentAggregateCount{
			TotalAttachments:   18,
			ByAttachmentStatus: map[string]int{"attached_verified": 18},
			ByArtifactKind:     map[string]int{"sbom": 18},
		},
	}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/count?service_id=service:example-api", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.ServiceID, "service:example-api"; got != want {
		t.Fatalf("ServiceID = %q, want %q", got, want)
	}
	var body struct {
		Scope map[string]string `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if got, want := body.Scope["service_id"], "service:example-api"; got != want {
		t.Fatalf("scope.service_id = %q, want %q", got, want)
	}
}

func TestSBOMAttestationAttachmentAggregateRoutesAcceptRepositoryScope(t *testing.T) {
	t.Parallel()

	store := &stubSBOMAttestationAttachmentAggregateStore{}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo://example/api",
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory?repository_id=repo://example/api&limit=10",
	} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestSBOMAttestationAttachmentAggregateInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubSBOMAttestationAttachmentAggregateStore{
		inventory: []SBOMAttestationAttachmentInventoryRow{
			{Dimension: SBOMAttestationAttachmentInventoryByAttachmentStatus, Value: "attached_verified", Count: 30},
			{Dimension: SBOMAttestationAttachmentInventoryByAttachmentStatus, Value: "attached_unverified", Count: 8},
		},
	}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=attachment_status&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastDimension != SBOMAttestationAttachmentInventoryByAttachmentStatus {
		t.Fatalf("dimension = %q, want attachment_status", store.lastDimension)
	}
	if store.lastLimit != 11 {
		t.Fatalf("internal limit = %d, want 11 (caller limit + 1)", store.lastLimit)
	}
	var body struct {
		Count     int    `json:"count"`
		GroupBy   string `json:"group_by"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2", body.Count)
	}
	if body.GroupBy != "attachment_status" {
		t.Fatalf("group_by = %q, want attachment_status", body.GroupBy)
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false (only 2 buckets, limit 10)")
	}
}

func TestSBOMAttestationAttachmentAggregateInventoryReportsTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]SBOMAttestationAttachmentInventoryRow, 6)
	for i := range rows {
		rows[i] = SBOMAttestationAttachmentInventoryRow{
			Dimension: SBOMAttestationAttachmentInventoryBySubjectDigest,
			Value:     "sha256:abc",
			Count:     i,
		}
	}
	store := &stubSBOMAttestationAttachmentAggregateStore{inventory: rows}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=subject_digest&limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body struct {
		Count      int  `json:"count"`
		Truncated  bool `json:"truncated"`
		NextOffset int  `json:"next_offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.Count != 5 {
		t.Fatalf("count = %d, want 5 (page trim)", body.Count)
	}
	if !body.Truncated {
		t.Fatalf("truncated = false, want true")
	}
	if body.NextOffset != 5 {
		t.Fatalf("next_offset = %d, want 5", body.NextOffset)
	}
}

func TestSBOMAttestationAttachmentAggregateRejectsOutOfContractEnums(t *testing.T) {
	t.Parallel()

	store := &stubSBOMAttestationAttachmentAggregateStore{}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// `verified` is a typo for `attached_verified`; `signed` is not in the
	// artifact_kind enum. Both aggregate endpoints must surface them as 400
	// instead of silently returning zero counts (mirrors the Copilot lesson
	// from #693/#694/#695).
	for _, target := range []string{
		"/api/v0/supply-chain/sbom-attestations/attachments/count?attachment_status=verified",
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory?attachment_status=verified",
		"/api/v0/supply-chain/sbom-attestations/attachments/count?artifact_kind=signed",
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory?artifact_kind=signed",
	} {
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
	if store.countCalls != 0 || store.invCalls != 0 {
		t.Fatalf("store called for out-of-contract filter (countCalls=%d invCalls=%d)",
			store.countCalls, store.invCalls)
	}
}

func TestSBOMAttestationAttachmentAggregateInventoryRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	store := &stubSBOMAttestationAttachmentAggregateStore{}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=document_id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.invCalls != 0 {
		t.Fatalf("store called for unknown dimension")
	}
}

func TestSBOMAttestationAttachmentAggregateInventoryRejectsOversizedLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SBOMAttachmentAggregates: &stubSBOMAttestationAttachmentAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/inventory?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestSBOMAttestationAttachmentAggregateInventoryRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SBOMAttachmentAggregates: &stubSBOMAttestationAttachmentAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/inventory?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestSBOMAttestationAttachmentAggregateInventoryRejectsOversizedOffset(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SBOMAttachmentAggregates: &stubSBOMAttestationAttachmentAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/inventory?offset=10001", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSBOMAttestationAttachmentAggregateInventoryNullsNextOffsetAtCeiling(t *testing.T) {
	t.Parallel()

	rows := make([]SBOMAttestationAttachmentInventoryRow, 6)
	for i := range rows {
		rows[i] = SBOMAttestationAttachmentInventoryRow{
			Dimension: SBOMAttestationAttachmentInventoryBySubjectDigest,
			Value:     "sha256:abc",
			Count:     i,
		}
	}
	store := &stubSBOMAttestationAttachmentAggregateStore{inventory: rows}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=subject_digest&limit=5&offset=10000", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body struct {
		Truncated  bool `json:"truncated"`
		NextOffset *int `json:"next_offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if !body.Truncated {
		t.Fatalf("truncated = false, want true")
	}
	if body.NextOffset != nil {
		t.Fatalf("next_offset = %d, want null when offset+limit exceeds documented max", *body.NextOffset)
	}
}

func TestNextSBOMAttestationAttachmentAggregateOffsetBound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		offset    int
		limit     int
		truncated bool
		want      any
	}{
		{"not truncated returns nil", 0, 100, false, nil},
		{"normal next offset", 200, 100, true, 300},
		{"exactly at ceiling boundary returns ceiling", 9900, 100, true, 10000},
		{"would exceed ceiling returns nil", 9950, 100, true, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nextSBOMAttestationAttachmentAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextSBOMAttestationAttachmentAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}

func TestSBOMAttestationAttachmentInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []SBOMAttestationAttachmentInventoryDimension{
		SBOMAttestationAttachmentInventoryByAttachmentStatus,
		SBOMAttestationAttachmentInventoryByArtifactKind,
		SBOMAttestationAttachmentInventoryBySubjectDigest,
	}
	for _, dim := range cases {
		if _, err := sbomAttestationAttachmentInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := sbomAttestationAttachmentInventoryGroupExpression("document_id"); err == nil {
		t.Fatal("sbomAttestationAttachmentInventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}
