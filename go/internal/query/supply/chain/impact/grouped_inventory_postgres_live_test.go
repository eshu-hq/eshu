// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9873 disposable PostgreSQL 18 proof of the winners-sourced grouped inventory (service_id, workload_id, environment): a two-service finding counts in both buckets, unmapped findings land in the unattributed bucket, each bucket equals the count route for filter=X (including the workload entity_keys leg and a BTRIM-ed environment), a repository grant hides an ungranted-only service and marks a partly hidden service partial, operator suppression expiry is evaluated at read time, ordering and truncation keep the whole-set distinct count, and an absent watermark or an off gate answers the typed unavailable error; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// groupedLiveOperatorExpiry is the future expiry of the one operator
// suppression that still hides a finding at seed time.
var groupedLiveOperatorExpiry = time.Now().UTC().Add(time.Hour)

// TestGroupedInventoryStoreWinnersLive drives PostgresGroupedInventoryStore and
// the existing PostgresAggregateStore count route against one seeded
// production-schema database with real materialized winners.
func TestGroupedInventoryStoreWinnersLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DSN"),
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE"),
		5*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
		},
	)
	seedGroupedInventoryLive(t, ctx, database)
	if err := storagepostgres.NewSupplyChainImpactWinnersStore(storagepostgres.SQLDB{DB: database}).
		RebuildAllWinners(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("RebuildAllWinners: %v", err)
	}

	store := PostgresGroupedInventoryStore{DB: database, ReadFromWinners: true}
	counts := PostgresAggregateStore{DB: database}
	precise := AggregateFilter{DetectionProfile: ProfilePrecise}
	read := func(t *testing.T, s PostgresGroupedInventoryStore, filter AggregateFilter, dimension GroupedInventoryDimension, limit int) GroupedInventoryPage {
		t.Helper()
		page, err := s.SupplyChainImpactGroupedInventory(ctx, filter, dimension, limit, 0)
		if err != nil {
			t.Fatalf("SupplyChainImpactGroupedInventory(%s): %v", dimension, err)
		}
		return page
	}
	total := func(t *testing.T, filter AggregateFilter) int {
		t.Helper()
		count, err := counts.CountSupplyChainImpactFindings(ctx, filter)
		if err != nil {
			t.Fatalf("CountSupplyChainImpactFindings: %v", err)
		}
		return count.TotalFindings
	}

	t.Run("service buckets fan out and match the count route", func(t *testing.T) {
		page := read(t, store, precise, GroupedInventoryByService, 50)
		got := groupedLiveCounts(page)
		want := map[string]int{"service:a": 3, "service:b": 2, "service:c": 1, "service:d": 1, "": 1}
		assertGroupedLiveCounts(t, got, want)
		if page.DistinctFindings != 6 || page.DistinctFindings != total(t, precise) {
			t.Fatalf("DistinctFindings = %d, want 6 and the count route total %d", page.DistinctFindings, total(t, precise))
		}
		for _, row := range page.Rows {
			if row.Unattributed {
				if row.Completeness != GroupCompletenessUnattributed {
					t.Fatalf("unattributed completeness = %q", row.Completeness)
				}
				continue
			}
			if row.Completeness != GroupCompletenessComplete {
				t.Fatalf("%s completeness = %q, want complete unscoped", row.Value, row.Completeness)
			}
			filter := precise
			filter.ServiceID = row.Value
			if want := total(t, filter); row.Count != want {
				t.Fatalf("bucket %s = %d, count route service_id=%s = %d", row.Value, row.Count, row.Value, want)
			}
		}
		a := groupedLiveRow(t, page, "service:a")
		if a.MaxPriorityScore != 90 || a.ByImpactStatus["affected_exact"] != 3 || a.BySeverity["critical"] != 1 {
			t.Fatalf("service:a = %#v", a)
		}
	})

	t.Run("workload and environment buckets match their filters", func(t *testing.T) {
		workloads := read(t, store, precise, GroupedInventoryByWorkload, 50)
		assertGroupedLiveCounts(t, groupedLiveCounts(workloads), map[string]int{
			"workload:w1": 2, "workload:w2": 2, "workload:w3": 1, "": 3,
		})
		environments := read(t, store, precise, GroupedInventoryByEnvironment, 50)
		assertGroupedLiveCounts(t, groupedLiveCounts(environments), map[string]int{"prod": 2, "qa": 1, "": 3})
		for _, row := range append(workloads.Rows, environments.Rows...) {
			if row.Unattributed {
				continue
			}
			filter := precise
			if row.Dimension == GroupedInventoryByWorkload {
				filter.WorkloadID = row.Value
			} else {
				filter.Environment = row.Value
			}
			if want := total(t, filter); row.Count != want {
				t.Fatalf("%s bucket %q = %d, count route = %d", row.Dimension, row.Value, row.Count, want)
			}
		}
		composed := precise
		composed.ServiceID = "service:a"
		assertGroupedLiveCounts(t, groupedLiveCounts(read(t, store, composed, GroupedInventoryByEnvironment, 50)),
			map[string]int{"prod": 2, "qa": 1})
	})

	t.Run("repository grant hides ungranted services and marks partial", func(t *testing.T) {
		scoped := precise
		scoped.AllowedRepositoryIDs = []string{"repository:r1", "repository:r2"}
		page := read(t, store, scoped, GroupedInventoryByService, 50)
		assertGroupedLiveCounts(t, groupedLiveCounts(page), map[string]int{"service:a": 3, "service:b": 2})
		if got := groupedLiveRow(t, page, "service:a").Completeness; got != GroupCompletenessPartial {
			t.Fatalf("service:a completeness = %q, want partial (repository:r6 is outside the grant)", got)
		}
		if got := groupedLiveRow(t, page, "service:b").Completeness; got != GroupCompletenessComplete {
			t.Fatalf("service:b completeness = %q, want complete", got)
		}
		if page.DistinctFindings != total(t, scoped) {
			t.Fatalf("scoped DistinctFindings = %d, count route = %d", page.DistinctFindings, total(t, scoped))
		}
		filter := scoped
		filter.ServiceID = "service:a"
		if want := total(t, filter); want != 3 {
			t.Fatalf("scoped count route service_id=service:a = %d, want 3", want)
		}
	})

	t.Run("operator suppression expiry is evaluated at read time", func(t *testing.T) {
		withSuppressed := precise
		withSuppressed.IncludeSuppressed = true
		assertGroupedLiveCounts(t, groupedLiveCounts(read(t, store, withSuppressed, GroupedInventoryByService, 50)),
			map[string]int{"service:a": 3, "service:b": 2, "service:c": 1, "service:d": 2, "": 1})
		later := groupedLiveOperatorExpiry.Add(time.Minute)
		future := store
		future.Now = func() time.Time { return later }
		page := read(t, future, precise, GroupedInventoryByService, 50)
		d := groupedLiveRow(t, page, "service:d")
		if d.Count != 2 {
			t.Fatalf("service:d after expiry = %d, want 2 (the hidden suppression expired)", d.Count)
		}
		// The watermark is now older than the freshness window for this clock.
		if d.Completeness != GroupCompletenessReadModelUnavailable || page.Freshness.State != FreshnessLabelStale {
			t.Fatalf("service:d completeness/freshness = %q/%q, want read_model_unavailable/stale", d.Completeness, page.Freshness.State)
		}
		laterCounts := PostgresAggregateStore{DB: database, Now: func() time.Time { return later }}
		filter := precise
		filter.ServiceID = "service:d"
		count, err := laterCounts.CountSupplyChainImpactFindings(ctx, filter)
		if err != nil || count.TotalFindings != 2 {
			t.Fatalf("count route after expiry = %d (%v), want 2", count.TotalFindings, err)
		}
	})

	t.Run("ordering and truncation keep the whole-set distinct count", func(t *testing.T) {
		page := read(t, store, precise, GroupedInventoryByService, 2)
		if len(page.Rows) != 2 || page.Rows[0].Value != "service:a" || page.Rows[1].Value != "service:b" {
			t.Fatalf("first page = %#v, want service:a then service:b (max 90, count 3 then 2)", page.Rows)
		}
		if page.DistinctFindings != 6 {
			t.Fatalf("truncated DistinctFindings = %d, want whole-set 6", page.DistinctFindings)
		}
		all := read(t, store, precise, GroupedInventoryByService, 50)
		order := make([]string, 0, len(all.Rows))
		for _, row := range all.Rows {
			order = append(order, row.Value)
		}
		want := []string{"service:a", "service:b", "service:c", "service:d", ""}
		if len(order) != len(want) {
			t.Fatalf("order = %q, want %q", order, want)
		}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("order = %q, want %q", order, want)
			}
		}
	})

	t.Run("gate off and absent watermark answer the typed error", func(t *testing.T) {
		off := PostgresGroupedInventoryStore{DB: database}
		if _, err := off.SupplyChainImpactGroupedInventory(ctx, precise, GroupedInventoryByService, 10, 0); !errors.Is(err, ErrGroupedInventoryReadModelUnavailable) {
			t.Fatalf("gate off err = %v", err)
		}
		if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_materialization`); err != nil {
			t.Fatalf("delete watermark: %v", err)
		}
		if _, err := store.SupplyChainImpactGroupedInventory(ctx, precise, GroupedInventoryByService, 10, 0); !errors.Is(err, ErrGroupedInventoryReadModelUnavailable) {
			t.Fatalf("absent watermark err = %v", err)
		}
	})
}

func groupedLiveCounts(page GroupedInventoryPage) map[string]int {
	out := map[string]int{}
	for _, row := range page.Rows {
		out[row.Value] = row.Count
	}
	return out
}

func assertGroupedLiveCounts(t *testing.T, got, want map[string]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("buckets = %v, want %v", got, want)
	}
	for value, count := range want {
		if got[value] != count {
			t.Fatalf("buckets = %v, want %v", got, want)
		}
	}
}

func groupedLiveRow(t *testing.T, page GroupedInventoryPage, value string) GroupedInventoryRow {
	t.Helper()
	for _, row := range page.Rows {
		if row.Value == value {
			return row
		}
	}
	t.Fatalf("bucket %q missing from %v", value, groupedLiveCounts(page))
	return GroupedInventoryRow{}
}

// seedGroupedInventoryLive seeds six repositories. r1 maps to service:a and
// service:b, workload:w1 (and workload:w2 only through entity_keys), and prod;
// r2 maps to service:a, workload:w3 (catalog), and " qa "; r3 maps only to
// service:c; r4 is unmapped; r5 maps to service:d and carries one expired and
// one still-active operator suppression; r6 maps to service:a and has no
// findings. A superseded-generation service fact on r4 must be ignored.
func seedGroupedInventoryLive(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, repo := range []string{"r1", "r2", "r3", "r4", "r5", "r6"} {
		groupedLiveScope(t, ctx, database, "git-repository-scope:repository:"+repo, "gen:"+repo)
	}
	groupedLiveScope(t, ctx, database, supplyChainImpactOperatorSuppressionScopeID, "gen:operator")
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('gen:r4:old', 'git-repository-scope:repository:r4', 'synthetic', NOW(), NOW(), 'superseded', NOW(), '{}'::jsonb)`); err != nil {
		t.Fatalf("insert superseded generation: %v", err)
	}

	runtime := []struct {
		repo, kind, generation string
		payload                map[string]any
	}{
		{"r1", serviceCatalogCorrelationFactKind, "", map[string]any{"service_id": "service:a", "outcome": "exact"}},
		{"r1", serviceCatalogCorrelationFactKind, "", map[string]any{"service_id": "service:b", "outcome": "derived"}},
		{"r1", "reducer_workload_identity", "", map[string]any{"workload_id": "workload:w1", "entity_keys": []string{"workload:w2", "k8s:r1"}}},
		{"r1", cicdRunCorrelationFactKind, "", map[string]any{"environment": "prod", "outcome": "exact"}},
		{"r2", serviceCatalogCorrelationFactKind, "", map[string]any{"service_id": "service:a", "workload_id": "workload:w3", "outcome": "exact"}},
		{"r2", serviceCatalogCorrelationFactKind, "", map[string]any{"service_id": "service:rejected", "outcome": "ambiguous"}},
		{"r2", cicdRunCorrelationFactKind, "", map[string]any{"environment": " qa ", "outcome": "exact"}},
		{"r3", serviceCatalogCorrelationFactKind, "", map[string]any{"service_id": "service:c", "outcome": "exact"}},
		{"r4", serviceCatalogCorrelationFactKind, "gen:r4:old", map[string]any{"service_id": "service:stale", "outcome": "exact"}},
		{"r5", serviceCatalogCorrelationFactKind, "", map[string]any{"service_id": "service:d", "outcome": "exact"}},
		{"r6", serviceCatalogCorrelationFactKind, "", map[string]any{"service_id": "service:a", "outcome": "exact"}},
	}
	for i, fact := range runtime {
		fact.payload["repository_id"] = "repository:" + fact.repo
		generation := fact.generation
		if generation == "" {
			generation = "gen:" + fact.repo
		}
		groupedLiveFact(t, ctx, database, "runtime:"+string(rune('a'+i)), "git-repository-scope:repository:"+fact.repo, generation, fact.kind, fact.payload)
	}

	findings := []struct {
		id, repo string
		score    int
		cvss     float64
	}{
		{"f1", "r1", 90, 9.5},
		{"f2", "r1", 50, 7.5},
		{"f3", "r2", 70, 7.0},
		{"f4", "r3", 80, 8.0},
		{"f5", "r4", 30, 4.0},
		{"f6", "r5", 60, 6.0},
		{"f7", "r5", 55, 5.0},
	}
	for _, finding := range findings {
		payload := groupedLiveFindingPayload(finding.id, finding.repo, finding.score, finding.cvss)
		groupedLiveFact(t, ctx, database, "finding:"+finding.id, "git-repository-scope:repository:"+finding.repo, "gen:"+finding.repo, FindingFactKind, payload)
	}
	for id, expiresAt := range map[string]string{
		"f6": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		"f7": groupedLiveOperatorExpiry.Format(time.RFC3339),
	} {
		payload := groupedLiveFindingPayload(id, "r5", 0, 0)
		delete(payload, "finding_id")
		delete(payload, "priority_score")
		delete(payload, "cvss_score")
		payload["suppression_state"] = "accepted_risk"
		payload["suppression"] = map[string]any{"state": "accepted_risk", "expires_at": expiresAt}
		groupedLiveFact(t, ctx, database, "operator:"+id, supplyChainImpactOperatorSuppressionScopeID, "gen:operator", FindingFactKind, payload)
	}
}

