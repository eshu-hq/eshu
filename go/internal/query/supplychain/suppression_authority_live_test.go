// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	// Registers the "pgx" driver this test opens by name. Other files in this
	// package also blank-import it, so the driver would resolve without this
	// line today -- but only by accident of what else compiles into the test
	// binary. Without it, removing a sibling import or moving this file to an
	// external package_test turns the failure into sql: unknown driver "pgx",
	// visible only when ESHU_POSTGRES_TEST_DSN is set, so CI would skip green.
	_ "github.com/jackc/pgx/v5/stdlib"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	suppressionAuthorityLiveCVE          = "CVE-2026-54650"
	suppressionAuthorityLivePackage      = "pkg:deb/example/suppression-authority"
	suppressionAuthorityLiveRepository   = "repository:r_5465_suppression_authority"
	suppressionAuthorityLiveSource       = "scope:5465:source"
	suppressionAuthorityLiveSecondSource = "scope:5465:source:second"
	suppressionAuthorityLiveOperator     = "operator:vulnerability_suppressions"
	suppressionAuthorityLiveSourceGen    = "generation:5465:source:active"
	suppressionAuthorityLiveSecondGen    = "generation:5465:source:second:active"
	suppressionAuthorityLiveOperatorGen  = "generation:5465:operator:active"
	suppressionAuthorityLiveFinding      = "finding:5465:suppression-authority"
	suppressionAuthorityLiveSourceFact   = "fact:5465:source"
	suppressionAuthorityLiveOperatorFact = "fact:5465:operator"
	suppressionAuthorityTimelessCVE      = "CVE-2026-54651"
	suppressionAuthorityTimelessFinding  = "finding:5465:suppression-timeless"
	suppressionAuthorityMalformedCVE     = "CVE-2026-54652"
	suppressionAuthorityMalformedFinding = "finding:5465:suppression-malformed"
	suppressionAuthorityOrphanCVE        = "CVE-2026-54653"
	suppressionAuthorityOrphanFinding    = "finding:5465:suppression-orphan"
	suppressionAuthorityOriginalImage    = "registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	suppressionAuthoritySecondImage      = "registry.example.com/team/api@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5465 suppression-authority proof")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply Postgres bootstrap schema: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	seedSupplyChainSuppressionAuthorityLiveFacts(t, ctx, tx)
	readAt := time.Date(2026, 7, 27, 12, 0, 10, 0, time.UTC)
	now := func() time.Time { return readAt }
	direct := impact.NewPostgresSupplyChainImpactFindingStore(tx)
	direct.Now = now
	materialized := impact.NewPostgresSupplyChainImpactFindingStoreWithReadModel(tx, true)
	materialized.Now = now
	aggregates := impact.NewPostgresSupplyChainImpactAggregateStore(tx)
	aggregates.Now = now

	assertSuppressionAuthorityState(t, ctx, direct, aggregates, false, 0, "")
	assertSuppressionAuthorityState(t, ctx, direct, aggregates, true, 1, "ignored")
	assertSuppressionAuthorityFilter(t, ctx, direct, aggregates, "ignored", true, 1)
	assertSuppressionAuthorityFilter(t, ctx, direct, aggregates, "expired", true, 0)
	assertSuppressionExpiryEdgeCases(t, ctx, direct)
	explanation, err := direct.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
		FindingID: suppressionAuthorityLiveFinding,
	})
	if err != nil {
		t.Fatalf("explain ignored finding: %v", err)
	}
	if got := explanation.Finding.Suppression.State; got != "ignored" {
		t.Fatalf("explain suppression state = %q, want ignored", got)
	}
	explanation, err = direct.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
		FindingID: suppressionAuthorityLiveSourceFact,
	})
	if err != nil {
		t.Fatalf("explain finding by legacy fact ID: %v", err)
	}
	if got := explanation.Finding.FindingID; got != suppressionAuthorityLiveFinding {
		t.Fatalf("legacy fact-ID explanation finding = %q, want %q", got, suppressionAuthorityLiveFinding)
	}

	rebuildSuppressionAuthorityWinners(t, ctx, tx)
	assertScopedSuppressionAuthority(t, ctx, direct, materialized, aggregates)
	assertSuppressionAuthorityCloneDriftAndOrphan(t, ctx, direct, materialized)
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, false, 0, "")
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, true, 1, "ignored")
	assertSuppressionAuthorityFilter(t, ctx, materialized, aggregates, "ignored", true, 1)
	assertSuppressionExpiryEdgeCases(t, ctx, materialized)

	// Advance only the read clock to expires_at. No suppression mutation,
	// reducer replay, winners rebuild, or fact update is allowed: query-time
	// expiry is the production mechanism under test, and equality must expire.
	readAt = time.Date(2026, 7, 27, 12, 0, 30, 0, time.UTC)
	assertSuppressionAuthorityState(t, ctx, direct, aggregates, false, 1, "expired")
	assertSuppressionAuthorityState(t, ctx, materialized, aggregates, false, 1, "expired")
	assertSuppressionAuthorityFilter(t, ctx, direct, aggregates, "expired", false, 1)
	assertSuppressionAuthorityFilter(t, ctx, direct, aggregates, "ignored", true, 0)
	assertSuppressionAuthorityFilter(t, ctx, materialized, aggregates, "expired", false, 1)
	assertSuppressionAuthorityFilter(t, ctx, materialized, aggregates, "ignored", true, 0)
	assertSuppressionAuthorityCursor(t, ctx, direct)
	assertSuppressionAuthorityCursor(t, ctx, materialized)
	assertSuppressionExpiryEdgeCases(t, ctx, direct)
	assertSuppressionExpiryEdgeCases(t, ctx, materialized)
	explanation, err = direct.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
		FindingID: suppressionAuthorityLiveFinding,
	})
	if err != nil {
		t.Fatalf("explain expired finding: %v", err)
	}
	if got := explanation.Finding.Suppression.State; got != "expired" {
		t.Fatalf("explain suppression state = %q, want expired", got)
	}

	var persistedState string
	if err := tx.QueryRowContext(
		ctx,
		`SELECT payload #>> '{suppression,state}' FROM fact_records WHERE fact_id = $1`,
		suppressionAuthorityLiveOperatorFact,
	).Scan(&persistedState); err != nil {
		t.Fatalf("read immutable operator fact: %v", err)
	}
	if persistedState != "ignored" {
		t.Fatalf("persisted suppression state = %q, want ignored (read path must not mutate facts)", persistedState)
	}
}

