// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchvector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// BuildBatch embeds active pending documents for several selected scopes.
// Stores that do not support batched pending selection fall back to the
// per-scope Build path so non-Postgres adapters keep their existing behavior.
func (b Builder) BuildBatch(ctx context.Context, reqs []BuildRequest) (BuildResult, error) {
	if len(reqs) == 0 {
		return BuildResult{}, nil
	}
	normalized, err := normalizeBatchBuildRequests(reqs)
	if err != nil {
		return BuildResult{}, err
	}
	for _, req := range normalized {
		if err := b.validate(req); err != nil {
			return BuildResult{}, err
		}
	}
	if batch, ok := b.Documents.(BatchPendingDocumentStore); ok {
		return b.buildBatchPendingDocuments(ctx, batch, normalized)
	}
	return b.buildBatchSerial(ctx, normalized)
}

func (b Builder) buildBatchSerial(ctx context.Context, reqs []BuildRequest) (BuildResult, error) {
	var result BuildResult
	var failures []error
	for _, req := range reqs {
		build, err := b.Build(ctx, req)
		result.DocumentCount += build.DocumentCount
		result.VectorCount += build.VectorCount
		result.DisabledCount += build.DisabledCount
		result.FailedCount += build.FailedCount
		result.QueryLoadDuration += build.QueryLoadDuration
		result.EmbedBuildDuration += build.EmbedBuildDuration
		result.WriteUpsertDuration += build.WriteUpsertDuration
		if err != nil {
			failures = append(failures, err)
		}
	}
	return result, errors.Join(failures...)
}

func (b Builder) buildBatchPendingDocuments(
	ctx context.Context,
	store BatchPendingDocumentStore,
	reqs []BuildRequest,
) (BuildResult, error) {
	now := b.now()
	loadStart := time.Now()
	rows, err := store.ListPendingVectorDocumentsForScopes(ctx, postgres.EshuSearchVectorDocumentBatchFilter{
		Scopes:             buildPendingDocumentScopes(reqs),
		SourceKinds:        reqs[0].SourceKinds,
		ProviderProfileID:  reqs[0].ProviderProfileID,
		SourceClass:        reqs[0].SourceClass,
		EmbeddingModelID:   reqs[0].EmbeddingModelID,
		VectorIndexVersion: reqs[0].VectorIndexVersion,
		Limit:              reqs[0].Limit,
	})
	result := BuildResult{QueryLoadDuration: time.Since(loadStart)}
	if err != nil {
		return result, fmt.Errorf("list batched pending search documents for vector build: %w", err)
	}
	var failures []error
	if err := b.buildDocumentRowsAcrossScopes(ctx, reqs[0], now, rows, &result, &failures); err != nil {
		return result, err
	}
	return result, errors.Join(failures...)
}

func (b Builder) buildDocumentRowsAcrossScopes(
	ctx context.Context,
	req BuildRequest,
	now time.Time,
	rows []postgres.EshuSearchDocumentRow,
	result *BuildResult,
	failures *[]error,
) error {
	generations := make(map[string]string, len(rows))
	metadataBatch := make([]postgres.EshuSearchVectorMetadata, 0, len(rows))
	valueBatch := make([]postgres.EshuSearchVectorValue, 0, len(rows))
	for _, row := range rows {
		if existing, ok := generations[row.ScopeID]; ok && existing != row.GenerationID {
			return fmt.Errorf("active search document generation for scope %q changed from %q to %q", row.ScopeID, existing, row.GenerationID)
		}
		generations[row.ScopeID] = row.GenerationID
		result.DocumentCount++
		if b.DocumentAllowed != nil && !b.DocumentAllowed(row) {
			metadataBatch = append(metadataBatch, b.metadataRow(req, row, now, postgres.EshuSearchVectorBuildStateDisabled, FailureClassPolicyDenied, nil))
			result.DisabledCount++
			continue
		}
		embedStart := time.Now()
		vector, failureClass, err := b.embed(ctx, row.Document)
		result.EmbedBuildDuration += time.Since(embedStart)
		if err != nil {
			metadataBatch = append(metadataBatch, b.metadataRow(req, row, now, postgres.EshuSearchVectorBuildStateFailed, failureClass, nil))
			result.FailedCount++
			*failures = append(*failures, fmt.Errorf("%s: %w", failureClass, err))
			continue
		}
		valueBatch = append(valueBatch, b.buildBatchedValueRow(req, row, now, vector))
		metadataBatch = append(metadataBatch, b.metadataRow(req, row, now, postgres.EshuSearchVectorBuildStateReady, "", &now))
		result.VectorCount++
	}

	writeStart := time.Now()
	if err := b.Values.UpsertBatch(ctx, dedupeValueBatch(valueBatch)); err != nil {
		return fmt.Errorf("upsert vector value batch (rows=%d): %w", len(valueBatch), err)
	}
	if err := b.Metadata.UpsertBatch(ctx, dedupeMetadataBatch(metadataBatch)); err != nil {
		return fmt.Errorf("upsert vector metadata batch (rows=%d): %w", len(metadataBatch), err)
	}
	result.WriteUpsertDuration += time.Since(writeStart)
	return nil
}

func normalizeBatchBuildRequests(reqs []BuildRequest) ([]BuildRequest, error) {
	out := make([]BuildRequest, 0, len(reqs))
	for _, req := range reqs {
		req = normalizeBuildRequest(req)
		if len(out) > 0 {
			first := out[0]
			if !buildRequestsShareBatchTuple(req, first) {
				return nil, fmt.Errorf("batched vector build requests must share provider, source, model, index version, source kinds, and limit")
			}
		}
		out = append(out, req)
	}
	return out, nil
}

func buildRequestsShareBatchTuple(req BuildRequest, first BuildRequest) bool {
	if req.ProviderProfileID != first.ProviderProfileID ||
		req.SourceClass != first.SourceClass ||
		req.EmbeddingModelID != first.EmbeddingModelID ||
		req.VectorIndexVersion != first.VectorIndexVersion ||
		req.Limit != first.Limit ||
		len(req.SourceKinds) != len(first.SourceKinds) {
		return false
	}
	for i := range req.SourceKinds {
		if req.SourceKinds[i] != first.SourceKinds[i] {
			return false
		}
	}
	return true
}

func buildPendingDocumentScopes(reqs []BuildRequest) []postgres.EshuSearchVectorDocumentScope {
	scopes := make([]postgres.EshuSearchVectorDocumentScope, 0, len(reqs))
	for _, req := range reqs {
		scopes = append(scopes, postgres.EshuSearchVectorDocumentScope{
			ScopeID:      req.ScopeID,
			GenerationID: req.GenerationID,
			RepoID:       req.RepoID,
		})
	}
	return scopes
}

func (b Builder) buildBatchedValueRow(
	req BuildRequest,
	row postgres.EshuSearchDocumentRow,
	now time.Time,
	vector []float64,
) postgres.EshuSearchVectorValue {
	return postgres.EshuSearchVectorValue{
		ScopeID:              row.ScopeID,
		GenerationID:         row.GenerationID,
		DocumentID:           row.Document.ID,
		ProviderProfileID:    req.ProviderProfileID,
		SourceClass:          req.SourceClass,
		EmbeddingModelID:     req.EmbeddingModelID,
		EmbeddingDimensions:  b.Embedder.Dimensions(),
		EmbeddingContentHash: searchhybrid.DocumentContentHash(row.Document),
		VectorIndexVersion:   req.VectorIndexVersion,
		VectorValues:         vector,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}
