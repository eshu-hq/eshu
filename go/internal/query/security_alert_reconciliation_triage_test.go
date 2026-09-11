// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSupplyChainListSecurityAlertReconciliationsSurfacesTriageDetails(t *testing.T) {
	t.Parallel()

	store := &recordingSecurityAlertReconciliationStore{
		rows: []SecurityAlertReconciliationRow{{
			ReconciliationID: "reconciliation-stale",
			ProviderAlert: ProviderSecurityAlertRow{
				Provider:            "github_dependabot",
				ProviderAlertNumber: 51,
				ProviderState:       "open",
				RepositoryID:        "repo://github/example-org/payments-api",
				PackageID:           "npm://registry.npmjs.org/left-pad",
				ManifestPath:        "old-package-lock.json",
			},
			ReconciliationStatus: "stale",
			Reason:               "newer owned dependency evidence no longer matches the provider alert manifest path",
			ReasonCode:           "provider_alert_stale",
			MissingEvidence: []SecurityAlertMissingEvidence{{
				Kind:       "current_manifest",
				Reason:     "provider_manifest_no_longer_observed",
				EvidenceID: "consume-current",
			}},
			SourceFreshness:  "active",
			SourceConfidence: "inferred",
		}},
	}
	handler := &SupplyChainHandler{SecurityAlerts: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo://github/example-org/payments-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Reconciliations []SecurityAlertReconciliationResult `json:"reconciliations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Reconciliations), 1; got != want {
		t.Fatalf("len(reconciliations) = %d, want %d", got, want)
	}
	row := resp.Reconciliations[0]
	if got, want := row.ReasonCode, "provider_alert_stale"; got != want {
		t.Fatalf("ReasonCode = %q, want %q", got, want)
	}
	if got, want := row.MissingEvidence[0].Reason, "provider_manifest_no_longer_observed"; got != want {
		t.Fatalf("MissingEvidence[0].Reason = %q, want %q", got, want)
	}
}
