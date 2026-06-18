package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchvector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const envSemanticSearchLocalEmbedder = "ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER"

func loadSemanticSearchLocalEmbedder(getenv func(string) string) (string, error) {
	raw := strings.TrimSpace(getenv(envSemanticSearchLocalEmbedder))
	switch raw {
	case "", "hash", "local_hash":
		return raw, nil
	default:
		return "", fmt.Errorf("invalid %s %q", envSemanticSearchLocalEmbedder, raw)
	}
}

func searchVectorBuildRunnerFor(
	database postgres.ExecQueryer,
	getenv func(string) string,
	logger *slog.Logger,
) (*reducer.SearchVectorBuildRunner, error) {
	localEmbedder, err := loadSemanticSearchLocalEmbedder(getenv)
	if err != nil {
		return nil, err
	}
	if localEmbedder == "" {
		return nil, nil
	}
	embedder, err := searchembed.NewHashEmbedder(searchembed.DefaultDimensions)
	if err != nil {
		return nil, err
	}
	vectorConfig := query.DefaultPersistedLocalSemanticSearchHybridConfig()
	return &reducer.SearchVectorBuildRunner{
		Pending: searchVectorPendingAdapter{store: postgres.NewEshuSearchVectorPendingStore(database)},
		Builder: searchVectorBuilderAdapter{builder: searchvector.Builder{
			Documents: postgres.NewEshuSearchDocumentStore(database),
			Metadata:  postgres.NewEshuSearchVectorMetadataStore(database),
			Values:    postgres.NewEshuSearchVectorValueStore(database),
			Embedder:  embedder,
		}},
		Config: reducer.SearchVectorBuildRunnerConfig{
			PollInterval:       30 * time.Second,
			ScopeLimit:         100,
			DocumentLimit:      500,
			EmbeddingModelID:   vectorConfig.EmbeddingModelID,
			VectorIndexVersion: vectorConfig.VectorIndexVersion,
		},
		Logger: logger,
	}, nil
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
		RepoID:             req.RepoID,
		EmbeddingModelID:   req.EmbeddingModelID,
		VectorIndexVersion: req.VectorIndexVersion,
		Limit:              req.Limit,
	})
	return reducer.SearchVectorBuildResult{
		DocumentCount: result.DocumentCount,
		VectorCount:   result.VectorCount,
		FailedCount:   result.FailedCount,
	}, err
}

type searchVectorPendingAdapter struct {
	store postgres.EshuSearchVectorPendingStore
}

func (a searchVectorPendingAdapter) ListPendingSearchVectorScopes(
	ctx context.Context,
	req reducer.SearchVectorBuildPendingRequest,
) ([]reducer.SearchVectorBuildPendingScope, error) {
	scopes, err := a.store.ListPendingSearchVectorScopes(ctx, postgres.EshuSearchVectorPendingRequest{
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
			ScopeID:      scope.ScopeID,
			GenerationID: scope.GenerationID,
			RepoID:       scope.RepoID,
		})
	}
	return out, nil
}
