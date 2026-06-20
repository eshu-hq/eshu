package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	defaultPersistedLocalVectorModelID      = "local-hash-v1"
	defaultPersistedLocalProviderProfileID  = "local"
	defaultPersistedLocalSourceClass        = "search_documents"
	defaultPersistedLocalVectorIndexVersion = "vector-v1"
)

// PersistedLocalSemanticSearchHybridConfig identifies the persisted vector index
// state that API and MCP semantic search may serve.
type PersistedLocalSemanticSearchHybridConfig struct {
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	CorpusLimit        int
	VectorRetrieval    searchhybrid.VectorRetrievalMode
}

// DefaultPersistedLocalSemanticSearchHybridConfig returns the deterministic
// local-hash vector identity produced by the current search-vector builder.
func DefaultPersistedLocalSemanticSearchHybridConfig() PersistedLocalSemanticSearchHybridConfig {
	return PersistedLocalSemanticSearchHybridConfig{
		ProviderProfileID:  defaultPersistedLocalProviderProfileID,
		SourceClass:        defaultPersistedLocalSourceClass,
		EmbeddingModelID:   defaultPersistedLocalVectorModelID,
		VectorIndexVersion: defaultPersistedLocalVectorIndexVersion,
		CorpusLimit:        semanticSearchLocalHybridCorpusLimit,
		VectorRetrieval:    searchhybrid.VectorRetrievalAuto,
	}
}

// SemanticSearchVectorMetadataStore loads active-generation vector readiness
// metadata for the request scope.
type SemanticSearchVectorMetadataStore interface {
	ListActive(context.Context, postgres.EshuSearchVectorMetadataFilter) ([]postgres.EshuSearchVectorMetadata, error)
}

// SemanticSearchVectorValueStore loads active-generation vector payloads for
// the request scope.
type SemanticSearchVectorValueStore interface {
	ListActive(context.Context, postgres.EshuSearchVectorValueFilter) ([]postgres.EshuSearchVectorValue, error)
}

// PersistedLocalSemanticSearchHybrid serves semantic and hybrid search from the
// persisted vector sidecar when the active generation is complete.
type PersistedLocalSemanticSearchHybrid struct {
	Documents SemanticSearchDocumentStore
	Metadata  SemanticSearchVectorMetadataStore
	Values    SemanticSearchVectorValueStore
	Embedder  searchhybrid.Embedder
	Config    PersistedLocalSemanticSearchHybridConfig
}

// NewPersistedLocalSemanticSearchHybrid creates a semantic/hybrid adapter
// backed by ready persisted vector metadata and payload rows.
func NewPersistedLocalSemanticSearchHybrid(
	documents SemanticSearchDocumentStore,
	metadata SemanticSearchVectorMetadataStore,
	values SemanticSearchVectorValueStore,
	embedder searchhybrid.Embedder,
	config PersistedLocalSemanticSearchHybridConfig,
) *PersistedLocalSemanticSearchHybrid {
	config = normalizePersistedLocalSemanticSearchHybridConfig(config)
	return &PersistedLocalSemanticSearchHybrid{
		Documents: documents,
		Metadata:  metadata,
		Values:    values,
		Embedder:  embedder,
		Config:    config,
	}
}

// NewDefaultPersistedLocalSemanticSearchHybrid creates the default deterministic
// local-hash persisted-vector adapter.
func NewDefaultPersistedLocalSemanticSearchHybrid(
	documents SemanticSearchDocumentStore,
	metadata SemanticSearchVectorMetadataStore,
	values SemanticSearchVectorValueStore,
) *PersistedLocalSemanticSearchHybrid {
	embedder, _ := searchembed.NewHashEmbedder(searchembed.DefaultDimensions)
	return NewPersistedLocalSemanticSearchHybrid(
		documents,
		metadata,
		values,
		embedder,
		DefaultPersistedLocalSemanticSearchHybridConfig(),
	)
}

