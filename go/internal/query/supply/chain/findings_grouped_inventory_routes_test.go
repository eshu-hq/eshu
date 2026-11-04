// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

func TestGroupedInventoryScopedGrantReachesStoreAndKeepsPartial(t *testing.T) {
	t.Parallel()

	store := &fakeGroupedInventoryStore{page: impact.GroupedInventoryPage{
		Rows: []impact.GroupedInventoryRow{
			namedRow(impact.GroupedInventoryByService, "svc-a", 1, impact.GroupCompletenessPartial),
		},
		DistinctFindings: 1,
	}}
	ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-1"},
		AllowedScopeIDs:      []string{"scope-1"},
	})
	rec, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
		"/api/v0/supply-chain/impact/inventory?group_by=service_id", ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := store.lastFilter.AllowedRepositoryIDs; len(got) != 1 || got[0] != "repo-1" {
		t.Fatalf("AllowedRepositoryIDs = %v, want [repo-1] handed to the store", got)
	}
	if got := store.lastFilter.AllowedScopeIDs; len(got) != 1 || got[0] != "scope-1" {
		t.Fatalf("AllowedScopeIDs = %v, want [scope-1] handed to the store", got)
	}
	if body.Buckets[0]["completeness"] != "partial" {
		t.Fatalf("scoped bucket completeness = %v, want the store's partial", body.Buckets[0]["completeness"])
	}
}

func TestGroupedInventoryEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	for _, dimension := range []string{"service_id", "workload_id", "environment"} {
		store := &fakeGroupedInventoryStore{err: errors.New("must not be read")}
		ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped})
		rec, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
			"/api/v0/supply-chain/impact/inventory?group_by="+dimension+"&service_id=svc-secret", ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", dimension, rec.Code, rec.Body.String())
		}
		if store.calls != 0 {
			t.Fatalf("%s: store read %d times for an empty grant, want 0", dimension, store.calls)
		}
		if len(body.Buckets) != 0 || body.Count != 0 || body.Truncated {
			t.Fatalf("%s: buckets = %v, want zero buckets and no unattributed row", dimension, body.Buckets)
		}
		if body.DistinctFindings == nil || *body.DistinctFindings != 0 {
			t.Fatalf("%s: distinct_findings = %v, want 0", dimension, body.DistinctFindings)
		}
		if body.GroupBy != dimension {
			t.Fatalf("group_by = %q, want %q", body.GroupBy, dimension)
		}
		if strings.Contains(rec.Body.String(), "svc-secret") {
			t.Fatalf("empty-grant response echoed the requested anchor: %s", rec.Body.String())
		}
	}
}

func TestGroupedInventoryReturns503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	// An aggregate store alone must not satisfy a grouped dimension: the
	// grouped read has its own port and never falls back to AggregateStore.
	aggregates := &countingAggregateStore{}
	rec, _ := serveGrouped(t, &Handler{ImpactAggregates: aggregates},
		"/api/v0/supply-chain/impact/inventory?group_by=service_id", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if aggregates.inventoryCalls != 0 {
		t.Fatalf("grouped dimension fell back to AggregateStore (%d calls)", aggregates.inventoryCalls)
	}
}

