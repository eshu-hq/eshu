// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin/retirement"
	adminstore "github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/semanticsearch"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/alerts"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	maintenance "github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newCoreCodeHybridRanker builds the optional find_code hybrid re-ranker
// over a process-local deterministic embedder: request snippets never
// egress on the find_code path. Nil when semantic search is disabled.
func newCoreCodeHybridRanker(config searchembedruntime.Config) codemodel.CodeResultReranker {
	if !config.Enabled {
		return nil
	}
	return codemodel.NewCodeHybridRanker(true)
}

// newCoreContentHybridRanker builds the optional content-search hybrid
// re-ranker with the same no-egress guarantee. Nil when disabled.
func newCoreContentHybridRanker(config searchembedruntime.Config) ContentResultReranker {
	if !config.Enabled {
		return nil
	}
	return NewContentHybridRanker(true)
}

// newCoreSemanticSearchScopeResolver builds the instrumented scope resolver
// over the shared read store.
func newCoreSemanticSearchScopeResolver(
	reader db.Queryer,
	instruments *telemetry.Instruments,
) SemanticSearchScopeResolver {
	return NewPostgresSemanticSearchScopeResolver(
		newCoreInstrumentedQueryer(reader, instruments, "semantic_search_scope"))
}

// newCoreInstrumentedQueryer wraps a read queryer with OTEL tracing when
// instruments are wired.
func newCoreInstrumentedQueryer(
	inner db.Queryer,
	instruments *telemetry.Instruments,
	storeName string,
) *pgstatus.InstrumentedQueryer {
	return &pgstatus.InstrumentedQueryer{
		Inner:       inner,
		Instruments: instruments,
		StoreName:   storeName,
	}
}

// newCoreSemanticSearchHybrid builds the persisted local semantic-search
// hybrid over instrumented vector readers. Nil when disabled.
func newCoreSemanticSearchHybrid(
	reader db.Queryer,
	config searchembedruntime.Config,
	instruments *telemetry.Instruments,
) SemanticSearchHybridStore {
	if !config.Enabled {
		return nil
	}
	metadataDB := newCoreInstrumentedQueryer(reader, instruments, "semantic_search_vector_metadata")
	valueDB := newCoreInstrumentedQueryer(reader, instruments, "semantic_search_vector_values")
	snapshotDB := newCoreInstrumentedQueryer(reader, instruments, "semantic_search_snapshot")
	vectorConfig := DefaultPersistedLocalSemanticSearchHybridConfig()
	vectorConfig.ProviderProfileID = config.ProviderProfileID
	vectorConfig.SourceClass = config.SourceClass
	vectorConfig.EmbeddingModelID = config.EmbeddingModelID
	vectorConfig.VectorIndexVersion = config.VectorIndexVersion
	vectorConfig.VectorRetrieval = config.VectorRetrieval
	return NewCachedPersistedLocalSemanticSearchHybrid(
		semanticsearch.NewPostgresSemanticSearchIndexStoreWithReadStore(reader),
		instrumentedSemanticSearchVectorMetadataStore{
			store:    pgstatus.NewEshuSearchVectorMetadataReader(metadataDB),
			database: metadataDB,
		},
		instrumentedSemanticSearchVectorValueStore{
			store:    pgstatus.NewEshuSearchVectorValueReader(valueDB),
			database: valueDB,
		},
		instrumentedSemanticSearchSnapshotStore{
			store:    NewPostgresSemanticSearchSnapshotStore(snapshotDB),
			database: snapshotDB,
		},
		config.Embedder,
		vectorConfig,
	)
}

// instrumentedSemanticSearchVectorMetadataStore carries its queryer for
// per-store telemetry labeling.
type instrumentedSemanticSearchVectorMetadataStore struct {
	store    pgstatus.EshuSearchVectorMetadataReader
	database *pgstatus.InstrumentedQueryer
}

// ListActive delegates to the Postgres vector-metadata reader.
func (s instrumentedSemanticSearchVectorMetadataStore) ListActive(
	ctx context.Context,
	filter pgstatus.EshuSearchVectorMetadataFilter,
) ([]pgstatus.EshuSearchVectorMetadata, error) {
	return s.store.ListActive(ctx, filter)
}

// instrumentedSemanticSearchVectorValueStore carries its queryer for
// per-store telemetry labeling.
type instrumentedSemanticSearchVectorValueStore struct {
	store    pgstatus.EshuSearchVectorValueReader
	database *pgstatus.InstrumentedQueryer
}

