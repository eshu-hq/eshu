// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// Real-Postgres proofs for #7258: the service catalog correlation handler,
// wired to the production RelationshipStore and
// PostgresServiceMaterializationWriter, must defer while a foreign scope's
// relationship generation is retired-or-pending instead of committing a
// service generation that drops that scope's deployment evidence. The
// negative control wires the pre-fix unfenced by-repos read and shows the
// spurious removal changed-since then reports.
//
// Run with:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ServiceCatalogCorpusFence -count=1 -v

const (
	serviceCatalogFenceServiceID = "svc-checkout"
	serviceCatalogFenceIntentID  = "service-catalog-manifest://repo-checkout/catalog-info.yaml"
)

// serviceCatalogFenceFactLoader serves the catalog facts correlating
// svc-checkout to repo-checkout, plus the active repository fact.
type serviceCatalogFenceFactLoader struct{}

func (serviceCatalogFenceFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	entityRef := "component:default/checkout"
	return []facts.Envelope{
		{
			FactID: "entity", FactKind: facts.ServiceCatalogEntityFactKind,
			SchemaVersion: facts.ServiceCatalogSchemaVersionV1, SourceConfidence: facts.SourceConfidenceReported,
			Payload: map[string]any{
				"provider": "backstage", "entity_ref": entityRef, "entity_type": "service",
				"display_name": "Checkout", "lifecycle": "production", "tier": "tier_1",
				"service_id": serviceCatalogFenceServiceID,
			},
		},
		{
			FactID: "ownership", FactKind: facts.ServiceCatalogOwnershipFactKind,
			SchemaVersion: facts.ServiceCatalogSchemaVersionV1, SourceConfidence: facts.SourceConfidenceReported,
			Payload: map[string]any{"provider": "backstage", "entity_ref": entityRef, "owner_ref": "team-payments"},
		},
		{
			FactID: "repo-link", FactKind: facts.ServiceCatalogRepositoryLinkFactKind,
			SchemaVersion: facts.ServiceCatalogSchemaVersionV1, SourceConfidence: facts.SourceConfidenceReported,
			Payload: map[string]any{"provider": "backstage", "entity_ref": entityRef, "repository_id": "repo-checkout"},
		},
	}, nil
}

func (serviceCatalogFenceFactLoader) ListActiveRepositoryFacts(context.Context) ([]facts.Envelope, error) {
	return []facts.Envelope{{
		FactID: "repo-checkout", FactKind: factload.FactKindRepository,
		Payload: map[string]any{"repo_id": "repo-checkout", "name": "checkout", "remote_url": "https://github.com/acme/checkout.git"},
	}}, nil
}

// serviceCatalogFenceNoopCorrelationWriter stands in for the correlation fact
// writer, which is not under test; it counts calls so a deferral can be shown
// to write nothing.
type serviceCatalogFenceNoopCorrelationWriter struct{ calls int }

func (w *serviceCatalogFenceNoopCorrelationWriter) WriteServiceCatalogCorrelations(
	_ context.Context,
	write servicecatalog.ServiceCatalogCorrelationWrite,
) (servicecatalog.ServiceCatalogCorrelationWriteResult, error) {
	w.calls++
	return servicecatalog.ServiceCatalogCorrelationWriteResult{FactsWritten: len(write.Decisions)}, nil
}

// unfencedAsFencedLoader reproduces the pre-#7258 handler read: the unfenced
// by-repos read, always reported complete. It is the negative control only.
type unfencedAsFencedLoader struct{ store *RelationshipStore }

func (l unfencedAsFencedLoader) GetResolvedRelationshipsForReposWithCorpusFence(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, bool, error) {
	rows, err := l.store.GetResolvedRelationshipsForRepos(ctx, repoIDs)
	return rows, true, err
}

