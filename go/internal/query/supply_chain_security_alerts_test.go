package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type recordingSecurityAlertReconciliationStore struct {
	rows       []SecurityAlertReconciliationRow
	lastFilter SecurityAlertReconciliationFilter
}

func (s *recordingSecurityAlertReconciliationStore) ListSecurityAlertReconciliations(
	_ context.Context,
	filter SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	s.lastFilter = filter
	return append([]SecurityAlertReconciliationRow(nil), s.rows...), nil
}

func TestSupplyChainListSecurityAlertReconciliationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SecurityAlerts: &recordingSecurityAlertReconciliationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/security-alerts/reconciliations?limit=10",
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo://github/eshu-hq/eshu",
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

func TestSupplyChainListSecurityAlertReconciliationsSeparatesProviderAndEshuState(t *testing.T) {
	t.Parallel()

	store := &recordingSecurityAlertReconciliationStore{
		rows: []SecurityAlertReconciliationRow{
			{
				ReconciliationID: "reconciliation-1",
				ProviderAlert: ProviderSecurityAlertRow{
					Provider:            "github_dependabot",
					ProviderAlertNumber: 42,
					ProviderState:       "open",
					RepositoryID:        "repo://github/eshu-hq/eshu",
					PackageID:           "npm://registry.npmjs.org/left-pad",
					PackageName:         "left-pad",
					Ecosystem:           "npm",
					ManifestPath:        "package-lock.json",
					DependencyScope:     "runtime",
					Relationship:        "direct",
					GHSAIDs:             []string{"GHSA-abcd-1234"},
					CVEIDs:              []string{"CVE-2026-0001"},
					VulnerableRange:     "<1.2.3",
					PatchedVersion:      "1.2.3",
					Severity:            "critical",
					SourceURL:           "https://github.com/eshu-hq/eshu/security/dependabot/42",
				},
				EshuImpact: SecurityAlertEshuImpactRow{
					ImpactStatus: "affected_exact",
					FindingID:    "impact-1",
				},
				ReconciliationStatus: "matched",
				Reason:               "provider alert matches owned dependency and reducer impact evidence",
				EvidenceFactIDs:      []string{"alert-1", "consume-1", "impact-1"},
				SourceFreshness:      "active",
				SourceConfidence:     "inferred",
			},
			{ReconciliationID: "reconciliation-2", ReconciliationStatus: "provider_only"},
		},
	}
	handler := &SupplyChainHandler{SecurityAlerts: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo://github/eshu-hq/eshu&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://github/eshu-hq/eshu"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Reconciliations []SecurityAlertReconciliationResult `json:"reconciliations"`
		Count           int                                 `json:"count"`
		Limit           int                                 `json:"limit"`
		Truncated       bool                                `json:"truncated"`
		NextCursor      map[string]string                   `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Reconciliations), 1; got != want {
		t.Fatalf("len(reconciliations) = %d, want %d", got, want)
	}
	row := resp.Reconciliations[0]
	if got, want := row.ProviderAlert.ProviderState, "open"; got != want {
		t.Fatalf("ProviderAlert.ProviderState = %q, want %q", got, want)
	}
	if got, want := row.EshuImpact.ImpactStatus, "affected_exact"; got != want {
		t.Fatalf("EshuImpact.ImpactStatus = %q, want %q", got, want)
	}
	if got, want := row.ReconciliationStatus, "matched"; got != want {
		t.Fatalf("ReconciliationStatus = %q, want %q", got, want)
	}
	if got, want := row.ProviderAlert.GHSAIDs, []string{"GHSA-abcd-1234"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ProviderAlert.GHSAIDs = %#v, want %#v", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_reconciliation_id"], "reconciliation-1"; got != want {
		t.Fatalf("next_cursor.after_reconciliation_id = %q, want %q", got, want)
	}
}

func TestPostgresSecurityAlertReconciliationQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"fact.payload->>'repository_id' = $2",
		"fact.payload->>'provider' = $3",
		"fact.payload->>'package_id' = $4",
		"fact.payload->'cve_ids' ? $5",
		"fact.payload->'ghsa_ids' ? $6",
		"fact.payload->>'provider_state' = $7",
		"fact.payload->>'reconciliation_status' = $8",
		"fact.fact_id > $9",
		"LIMIT $10",
	} {
		if !strings.Contains(listSecurityAlertReconciliationsQuery, want) {
			t.Fatalf("listSecurityAlertReconciliationsQuery missing %q:\n%s", want, listSecurityAlertReconciliationsQuery)
		}
	}
}