// Search loads active documents and uses persisted vectors only when the
// active generation is complete, ready, and compatible with the configured
// embedder.
func (h *PersistedLocalSemanticSearchHybrid) Search(
	ctx context.Context,
	query semanticSearchIndexQuery,
) (semanticSearchIndexResult, error) {
	if err := h.validate(); err != nil {
		return semanticSearchIndexResult{}, err
	}
	rows, err := h.Documents.ListActiveDocuments(ctx, semanticSearchDocumentQuery{
		ScopeID:     query.ScopeID,
		RepoID:      query.RepoID,
		SourceKinds: query.SourceKinds,
		Limit:       h.Config.CorpusLimit,
	})
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	docs := semanticSearchDocuments(rows)
	vectors, state, err := h.readyVectors(ctx, docs, query.ScopeID)
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	if state != "ready" {
		return h.keywordFallback(ctx, query, docs, state, len(rows))
	}

	index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{
		MaxDocuments:               h.Config.CorpusLimit,
		Embedder:                   h.Embedder,
		PrecomputedDocumentVectors: vectors,
		VectorRetrieval:            h.Config.VectorRetrieval,
	})
	if err != nil {
		return h.keywordFallback(ctx, query, docs, "index_unready", len(rows))
	}
	candidates, err := (searchhybrid.Backend{Index: index}).Search(ctx, query.Request)
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	annotateSemanticSearchCandidates(candidates, map[string]string{
		"vector_source":          "persisted_local",
		"vector_retrieval_state": "ready",
	})
	return semanticSearchIndexResult{
		Candidates:           candidates,
		IndexedDocumentCount: index.Size(),
		CorpusLimit:          h.Config.CorpusLimit,
		CorpusMayBeTruncated: index.Overflow() > 0 || len(rows) >= h.Config.CorpusLimit,
		RetrievalState:       semanticSearchActiveRetrievalState(query.Request.Mode, candidates),
	}, nil
}

func (h *PersistedLocalSemanticSearchHybrid) keywordFallback(
	ctx context.Context,
	query semanticSearchIndexQuery,
	docs []searchdocs.Document,
	state string,
	rowCount int,
) (semanticSearchIndexResult, error) {
	index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{
		MaxDocuments: h.Config.CorpusLimit,
	})
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	fallbackRequest := query.Request
	fallbackRequest.Mode = searchbench.ModeKeyword
	candidates, err := (searchhybrid.Backend{Index: index}).Search(ctx, fallbackRequest)
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	annotateSemanticSearchCandidates(candidates, map[string]string{
		"vector_retrieval_state": state,
	})
	return semanticSearchIndexResult{
		Candidates:           candidates,
		IndexedDocumentCount: index.Size(),
		CorpusLimit:          h.Config.CorpusLimit,
		CorpusMayBeTruncated: index.Overflow() > 0 || rowCount >= h.Config.CorpusLimit,
		RetrievalState:       state,
	}, nil
}

func (h *PersistedLocalSemanticSearchHybrid) readyVectors(
	ctx context.Context,
	docs []searchdocs.Document,
	scopeID string,
) (map[string][]float64, string, error) {
	limit := h.Config.CorpusLimit
	documentIDs := semanticSearchDocumentIDs(docs)
	metadataRows, err := h.Metadata.ListActive(ctx, postgres.EshuSearchVectorMetadataFilter{
		ScopeID:            scopeID,
		ProviderProfileID:  h.Config.ProviderProfileID,
		SourceClass:        h.Config.SourceClass,
		EmbeddingModelID:   h.Config.EmbeddingModelID,
		VectorIndexVersion: h.Config.VectorIndexVersion,
		DocumentIDs:        documentIDs,
		Limit:              limit,
	})
	if err != nil {
		return nil, "", err
	}
	valueRows, err := h.Values.ListActive(ctx, postgres.EshuSearchVectorValueFilter{
		ScopeID:            scopeID,
		ProviderProfileID:  h.Config.ProviderProfileID,
		SourceClass:        h.Config.SourceClass,
		EmbeddingModelID:   h.Config.EmbeddingModelID,
		VectorIndexVersion: h.Config.VectorIndexVersion,
		DocumentIDs:        documentIDs,
		Limit:              limit,
	})
	if err != nil {
		return nil, "", err
	}
	metadataByDoc := make(map[string]postgres.EshuSearchVectorMetadata, len(metadataRows))
	for _, row := range metadataRows {
		metadataByDoc[row.DocumentID] = row
	}
	valuesByDoc := make(map[string]postgres.EshuSearchVectorValue, len(valueRows))
	for _, row := range valueRows {
		valuesByDoc[row.DocumentID] = row
	}
	vectors := make(map[string][]float64, len(docs))
	for _, doc := range docs {
		metadata, ok := metadataByDoc[doc.ID]
		if !ok || metadata.BuildState != postgres.EshuSearchVectorBuildStateReady {
			return nil, "index_unready", nil
		}
		value, ok := valuesByDoc[doc.ID]
		if !ok {
			return nil, "index_unready", nil
		}
		if !compatiblePersistedVector(doc, metadata, value, h.Embedder.Dimensions()) {
			return nil, "index_unready", nil
		}
		vectors[doc.ID] = append([]float64(nil), value.VectorValues...)
	}
	if len(vectors) != len(docs) {
		return nil, "index_unready", nil
	}
	return vectors, "ready", nil
}