func TestGroupedInventoryReadModelUnavailableAnswers503(t *testing.T) {
	t.Parallel()

	store := &fakeGroupedInventoryStore{err: impact.ErrGroupedInventoryReadModelUnavailable}
	aggregates := &countingAggregateStore{}
	rec, _ := serveGrouped(t, &Handler{ImpactGroupedInventory: store, ImpactAggregates: aggregates},
		"/api/v0/supply-chain/impact/inventory?group_by=environment", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if aggregates.inventoryCalls != 0 {
		t.Fatalf("read-model-unavailable fell back to AggregateStore, want no silent fallback")
	}
	if strings.Contains(rec.Body.String(), impact.ErrGroupedInventoryReadModelUnavailable.Error()) &&
		strings.Contains(rec.Body.String(), "stage_failed") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestGroupedInventoryStoreFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	store := &fakeGroupedInventoryStore{err: errors.New("pq: password authentication failed for user eshu")}
	rec, _ := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
		"/api/v0/supply-chain/impact/inventory?group_by=service_id", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("500 body leaked the store error: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), impactGroupedInventoryReadFailedMessage) {
		t.Fatalf("body = %s, want the fixed grouped-inventory failure message", rec.Body.String())
	}
}

func TestInventoryInvalidGroupByListsAllEightDimensions(t *testing.T) {
	t.Parallel()

	handler := &Handler{ImpactAggregates: &countingAggregateStore{}, ImpactGroupedInventory: &fakeGroupedInventoryStore{}}
	rec, _ := serveGrouped(t, handler, "/api/v0/supply-chain/impact/inventory?group_by=bogus", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	for _, dimension := range []string{
		"impact_status", "priority_bucket", "severity", "repository_id", "ecosystem",
		"service_id", "workload_id", "environment",
	} {
		if !strings.Contains(rec.Body.String(), dimension) {
			t.Errorf("400 message %q does not list %s", rec.Body.String(), dimension)
		}
	}
}

func TestInventoryExistingDimensionsStayOnAggregateStore(t *testing.T) {
	t.Parallel()

	grouped := &fakeGroupedInventoryStore{}
	aggregates := &countingAggregateStore{}
	rec, _ := serveGrouped(t, &Handler{ImpactAggregates: aggregates, ImpactGroupedInventory: grouped},
		"/api/v0/supply-chain/impact/inventory?group_by=repository_id", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if aggregates.inventoryCalls != 1 || grouped.calls != 0 {
		t.Fatalf("aggregate calls = %d grouped calls = %d; want 1 and 0", aggregates.inventoryCalls, grouped.calls)
	}
	// The existing bucket shape is unchanged: no distinct_findings.
	if strings.Contains(rec.Body.String(), "distinct_findings") {
		t.Fatalf("existing dimension response gained distinct_findings: %s", rec.Body.String())
	}
}

func TestGroupedInventoryEchoesDetectionProfileTruthfully(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		query      string
		wantEcho   string
		wantFilter string
	}{
		"default is precise":     {"", impact.ProfilePrecise, impact.ProfilePrecise},
		"explicit precise":       {"&profile=precise", impact.ProfilePrecise, impact.ProfilePrecise},
		"comprehensive includes": {"&profile=comprehensive", impact.ProfileComprehensive, ""},
	} {
		store := &fakeGroupedInventoryStore{}
		rec, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
			"/api/v0/supply-chain/impact/inventory?group_by=service_id"+tc.query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", name, rec.Code, rec.Body.String())
		}
		if body.DetectionProfile != tc.wantEcho {
			t.Errorf("%s: detection_profile = %q, want %q", name, body.DetectionProfile, tc.wantEcho)
		}
		if body.Scope["profile"] != tc.wantEcho {
			t.Errorf("%s: scope.profile = %q, want %q", name, body.Scope["profile"], tc.wantEcho)
		}
		if store.lastFilter.DetectionProfile != tc.wantFilter {
			t.Errorf("%s: store filter profile = %q, want %q", name, store.lastFilter.DetectionProfile, tc.wantFilter)
		}
	}
}

func TestGroupedInventoryEmptyStorePageIsExplicitlyEmpty(t *testing.T) {
	t.Parallel()

	rec, body := serveGrouped(t, &Handler{ImpactGroupedInventory: &fakeGroupedInventoryStore{}},
		"/api/v0/supply-chain/impact/inventory?group_by=service_id", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if body.Buckets == nil || len(body.Buckets) != 0 {
		t.Fatalf("buckets = %v, want an empty array, not null; body = %s", body.Buckets, rec.Body.String())
	}
	if body.Freshness["state"] != impact.FreshnessLabelUnknown {
		t.Fatalf("freshness = %v, want unknown when the store reports none", body.Freshness)
	}
}

func TestGroupedInventoryTruthEnvelopeAndComposition(t *testing.T) {
	t.Parallel()

	store := &fakeGroupedInventoryStore{}
	handler := &Handler{ImpactGroupedInventory: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/impact/inventory?group_by=environment&service_id=svc-a", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := store.lastFilter.ServiceID; got != "svc-a" {
		t.Fatalf("composition filter ServiceID = %q, want svc-a", got)
	}
	var env struct {
		Data  map[string]any `json:"data"`
		Truth map[string]any `json:"truth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Truth == nil || env.Truth["capability"] != ImpactAggregateCapability {
		t.Fatalf("truth = %v, want the %s capability envelope", env.Truth, ImpactAggregateCapability)
	}
}

func TestEmptyGrantAggregateRoutesEchoTheProfileActuallyUsed(t *testing.T) {
	t.Parallel()

	// #9873 defect: the empty-grant count and inventory writers echoed
	// comprehensive while the default profile is precise.
	for _, route := range []string{
		"/api/v0/supply-chain/impact/findings/count",
		"/api/v0/supply-chain/impact/inventory?group_by=severity",
		"/api/v0/supply-chain/impact/inventory?group_by=service_id",
	} {
		sep := "?"
		if strings.Contains(route, "?") {
			sep = "&"
		}
		for name, tc := range map[string]struct {
			suffix string
			want   string
		}{
			"default":       {"", impact.ProfilePrecise},
			"precise":       {sep + "profile=precise", impact.ProfilePrecise},
			"comprehensive": {sep + "profile=comprehensive", impact.ProfileComprehensive},
		} {
			ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped})
			handler := &Handler{
				ImpactAggregates:       &countingAggregateStore{},
				ImpactGroupedInventory: &fakeGroupedInventoryStore{},
			}
			rec, body := serveGrouped(t, handler, route+tc.suffix, ctx)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: status = %d, body = %s", route, name, rec.Code, rec.Body.String())
			}
			if body.DetectionProfile != tc.want {
				t.Errorf("%s %s: detection_profile = %q, want %q", route, name, body.DetectionProfile, tc.want)
			}
		}
		ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped})
		rec, _ := serveGrouped(t, &Handler{ImpactAggregates: &countingAggregateStore{}, ImpactGroupedInventory: &fakeGroupedInventoryStore{}},
			route+sep+"profile=bogus", ctx)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s bogus profile: status = %d, want 400 like the list route", route, rec.Code)
		}
	}
}