func rebuildSuppressionAuthorityWinners(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
) {
	t.Helper()
	store := storagepostgres.NewSupplyChainImpactWinnersStore(tx)
	if err := store.RebuildAllWinners(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("rebuild canonical winners: %v", err)
	}
}

func seedSupplyChainSuppressionAuthorityLiveFacts(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
) {
	t.Helper()
	for _, scope := range []struct {
		scopeID      string
		generationID string
	}{
		{suppressionAuthorityLiveSource, suppressionAuthorityLiveSourceGen},
		{suppressionAuthorityLiveSecondSource, suppressionAuthorityLiveSecondGen},
		{suppressionAuthorityLiveOperator, suppressionAuthorityLiveOperatorGen},
	} {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'synthetic', 'synthetic', $1, 'synthetic', $1, NOW(), NOW(), 'active', '{}'::jsonb)`,
			scope.scopeID,
		); err != nil {
			t.Fatalf("insert scope %s: %v", scope.scopeID, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)`,
			scope.generationID,
			scope.scopeID,
		); err != nil {
			t.Fatalf("insert generation %s: %v", scope.generationID, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
			scope.generationID,
			scope.scopeID,
		); err != nil {
			t.Fatalf("activate generation %s: %v", scope.generationID, err)
		}
	}

	basePayload := map[string]any{
		"finding_id":        suppressionAuthorityLiveFinding,
		"cve_id":            suppressionAuthorityLiveCVE,
		"package_id":        suppressionAuthorityLivePackage,
		"repository_id":     suppressionAuthorityLiveRepository,
		"impact_status":     "affected_exact",
		"detection_profile": "comprehensive",
		"cvss_score":        8.0,
		"priority_score":    "50",
		"priority_bucket":   "high",
		"image_ref":         suppressionAuthorityOriginalImage,
		"severity_bucket":   "high",
		"suppression_state": "active",
		"suppression":       map[string]any{"state": "active"},
		"service_ids":       []string{},
		"workload_ids":      []string{},
		"environments":      []string{},
		"evidence_fact_ids": []string{},
	}
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		suppressionAuthorityLiveSourceFact,
		suppressionAuthorityLiveSource,
		suppressionAuthorityLiveSourceGen,
		impact.SupplyChainImpactFindingFactKind,
		false,
		basePayload,
	)

	secondSourcePayload := make(map[string]any, len(basePayload))
	for key, value := range basePayload {
		secondSourcePayload[key] = value
	}
	secondSourcePayload["image_ref"] = suppressionAuthoritySecondImage
	secondSourcePayload["priority_score"] = "90"
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		"fact:5465:source:second",
		suppressionAuthorityLiveSecondSource,
		suppressionAuthorityLiveSecondGen,
		impact.SupplyChainImpactFindingFactKind,
		false,
		secondSourcePayload,
	)

	operatorPayload := make(map[string]any, len(basePayload))
	for key, value := range basePayload {
		operatorPayload[key] = value
	}
	operatorPayload["suppression_state"] = "ignored"
	operatorPayload["cvss_score"] = 9.9
	operatorPayload["priority_bucket"] = "critical"
	operatorPayload["severity_bucket"] = "critical"
	operatorPayload["suppression"] = map[string]any{
		"state":          "ignored",
		"suppression_id": "suppression-5465-live",
		"source":         "eshu_policy",
		"justification":  "ignored",
		"author":         "shared_token",
		"authored_at":    "2026-07-27T12:00:00Z",
		"expires_at":     "2026-07-27T12:00:30Z",
		"reason":         "synthetic temporary exception",
	}
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		suppressionAuthorityLiveOperatorFact,
		suppressionAuthorityLiveOperator,
		suppressionAuthorityLiveOperatorGen,
		impact.SupplyChainImpactFindingFactKind,
		false,
		operatorPayload,
	)

	orphanPayload := make(map[string]any, len(operatorPayload))
	for key, value := range operatorPayload {
		orphanPayload[key] = value
	}
	orphanPayload["finding_id"] = suppressionAuthorityOrphanFinding
	orphanPayload["cve_id"] = suppressionAuthorityOrphanCVE
	orphanPayload["suppression"] = map[string]any{
		"state":          "ignored",
		"suppression_id": "suppression-5465-orphan",
		"source":         "eshu_policy",
		"justification":  "ignored",
		"author":         "shared_token",
		"authored_at":    "2026-07-27T12:00:00Z",
		"expires_at":     "2026-07-27T12:00:30Z",
		"reason":         "synthetic orphaned decision",
	}
	insertSupplyChainRuntimeFilterFact(
		t,
		ctx,
		tx,
		"fact:5465:operator:orphan",
		suppressionAuthorityLiveOperator,
		suppressionAuthorityLiveOperatorGen,
		impact.SupplyChainImpactFindingFactKind,
		false,
		orphanPayload,
	)

	for _, edge := range []struct {
		factID, findingID, cveID string
		expiresAt                string
	}{
		{
			factID:    "fact:5465:operator:timeless",
			findingID: suppressionAuthorityTimelessFinding,
			cveID:     suppressionAuthorityTimelessCVE,
		},
		{
			factID:    "fact:5465:operator:malformed",
			findingID: suppressionAuthorityMalformedFinding,
			cveID:     suppressionAuthorityMalformedCVE,
			expiresAt: "not-a-time",
		},
	} {
		sourcePayload := make(map[string]any, len(basePayload))
		for key, value := range basePayload {
			sourcePayload[key] = value
		}
		sourcePayload["finding_id"] = edge.findingID
		sourcePayload["cve_id"] = edge.cveID
		insertSupplyChainRuntimeFilterFact(
			t,
			ctx,
			tx,
			edge.factID+":source",
			suppressionAuthorityLiveSource,
			suppressionAuthorityLiveSourceGen,
			impact.SupplyChainImpactFindingFactKind,
			false,
			sourcePayload,
		)

		operatorPayload := make(map[string]any, len(sourcePayload))
		for key, value := range sourcePayload {
			operatorPayload[key] = value
		}
		operatorPayload["suppression_state"] = "ignored"
		suppression := map[string]any{
			"state":          "ignored",
			"suppression_id": "suppression-" + edge.factID,
			"source":         "eshu_policy",
			"justification":  "ignored",
			"author":         "shared_token",
			"authored_at":    "2026-07-27T12:00:00Z",
			"reason":         "synthetic edge-case exception",
		}
		if edge.expiresAt != "" {
			suppression["expires_at"] = edge.expiresAt
		}
		operatorPayload["suppression"] = suppression
		insertSupplyChainRuntimeFilterFact(
			t,
			ctx,
			tx,
			edge.factID,
			suppressionAuthorityLiveOperator,
			suppressionAuthorityLiveOperatorGen,
			impact.SupplyChainImpactFindingFactKind,
			false,
			operatorPayload,
		)
	}
}