// seedServiceCatalogFenceScope creates an active scope whose active
// relationship generation carries one resolved row from repo-checkout.
func seedServiceCatalogFenceScope(
	t *testing.T, ctx context.Context, db *sql.DB,
	scope, generation, target, relType string,
) {
	t.Helper()
	now := time.Now().UTC()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			`INSERT INTO ingestion_scopes
		   (scope_id, scope_kind, source_system, source_key, collector_kind,
		    partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		  VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
			[]any{scope, now, generation},
		},
		{`INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
		  VALUES ($1, $2, 'active', $3, $3)`, []any{generation, scope, now}},
		{serviceCatalogFenceRowInsertSQL, []any{scope + ":" + target, generation, target, relType}},
	} {
		if _, err := db.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed scope %s: %v", scope, err)
		}
	}
}

const serviceCatalogFenceRowInsertSQL = `
INSERT INTO resolved_relationships
  (resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
   confidence, evidence_count, rationale, resolution_source, details)
VALUES ($1, $2, 'repo-checkout', $3, $4, 0.9, 1, 'seed', 'inferred', '{}'::jsonb)`

type serviceCatalogFenceHarness struct {
	ctx         context.Context
	db          *sql.DB
	status      StatusStore
	correlation *serviceCatalogFenceNoopCorrelationWriter
	handler     servicecatalog.ServiceCatalogCorrelationHandler
}

func newServiceCatalogFenceHarness(t *testing.T, prefix string, unfenced bool) serviceCatalogFenceHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)
	db, _ := openServiceLineageSchemaLive(ctx, t, prefix)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	seedServiceCatalogFenceScope(t, ctx, db, "scope-checkout", "gen-checkout-1", "repo-deploy", "DEPLOYS_FROM")
	seedServiceCatalogFenceScope(t, ctx, db, "scope-infra", "gen-infra-1", "repo-infra", "DISCOVERS_CONFIG_IN")

	store := NewRelationshipStore(SQLDB{DB: db})
	var loader servicecatalog.CorpusFencedResolvedRelationshipLoader = store
	if unfenced {
		loader = unfencedAsFencedLoader{store: store}
	}
	correlation := &serviceCatalogFenceNoopCorrelationWriter{}
	return serviceCatalogFenceHarness{
		ctx: ctx, db: db, status: NewStatusStore(SQLDB{DB: db}), correlation: correlation,
		handler: servicecatalog.ServiceCatalogCorrelationHandler{
			FactLoader: serviceCatalogFenceFactLoader{},
			Writer:     correlation,
			MaterializationWriter: servicecatalog.PostgresServiceMaterializationWriter{
				DB: servicecatalog.ServiceMaterializationSQLBeginner{DB: db}, Now: time.Now,
			},
			DeploymentRelationshipLoader: loader,
		},
	}
}

func (h serviceCatalogFenceHarness) handle() error {
	_, err := h.handler.Handle(h.ctx, reducercontract.Intent{
		IntentID:     "intent-7258",
		ScopeID:      serviceCatalogFenceIntentID,
		GenerationID: "catalog-gen-1",
		Domain:       reducercontract.DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	})
	return err
}

func (h serviceCatalogFenceHarness) exec(t *testing.T, query string) {
	t.Helper()
	if _, err := h.db.ExecContext(h.ctx, query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// generations returns every service generation id with its status.
func (h serviceCatalogFenceHarness) generations(t *testing.T) map[string]string {
	t.Helper()
	rows, err := h.db.QueryContext(h.ctx,
		`SELECT generation_id, status FROM service_materialization_generations WHERE service_id = $1`,
		serviceCatalogFenceServiceID)
	if err != nil {
		t.Fatalf("query generations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan generation: %v", err)
		}
		out[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate generations: %v", err)
	}
	return out
}

func activeGeneration(t *testing.T, generations map[string]string) string {
	t.Helper()
	var active []string
	for id, status := range generations {
		if status == "active" {
			active = append(active, id)
		}
	}
	if len(active) != 1 {
		t.Fatalf("active service generations = %v, want exactly one", generations)
	}
	return active[0]
}

// deploymentDelta returns the deployment category of changed-since between
// since and the current active generation.
func (h serviceCatalogFenceHarness) deploymentDelta(t *testing.T, since string) statuspkg.ChangedSinceCategoryDelta {
	t.Helper()
	summary, err := h.status.ComputeServiceChangedSinceDelta(h.ctx, statuspkg.ServiceChangedSinceFilter{
		ServiceID: serviceCatalogFenceServiceID, SinceGenerationID: since,
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta: %v", err)
	}
	for _, category := range summary.Categories {
		if category.Category == "deployment" {
			return category
		}
	}
	t.Fatalf("changed-since summary has no deployment category: %+v", summary)
	return statuspkg.ChangedSinceCategoryDelta{}
}

func TestServiceCatalogCorpusFenceDefersAndPreservesEvidenceLive(t *testing.T) {
	h := newServiceCatalogFenceHarness(t, "eshu_7258_fence", false)

	// 1. Corpus complete: the first generation carries both scopes' rows.
	if err := h.handle(); err != nil {
		t.Fatalf("initial Handle: %v", err)
	}
	first := activeGeneration(t, h.generations(t))

	// 2. The foreign scope's relationship generation is re-resolving.
	h.exec(t, `UPDATE relationship_generations SET status = 'pending' WHERE generation_id = 'gen-infra-1'`)
	callsBefore := h.correlation.calls
	err := h.handle()
	var classified interface {
		Retryable() bool
		FailureClass() string
	}
	if !errors.As(err, &classified) || !classified.Retryable() ||
		classified.FailureClass() != servicecatalog.ServiceCatalogCorrelationResolutionNotReadyFailureClass {
		t.Fatalf("Handle with the fence open: error = %v, want a retryable %s deferral",
			err, servicecatalog.ServiceCatalogCorrelationResolutionNotReadyFailureClass)
	}
	if h.correlation.calls != callsBefore {
		t.Fatalf("correlation writer called %d time(s) during the deferral, want 0", h.correlation.calls-callsBefore)
	}
	if generations := h.generations(t); len(generations) != 1 || generations[first] != "active" {
		t.Fatalf("service generations after deferral = %v, want only %s active (no generation row written)", generations, first)
	}

	// 3. The foreign scope re-activates and the own scope legitimately adds a
	//    deployment row, so the retry commits a new generation.
	h.exec(t, `UPDATE relationship_generations SET status = 'active' WHERE generation_id = 'gen-infra-1'`)
	if _, err := h.db.ExecContext(h.ctx, serviceCatalogFenceRowInsertSQL,
		"scope-checkout:repo-deploy-2", "gen-checkout-1", "repo-deploy-2", "DEPLOYS_FROM"); err != nil {
		t.Fatalf("seed own-scope row: %v", err)
	}
	if err := h.handle(); err != nil {
		t.Fatalf("retry Handle after the fence closed: %v", err)
	}
	second := activeGeneration(t, h.generations(t))
	if second == first {
		t.Fatalf("retry did not commit a new generation (still %s)", first)
	}
	delta := h.deploymentDelta(t, first)
	if delta.Counts.Retired != 0 || delta.Counts.Superseded != 0 || delta.Counts.Added != 1 || delta.Counts.Unchanged != 2 {
		t.Fatalf("deployment delta %s -> %s counts = %+v, want added=1 unchanged=2 retired=0 superseded=0 (no spurious removal of the foreign scope's row)",
			first, second, delta.Counts)
	}
}

// TestServiceCatalogCorpusFenceUnfencedReadReportsSpuriousRemovalLive is the
// negative control: the pre-#7258 unfenced read commits a generation while
// the foreign scope is re-resolving, and changed-since reports its deployment
// row as removed (superseded) although nothing about the service changed.
func TestServiceCatalogCorpusFenceUnfencedReadReportsSpuriousRemovalLive(t *testing.T) {
	h := newServiceCatalogFenceHarness(t, "eshu_7258_control", true)
	if err := h.handle(); err != nil {
		t.Fatalf("initial Handle: %v", err)
	}
	first := activeGeneration(t, h.generations(t))

	h.exec(t, `UPDATE relationship_generations SET status = 'pending' WHERE generation_id = 'gen-infra-1'`)
	if err := h.handle(); err != nil {
		t.Fatalf("unfenced Handle: %v (the pre-fix read never defers)", err)
	}
	second := activeGeneration(t, h.generations(t))
	if second == first {
		t.Fatal("control: the unfenced read did not supersede the generation; the harm is not reproduced")
	}
	delta := h.deploymentDelta(t, first)
	// A key present in the prior generation and absent from the current one
	// is classified superseded (dropped without a tombstone).
	if delta.Counts.Superseded != 1 || delta.Counts.Unchanged != 1 {
		t.Fatalf("control: deployment delta counts = %+v, want superseded=1 unchanged=1 (the spurious removal #7258 fixes)", delta.Counts)
	}
}