func (h *PersistedLocalSemanticSearchHybrid) validate() error {
	if h == nil {
		return fmt.Errorf("persisted local semantic search hybrid is required")
	}
	if h.Documents == nil {
		return fmt.Errorf("semantic search document store is required")
	}
	if h.Metadata == nil {
		return fmt.Errorf("semantic search vector metadata store is required")
	}
	if h.Values == nil {
		return fmt.Errorf("semantic search vector value store is required")
	}
	if h.Embedder == nil || h.Embedder.Dimensions() <= 0 {
		return fmt.Errorf("semantic search local embedder is required")
	}
	return nil
}

func semanticSearchDocuments(rows []semanticSearchDocumentRow) []searchdocs.Document {
	docs := make([]searchdocs.Document, 0, len(rows))
	for _, row := range rows {
		docs = append(docs, row.Document)
	}
	return docs
}

func semanticSearchDocumentIDs(docs []searchdocs.Document) []string {
	seen := make(map[string]struct{}, len(docs))
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		if doc.ID == "" {
			continue
		}
		if _, ok := seen[doc.ID]; ok {
			continue
		}
		seen[doc.ID] = struct{}{}
		ids = append(ids, doc.ID)
	}
	slices.Sort(ids)
	return ids
}

func compatiblePersistedVector(
	doc searchdocs.Document,
	metadata postgres.EshuSearchVectorMetadata,
	value postgres.EshuSearchVectorValue,
	dimensions int,
) bool {
	hash := searchhybrid.DocumentContentHash(doc)
	return metadata.GenerationID == value.GenerationID &&
		metadata.ScopeID == value.ScopeID &&
		metadata.DocumentID == value.DocumentID &&
		metadata.ProviderProfileID == value.ProviderProfileID &&
		metadata.SourceClass == value.SourceClass &&
		metadata.EmbeddingModelID == value.EmbeddingModelID &&
		metadata.VectorIndexVersion == value.VectorIndexVersion &&
		metadata.EmbeddingDimensions == dimensions &&
		value.EmbeddingDimensions == dimensions &&
		metadata.EmbeddingContentHash == hash &&
		value.EmbeddingContentHash == hash
}

func annotateSemanticSearchCandidates(candidates []searchretrieval.Candidate, metadata map[string]string) {
	for i := range candidates {
		if candidates[i].Metadata == nil {
			candidates[i].Metadata = map[string]string{}
		}
		for key, value := range metadata {
			candidates[i].Metadata[key] = value
		}
	}
}

func normalizePersistedLocalSemanticSearchHybridConfig(
	config PersistedLocalSemanticSearchHybridConfig,
) PersistedLocalSemanticSearchHybridConfig {
	if config.EmbeddingModelID == "" {
		config.EmbeddingModelID = defaultPersistedLocalVectorModelID
	}
	if config.ProviderProfileID == "" {
		config.ProviderProfileID = defaultPersistedLocalProviderProfileID
	}
	if config.SourceClass == "" {
		config.SourceClass = defaultPersistedLocalSourceClass
	}
	if config.VectorIndexVersion == "" {
		config.VectorIndexVersion = defaultPersistedLocalVectorIndexVersion
	}
	if config.CorpusLimit <= 0 || config.CorpusLimit > semanticSearchLocalHybridCorpusLimit {
		config.CorpusLimit = semanticSearchLocalHybridCorpusLimit
	}
	if config.VectorRetrieval == "" {
		config.VectorRetrieval = searchhybrid.VectorRetrievalAuto
	}
	return config
}