func groupedLiveFindingPayload(id, repo string, score int, cvss float64) map[string]any {
	return map[string]any{
		"finding_id":        "finding:" + id,
		"cve_id":            "CVE-2026-" + id,
		"package_id":        "pkg:npm/example-" + id + "@1.0.0",
		"repository_id":     "repository:" + repo,
		"impact_status":     "affected_exact",
		"detection_profile": ProfilePrecise,
		"observed_version":  "1.0.0",
		"match_reason":      "npm_semver_affected_range",
		"cvss_score":        cvss,
		"priority_score":    score,
		"priority_bucket":   "high",
		"suppression_state": "active",
		"service_ids":       []string{},
		"workload_ids":      []string{},
		"environments":      []string{},
	}
}

func groupedLiveScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'synthetic', 'synthetic', $1, 'synthetic', $1, NOW(), NOW(), 'active', $2, '{}'::jsonb)`, scopeID, generationID); err != nil {
		t.Fatalf("insert scope %s: %v", scopeID, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)`, generationID, scopeID); err != nil {
		t.Fatalf("insert generation %s: %v", generationID, err)
	}
}

func groupedLiveFact(t *testing.T, ctx context.Context, database *sql.DB, factID, scopeID, generationID, kind string, payload map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", factID, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence, source_system,
  source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', 1, 'observed', 'synthetic', $1, NOW(), NOW(), $5::jsonb)`,
		factID, scopeID, generationID, kind, string(body)); err != nil {
		t.Fatalf("insert fact %s: %v", factID, err)
	}
}
