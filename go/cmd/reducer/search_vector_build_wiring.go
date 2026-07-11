// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	"github.com/eshu-hq/eshu/go/internal/searchvector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const envSemanticSearchLocalEmbedder = searchembedruntime.EnvLocalEmbedder

func searchVectorBuildRunnerFor(
	database postgres.ExecQueryer,
	getenv func(string) string,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) (*reducer.SearchVectorBuildRunner, error) {
	embeddingConfig, err := searchembedruntime.ConfigFromEnv(getenv, nil)
	if err != nil {
		return nil, err
	}
	if !embeddingConfig.Enabled {
		return nil, nil
	}
	vectorConfig := query.DefaultPersistedLocalSemanticSearchHybridConfig()
	vectorConfig.ProviderProfileID = embeddingConfig.ProviderProfileID
	vectorConfig.SourceClass = embeddingConfig.SourceClass
	vectorConfig.EmbeddingModelID = embeddingConfig.EmbeddingModelID
	vectorConfig.VectorIndexVersion = embeddingConfig.VectorIndexVersion
	scopeStateStore := postgres.NewEshuSearchVectorScopeStateStore(database)
	return &reducer.SearchVectorBuildRunner{
		Pending: searchVectorScopeStatePendingAdapter{store: scopeStateStore},
		Builder: searchVectorBuilderAdapter{builder: searchvector.Builder{
			Documents: postgres.NewEshuSearchDocumentStore(database),
			Metadata:  postgres.NewEshuSearchVectorMetadataStore(database),
			Values:    postgres.NewEshuSearchVectorValueStore(database),
			Embedder:  embeddingConfig.Embedder,
			DocumentAllowed: func(row postgres.EshuSearchDocumentRow) bool {
				return embeddingConfig.AllowsSearchDocument(row.Document.RepoID, row.Document.ID, row.Document.Path)
			},
		}},
		Config: reducer.SearchVectorBuildRunnerConfig{
			PollInterval:       30 * time.Second,
			ScopeLimit:         100,
			DocumentLimit:      500,
			ProviderProfileID:  vectorConfig.ProviderProfileID,
			SourceClass:        vectorConfig.SourceClass,
			EmbeddingModelID:   vectorConfig.EmbeddingModelID,
			VectorIndexVersion: vectorConfig.VectorIndexVersion,
		},
		Logger:         logger,
		Instruments:    instruments,
		ReadyPublisher: searchVectorReadyPublisherAdapter{store: postgres.NewEshuSearchVectorBuildReadyStore(database)},
		ScopeState:     searchVectorScopeStateManagerAdapter{store: scopeStateStore},
	}, nil
}

type searchVectorReadyPublisherAdapter struct {
	store postgres.EshuSearchVectorBuildReadyStore
}

func (a searchVectorReadyPublisherAdapter) PublishSearchVectorReady(
	ctx context.Context,
	identity reducer.SearchVectorBuildIdentity,
) error {
	return a.store.PublishSearchVectorReady(ctx, postgres.EshuSearchVectorBuildIdentity{
		ProviderProfileID:  identity.ProviderProfileID,
		SourceClass:        identity.SourceClass,
		EmbeddingModelID:   identity.EmbeddingModelID,
		VectorIndexVersion: identity.VectorIndexVersion,
	})
}

type searchVectorBuilderAdapter struct {
	builder searchvector.Builder
}

func (a searchVectorBuilderAdapter) BuildSearchVectors(
	ctx context.Context,
	req reducer.SearchVectorBuildRequest,
) (reducer.SearchVectorBuildResult, error) {
	result, err := a.builder.Build(ctx, searchvector.BuildRequest{
		ScopeID:            req.ScopeID,
		GenerationID:       req.GenerationID,
		RepoID:             req.RepoID,
		ProviderProfileID:  req.ProviderProfileID,
		SourceClass:        req.SourceClass,
		EmbeddingModelID:   req.EmbeddingModelID,
		VectorIndexVersion: req.VectorIndexVersion,
		Limit:              req.Limit,
		ProjectionRevision: req.ProjectionRevision,
		BuildFence:         req.BuildFence,
	})
	return reducer.SearchVectorBuildResult{
		DocumentCount:       result.DocumentCount,
		VectorCount:         result.VectorCount,
		DisabledCount:       result.DisabledCount,
		FailedCount:         result.FailedCount,
		QueryLoadDuration:   result.QueryLoadDuration,
		EmbedBuildDuration:  result.EmbedBuildDuration,
		WriteUpsertDuration: result.WriteUpsertDuration,
	}, err
}

