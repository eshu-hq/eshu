// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq/eshu

package chain

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// supportsRefusingFindingStore answers every list with the supports refusal the
// Postgres store raises while the per-support table is not ready (#10880 PR-B2a).
type supportsRefusingFindingStore struct{}

func (supportsRefusingFindingStore) ListSupplyChainImpactFindings(context.Context, impact.FindingFilter) ([]impact.FindingRow, error) {
	return nil, fmt.Errorf("list supply chain impact findings: %w", impact.ErrFindingSupportsUnavailable)
}

// TestListImpactFindingsAnswers503WhenSupportsAreUnavailable pins the handler
// side of the fail-closed read: a granted call the supports table cannot serve
// answers the retryable 503 backend_unavailable envelope, names the supports
// read model, and never a 500 or an empty page.
func TestListImpactFindingsAnswers503WhenSupportsAreUnavailable(t *testing.T) {
	t.Parallel()

	handler := &Handler{ImpactFindings: supportsRefusingFindingStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After = %q, want 30 (one supports backfill poll)", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "supports read model is not ready") {
		t.Fatalf("body = %s, want the supports text", body)
	}
}

// routedFreshnessStore records which freshness path the handler asked for.
type routedFreshnessStore struct {
	capturingFindingStore
	routed    bool
	watermark bool
}

func (s *routedFreshnessStore) SupplyChainImpactFreshness(_ context.Context, _ impact.FindingFilter) (impact.WinnersFreshness, error) {
	s.routed = true
	return impact.WinnersFreshness{}, nil
}

func (s *routedFreshnessStore) SupplyChainImpactWinnersWatermark(context.Context) (impact.WinnersFreshness, error) {
	s.watermark = true
	return impact.WinnersFreshness{ServingFromWinners: true, Present: true, MaterializedAt: time.Unix(1, 0)}, nil
}

// TestListImpactFindingsAsksTheRoutedFreshness pins that the handler reports the
// freshness of the read model that answered the call: a store that routes by
// filter is asked per filter, and the unconditional winners watermark is not
// consulted, so a call answered from the always-current supports table is never
// downgraded by a lagging winners resweep.
func TestListImpactFindingsAsksTheRoutedFreshness(t *testing.T) {
	t.Parallel()

	store := &routedFreshnessStore{}
	handler := &Handler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !store.routed || store.watermark {
		t.Fatalf("routed freshness asked = %v, unconditional watermark asked = %v; want routed only", store.routed, store.watermark)
	}
}