// ListActive delegates to the Postgres vector-value reader.
func (s instrumentedSemanticSearchVectorValueStore) ListActive(
	ctx context.Context,
	filter pgstatus.EshuSearchVectorValueFilter,
) ([]pgstatus.EshuSearchVectorValue, error) {
	return s.store.ListActive(ctx, filter)
}

// instrumentedSemanticSearchSnapshotStore carries its queryer for
// per-store telemetry labeling.
type instrumentedSemanticSearchSnapshotStore struct {
	store    PostgresSemanticSearchSnapshotStore
	database *pgstatus.InstrumentedQueryer
}

// Load delegates to the Postgres snapshot reader.
func (s instrumentedSemanticSearchSnapshotStore) Load(
	ctx context.Context,
	request SemanticSearchSnapshotRequest,
) (SemanticSearchSnapshot, error) {
	return s.store.Load(ctx, request)
}

// newCoreSupplyChainHandler builds the supply-chain read handler over its
// full set of Postgres-backed evidence, advisory, impact, container-image,
// and security-alert stores.
func newCoreSupplyChainHandler(
	suppressionAdmission SuppressionAdmission,
	reader db.ReadStore,
	neo4jReader GraphQuery,
	contentReader ContentStore,
	profile QueryProfile,
	readImpactFromWinners bool,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) *SupplyChainHandler {
	return &SupplyChainHandler{
		Neo4j:                        neo4jReader,
		Logger:                       logger,
		Content:                      contentReader,
		SBOMAttachments:              NewPostgresSBOMAttestationAttachmentStoreWithReadStore(reader),
		SBOMAttachmentAggregates:     NewPostgresSBOMAttestationAttachmentAggregateStoreWithReadStore(reader),
		AdvisoryEvidence:             advisory.NewPostgresEvidenceStoreWithReadStore(reader),
		AdvisoryCatalog:              advisory.NewPostgresCatalogStoreWithReadStore(reader),
		ImpactFindings:               impact.NewPostgresFindingStoreWithReadStore(reader, readImpactFromWinners),
		ImpactAggregates:             impact.NewPostgresAggregateStoreWithReadStore(reader),
		ImpactGroupedInventory:       impact.NewPostgresGroupedInventoryStoreWithReadStore(reader, readImpactFromWinners, logger),
		ImpactExplanations:           impact.NewPostgresFindingStoreWithReadStore(reader, false),
		ContainerImageIdentities:     NewPostgresContainerImageIdentityStoreWithReadStore(reader),
		ContainerImageAggregates:     NewPostgresContainerImageIdentityAggregateStoreWithReadStore(reader),
		SecurityAlerts:               alerts.NewPostgresStoreWithReadStore(reader),
		SecurityAlertAggregates:      alerts.NewPostgresAggregateStoreWithReadStore(reader),
		Readiness:                    impact.NewPostgresReadinessStoreWithReadStore(reader),
		SuppressionMutations:         suppressionAdmission,
		CloudResourceInventory:       NewPostgresCloudResourceListStoreWithReadStore(reader),
		CollectorReadiness:           NewPostgresCollectorListReadinessStoreWithReadStore(reader),
		SecurityAlertCollectionState: NewPostgresSecurityAlertCollectionStateStoreWithReadStore(reader),
		PacketResponder:              NewSupplyChainImpactPacketResponder(),
		Profile:                      profile,
		Instruments:                  instruments,
	}
}

// newCoreFreshnessHandler builds the freshness read handler. The readers
// are backed by the Postgres status store; all remain nil when db is nil.
// The changed-since leg honors the PR-3c ledger-read switch (off by
// default: no ledger SQL when the flag is unset).
func newCoreFreshnessHandler(reader db.ReadStore, profile QueryProfile, getenv func(string) string) *FreshnessHandler {
	var generations GenerationLifecycleReader
	var changedSince ChangedSinceReader
	var serviceChangedSince ServiceChangedSinceReader
	if reader != nil {
		generations = pgstatus.NewStatusStore(reader)
		ledgerStore := pgstatus.NewStatusStore(reader)
		ledgerStore.LedgerReadEnabled = linksfreshnessstore.ReadEnabled(getenvOrFunc(getenv))
		changedSince = ledgerStore
		serviceChangedSince = pgstatus.NewStatusStore(reader)
	}
	return &FreshnessHandler{
		Generations:         generations,
		ChangedSince:        changedSince,
		ServiceChangedSince: serviceChangedSince,
		Profile:             profile,
	}
}