func (a searchVectorBuilderAdapter) BuildSearchVectorsBatch(
	ctx context.Context,
	reqs []reducer.SearchVectorBuildRequest,
) (reducer.SearchVectorBuildResult, error) {
	buildReqs := make([]searchvector.BuildRequest, 0, len(reqs))
	for _, req := range reqs {
		buildReqs = append(buildReqs, searchvector.BuildRequest{
			ScopeID:            req.ScopeID,
			GenerationID:       req.GenerationID,
			RepoID:             req.RepoID,
			ProviderProfileID:  req.ProviderProfileID,
			SourceClass:        req.SourceClass,
			EmbeddingModelID:   req.EmbeddingModelID,
			VectorIndexVersion: req.VectorIndexVersion,
			Limit:              req.Limit,
			ProjectionRevision: req.ProjectionRevision,
			BuildFence:         req.BuildFence,
		})
	}
	result, err := a.builder.BuildBatch(ctx, buildReqs)
	return reducer.SearchVectorBuildResult{
		DocumentCount:       result.DocumentCount,
		VectorCount:         result.VectorCount,
		DisabledCount:       result.DisabledCount,
		FailedCount:         result.FailedCount,
		QueryLoadDuration:   result.QueryLoadDuration,
		EmbedBuildDuration:  result.EmbedBuildDuration,
		WriteUpsertDuration: result.WriteUpsertDuration,
	}, err
}

// searchVectorScopeStatePendingAdapter wraps the #4233
// EshuSearchVectorScopeStateStore as the reducer's
// SearchVectorBuildPendingLister, mapping ProjectionRevision through so the
// build runner can CAS-finalize by revision.
type searchVectorScopeStatePendingAdapter struct {
	store postgres.EshuSearchVectorScopeStateStore
}

func (a searchVectorScopeStatePendingAdapter) ListPendingSearchVectorScopes(
	ctx context.Context,
	req reducer.SearchVectorBuildPendingRequest,
) ([]reducer.SearchVectorBuildPendingScope, error) {
	scopes, err := a.store.ListPendingSearchVectorScopes(ctx, postgres.EshuSearchVectorPendingRequest{
		ProviderProfileID:  req.ProviderProfileID,
		SourceClass:        req.SourceClass,
		EmbeddingModelID:   req.EmbeddingModelID,
		VectorIndexVersion: req.VectorIndexVersion,
		Limit:              req.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]reducer.SearchVectorBuildPendingScope, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, reducer.SearchVectorBuildPendingScope{
			ScopeID:            scope.ScopeID,
			GenerationID:       scope.GenerationID,
			RepoID:             scope.RepoID,
			ProjectionRevision: scope.ProjectionRevision,
		})
	}
	return out, nil
}

// searchVectorScopeStateManagerAdapter wraps EshuSearchVectorScopeStateStore
// as the reducer's SearchVectorScopeStateManager, mapping
// reducer.SearchVectorBuildIdentity ↔ postgres.EshuSearchVectorIdentity.
type searchVectorScopeStateManagerAdapter struct {
	store postgres.EshuSearchVectorScopeStateStore
}

func (a searchVectorScopeStateManagerAdapter) BeginBuilding(
	ctx context.Context,
	scopeID, generationID string,
	identity reducer.SearchVectorBuildIdentity,
	projectionRevision int64,
) (int64, error) {
	return a.store.BeginBuilding(ctx, scopeID, generationID, postgres.EshuSearchVectorIdentity{
		ProviderProfileID:  identity.ProviderProfileID,
		SourceClass:        identity.SourceClass,
		EmbeddingModelID:   identity.EmbeddingModelID,
		VectorIndexVersion: identity.VectorIndexVersion,
	}, projectionRevision)
}

func (a searchVectorScopeStateManagerAdapter) ScopeVectorComplete(
	ctx context.Context,
	scopeID, generationID string,
	identity reducer.SearchVectorBuildIdentity,
) (bool, error) {
	return a.store.ScopeVectorComplete(ctx, scopeID, generationID, postgres.EshuSearchVectorIdentity{
		ProviderProfileID:  identity.ProviderProfileID,
		SourceClass:        identity.SourceClass,
		EmbeddingModelID:   identity.EmbeddingModelID,
		VectorIndexVersion: identity.VectorIndexVersion,
	})
}

func (a searchVectorScopeStateManagerAdapter) FinalizeReady(
	ctx context.Context,
	scopeID, generationID string,
	identity reducer.SearchVectorBuildIdentity,
	projectionRevision, fence int64,
) (bool, error) {
	return a.store.FinalizeReady(ctx, scopeID, generationID, postgres.EshuSearchVectorIdentity{
		ProviderProfileID:  identity.ProviderProfileID,
		SourceClass:        identity.SourceClass,
		EmbeddingModelID:   identity.EmbeddingModelID,
		VectorIndexVersion: identity.VectorIndexVersion,
	}, projectionRevision, fence)
}
