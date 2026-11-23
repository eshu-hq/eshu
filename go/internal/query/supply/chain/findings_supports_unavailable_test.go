// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// TestImpactAggregatesAnswer503NamingTheSupportsModel pins the aggregate side of
// the fail-closed read: a granted count or inventory call the supports table
// cannot serve answers the retryable 503 with the supports text and the backfill
// poll Retry-After (30 s), not the winners text and window (120 s), and a plain
// winners refusal keeps its own text.
func TestImpactAggregatesAnswer503NamingTheSupportsModel(t *testing.T) {
	t.Parallel()

	supportsErr := fmt.Errorf("%w: %w", impact.ErrAggregateReadModelUnavailable, impact.ErrFindingSupportsUnavailable)
	for name, tc := range map[string]struct {
		err        error
		retryAfter string
		text       string
	}{
		"supports not ready": {supportsErr, "30", "supports read model is not ready"},
		"winners stale":      {impact.ErrAggregateReadModelUnavailable, "120", "winners read model is not fresh"},
	} {
		for route, target := range map[string]string{
			"count":     "/api/v0/supply-chain/impact/findings/count?cve_id=CVE-2026-0001",
			"inventory": "/api/v0/supply-chain/impact/inventory?group_by=impact_status&limit=10",
		} {
			t.Run(name+"/"+route, func(t *testing.T) {
				t.Parallel()
				handler := &Handler{ImpactAggregates: errImpactAggregateStore{countErr: tc.err, inventoryErr: tc.err}}
				mux := http.NewServeMux()
				handler.Mount(mux)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
				if rec.Code != http.StatusServiceUnavailable {
					t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
				}
				if got := rec.Header().Get("Retry-After"); got != tc.retryAfter {
					t.Fatalf("Retry-After = %q, want %s", got, tc.retryAfter)
				}
				if !strings.Contains(rec.Body.String(), tc.text) {
					t.Fatalf("body = %s, want %q", rec.Body.String(), tc.text)
				}
			})
		}
	}
}

// TestImpactExplanationAnswers503WhenSupportsAreUnavailable pins the explain and
// packet side of the fail-closed read: a lookup the supports table cannot serve
// answers the retryable 503 naming the supports model on both routes, never a 500
// or a not-found body.
func TestImpactExplanationAnswers503WhenSupportsAreUnavailable(t *testing.T) {
	t.Parallel()

	refusal := fmt.Errorf("explain: %w", impact.ErrFindingSupportsUnavailable)
	for route, target := range map[string]string{
		"explain": "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
		"packet":  "/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1",
	} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			handler := &Handler{
				ImpactExplanations: errExplanationStore{err: refusal},
				PacketResponder:    stubImpactPacketResponder{},
			}
			mux := http.NewServeMux()
			handler.Mount(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Retry-After"); got != "30" {
				t.Fatalf("Retry-After = %q, want 30", got)
			}
			if body := rec.Body.String(); !strings.Contains(body, "supports read model is not ready") {
				t.Fatalf("body = %s, want the supports text", body)
			}
		})
	}
}