// newCoreAdminReadStore builds the admin read port over the shared read
// store.
func newCoreAdminReadStore(reader db.ReadStore) AdminReadStore {
	return adminstore.NewReadStore(reader)
}

// coreReindexProgressSource fans the reindex progress endpoint's read-only
// inputs out to the stores that own them.
type coreReindexProgressSource struct {
	state   maintenance.StatusRequestStore
	perRepo maintenance.RepositoryReindexStore
	scopes  pgstatus.IngestionStore
}

// GetReindexState returns the fleet watermark state.
func (s coreReindexProgressSource) GetReindexState(ctx context.Context, ingester string) (internalruntime.ReindexRequest, error) {
	return s.state.GetReindexState(ctx, ingester)
}

// RepositoryReindexRequestedAt returns per-repository watermarks.
func (s coreReindexProgressSource) RepositoryReindexRequestedAt(
	ctx context.Context,
	scopeIDs []string,
) (map[string]time.Time, error) {
	return s.perRepo.RepositoryReindexRequestedAt(ctx, scopeIDs)
}

// FullReconcileState returns the reconciliation-sweep view of scopeID.
func (s coreReindexProgressSource) FullReconcileState(
	ctx context.Context,
	scopeID string,
) (scope.FullReconcileState, error) {
	return s.scopes.FullReconcileState(ctx, scopeID)
}

// wireCoreAdminRecovery binds the core-owned recovery, reindex, retirement,
// and terminal-failure admissions. Pure read wiring (writer nil) skips this
// block: the versioned #8872 admission contracts need the writer handle.
func wireCoreAdminRecovery(
	router *APIRouter,
	stores *coreRouterStores,
) error {
	writer := stores.writer
	contentReader := stores.content
	governanceAudit := stores.governanceAudit
	instruments := stores.instruments
	// Always mount: unwired handlers serve explicit 503s (fail closed),
	// never a misleading 404 for a frozen row.
	router.AdminRetirement = &AdminRetirementHandler{}
	router.AdminTerminalFailure = &AdminTerminalFailureHandler{}
	if writer == nil {
		return nil
	}
	writerHandle := pgstatus.SQLDB{DB: writer}

	// The core-owned recovery handler travels through the versioned #8872
	// recovery admission contract, never a raw store handle.
	recoveryHandler, err := recovery.NewHandler(
		pgstatus.NewRecoveryStore(writerHandle, pgstatus.WithRecoveryInstruments(instruments)),
	)
	if err != nil {
		return fmt.Errorf("new recovery handler: %w", err)
	}
	reindexer, err := internalruntime.NewStatusRequestHandler(maintenance.NewStatusRequestStore(writerHandle))
	if err != nil {
		return fmt.Errorf("new status request handler: %w", err)
	}
	router.Admin.Recovery = recoveryHandler
	router.Admin.Reindexer = reindexer
	router.Admin.Repositories = contentReader
	router.Admin.RepositoryReindexer = maintenance.NewRepositoryReindexStore(writerHandle)
	router.Admin.ReindexProgress = coreReindexProgressSource{
		state:   maintenance.NewStatusRequestStore(writerHandle),
		perRepo: maintenance.NewRepositoryReindexStore(writerHandle),
		scopes:  pgstatus.NewIngestionStore(writerHandle),
	}
	router.AdminRetirement = &AdminRetirementHandler{
		Catalog: contentReader,
		Triggerer: &retirement.Trigger{
			Marker:      pgstatus.NewRetirementStore(writerHandle),
			Fencer:      pgstatus.NewRetirementFencer(writerHandle, writerHandle),
			Purger:      pgstatus.NewRetirementPurger(writerHandle, writerHandle),
			Instruments: instruments,
			Audit:       coreRecoveryAuditAppender(governanceAudit),
		},
		Audit: coreRecoveryAuditAppender(governanceAudit),
	}
	router.AdminTerminalFailure = &AdminTerminalFailureHandler{
		Acknowledger: pgstatus.NewTerminalFailureAckStore(writerHandle, writerHandle),
		Ledger:       NewPostgresAdminStore(writer, instruments),
		Audit:        coreRecoveryAuditAppender(governanceAudit),
		Instruments:  instruments,
	}
	return nil
}
