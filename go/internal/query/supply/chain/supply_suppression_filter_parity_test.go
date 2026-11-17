// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// capturingFindingStore stands in for the guarded Postgres finding reader:
// it records the filter the route built so the parity proof can verify
// the suppression-state filters reach the reader-store SQL through the
// pin-embedded FindingStore port.
type capturingFindingStore struct {
	got impact.FindingFilter
}

func (s *capturingFindingStore) ListSupplyChainImpactFindings(_ context.Context, filter impact.FindingFilter) ([]impact.FindingRow, error) {
	s.got = filter
	return nil, nil
}

// capturingReconciliationStore stands in for the guarded Postgres
// reconciliation reader: it records the filter the route built so the
// parity proof can verify the provider-state and reconciliation-status
// filters reach the reader-store SQL through the pin-embedded
// SecurityAlertReconciliationStore port.
type capturingReconciliationStore struct {
	got SecurityAlertReconciliationFilter
}

func (s *capturingReconciliationStore) ListSecurityAlertReconciliations(_ context.Context, filter SecurityAlertReconciliationFilter) ([]SecurityAlertReconciliationRow, error) {
	s.got = filter
	return nil, nil
}

// TestSupplyChainSuppressionFiltersReachReaderStore is the core half of
// the arbiter Decision 2 parity obligation: the impact-findings route
// threads the suppression_state and include_suppressed query params into
// the FindingFilter the pin-embedded FindingStore port receives, and the
// reconciliations route threads provider_state and
// reconciliation_status into the SecurityAlertReconciliationFilter its
// pin-embedded port receives. The finding reads consult suppression
// state only inside reader-store SQL — never the suppressions mutation
// port — so proving the filters arrive intact at the port proves the
// split point the satellite parity proof exercises.
func TestSupplyChainSuppressionFiltersReachReaderStore(t *testing.T) {
	t.Parallel()

	findings := &capturingFindingStore{}
	recon := &capturingReconciliationStore{}
	handler := &Handler{ImpactFindings: findings, SecurityAlerts: recon}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10&repository_id=repository:r_217415d9&suppression_state=accepted_risk&include_suppressed=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("findings status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if findings.got.SuppressionState != "accepted_risk" {
		t.Errorf("FindingFilter.SuppressionState = %q, want %q", findings.got.SuppressionState, "accepted_risk")
	}
	if !findings.got.IncludeSuppressed {
		t.Error("FindingFilter.IncludeSuppressed = false, want true")
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?cve_id=CVE-2026-0001&limit=10&repository_id=repository:r_217415d9&provider_state=open&reconciliation_status=pending", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("reconciliations status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if recon.got.ProviderState != "open" {
		t.Errorf("SecurityAlertReconciliationFilter.ProviderState = %q, want %q", recon.got.ProviderState, "open")
	}
	if recon.got.ReconciliationStatus != "pending" {
		t.Errorf("SecurityAlertReconciliationFilter.ReconciliationStatus = %q, want %q", recon.got.ReconciliationStatus, "pending")
	}
}
