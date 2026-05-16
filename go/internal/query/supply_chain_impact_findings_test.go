package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingSupplyChainImpactFindingStore struct {
	rows       []SupplyChainImpactFindingRow
	lastFilter SupplyChainImpactFindingFilter
}

func (s *recordingSupplyChainImpactFindingStore) ListSupplyChainImpactFindings(
	_ context.Context,
	filter SupplyChainImpactFindingFilter,
) ([]SupplyChainImpactFindingRow, error) {
	s.lastFilter = filter
	return append([]SupplyChainImpactFindingRow(nil), s.rows...), nil
}

type unusedSupplyChainImpactFindingQueryer struct{}

func (unusedSupplyChainImpactFindingQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	return nil, fmt.Errorf("query must not run for invalid filters")
}

func TestSupplyChainListImpactFindingsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactFindings: &recordingSupplyChainImpactFindingStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings?limit=10",
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001",
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

func TestPostgresSupplyChainImpactFindingStoreReportsPaginationLimit(t *testing.T) {
	t.Parallel()

	store := NewPostgresSupplyChainImpactFindingStore(unusedSupplyChainImpactFindingQueryer{})

	_, err := store.ListSupplyChainImpactFindings(context.Background(), SupplyChainImpactFindingFilter{
		CVEID: "CVE-2026-0001",
		Limit: supplyChainImpactFindingMaxLimit + 2,
	})
	if err == nil {
		t.Fatal("ListSupplyChainImpactFindings() error = nil, want limit error")
	}
	want := fmt.Sprintf("limit must be between 1 and %d for internal pagination", supplyChainImpactFindingMaxLimit+1)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestSupplyChainListImpactFindingsUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:           "finding-1",
				CVEID:               "CVE-2026-0001",
				PackageID:           "pkg:npm/example",
				PURL:                "pkg:npm/example@1.2.3",
				ImpactStatus:        "affected_exact",
				Confidence:          "exact",
				CVSSScore:           9.8,
				EPSSProbability:     "0.71",
				KnownExploited:      true,
				RuntimeReachability: "package_manifest",
				RepositoryID:        "repo://example/api",
				MissingEvidence:     []string{"deployment evidence missing"},
				EvidencePath:        []string{"vulnerability.cve", "vulnerability.affected_package", "package_registry.package_version"},
				EvidenceFactIDs:     []string{"cve-1", "affected-1", "version-1"},
				SourceFreshness:     "active",
				SourceConfidence:    "inferred",
			},
			{FindingID: "finding-2", ImpactStatus: "possibly_affected"},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.CVEID, "CVE-2026-0001"; got != want {
		t.Fatalf("CVEID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Findings   []SupplyChainImpactFindingResult `json:"findings"`
		Count      int                              `json:"count"`
		Limit      int                              `json:"limit"`
		Truncated  bool                             `json:"truncated"`
		NextCursor map[string]string                `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}
	if !resp.Findings[0].KnownExploited {
		t.Fatalf("KnownExploited = false, want true")
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_finding_id"], "finding-1"; got != want {
		t.Fatalf("next_cursor.after_finding_id = %q, want %q", got, want)
	}
}

func TestSupplyChainImpactFindingQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"scope.active_generation_id = fact.generation_id",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'cve_id' = $2",
		"fact.payload->>'package_id' = $3",
		"fact.payload->>'repository_id' = $4",
		"fact.payload->>'subject_digest' = $5",
		"fact.payload->>'impact_status' = $6",
	} {
		if !strings.Contains(listSupplyChainImpactFindingsQuery, want) {
			t.Fatalf("listSupplyChainImpactFindingsQuery missing %q:\n%s", want, listSupplyChainImpactFindingsQuery)
		}
	}
}
