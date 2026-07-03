// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchvector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const defaultBuildLimit = 500

const (
	// FailureClassEmbedder records a bounded embedder failure.
	FailureClassEmbedder = "embedder_error"
	// FailureClassInvalidVector records a malformed vector returned by an embedder.
	FailureClassInvalidVector = "invalid_vector"
	// FailureClassPolicyDenied records a source-policy-denied document.
	FailureClassPolicyDenied = "policy_denied"
)

// DocumentStore reads active curated search documents for a bounded scope.
type DocumentStore interface {
	ListActiveDocuments(context.Context, postgres.EshuSearchDocumentFilter) ([]postgres.EshuSearchDocumentRow, error)
}

// PendingDocumentStore narrows vector builds to active documents that do not
// already have a ready/disabled vector row for the requested embedding tuple.
type PendingDocumentStore interface {
	ListPendingVectorDocuments(context.Context, postgres.EshuSearchVectorDocumentFilter) ([]postgres.EshuSearchDocumentRow, error)
}

// BatchPendingDocumentStore narrows vector builds across multiple selected
// scopes with one bounded pending-document query.
type BatchPendingDocumentStore interface {
	ListPendingVectorDocumentsForScopes(context.Context, postgres.EshuSearchVectorDocumentBatchFilter) ([]postgres.EshuSearchDocumentRow, error)
}

// MetadataStore persists vector build metadata rows. UpsertBatch takes a
// bounded slice per document page so the builder issues one multi-row
// statement per page instead of one round trip per document (#4430).
type MetadataStore interface {
	UpsertBatch(context.Context, []postgres.EshuSearchVectorMetadata) error
}

// ValueStore persists vector payload rows. UpsertBatch takes a bounded slice
// per document page so the builder issues one multi-row statement per page
// instead of one round trip per document (#4430).
type ValueStore interface {
	UpsertBatch(context.Context, []postgres.EshuSearchVectorValue) error
}

// Builder builds vector rows from active curated search documents.
type Builder struct {
	Documents DocumentStore
	Metadata  MetadataStore
	Values    ValueStore
	Embedder  searchhybrid.Embedder
	// DocumentAllowed gates hosted/provider builds per source document. Nil
	// means every document in the already-bounded scope is allowed.
	DocumentAllowed func(postgres.EshuSearchDocumentRow) bool
	Clock           func() time.Time
}

// BuildRequest identifies the active search-document slice and vector identity
// to build.
type BuildRequest struct {
	ScopeID            string
	GenerationID       string
	RepoID             string
	SourceKinds        []searchdocs.SourceKind
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	Limit              int
}

// BuildResult summarizes a vector build attempt.
type BuildResult struct {
	DocumentCount int
	VectorCount   int
	DisabledCount int
	FailedCount   int
	// QueryLoadDuration is time spent listing active search documents.
	QueryLoadDuration time.Duration
	// EmbedBuildDuration is time spent embedding document text.
	EmbedBuildDuration time.Duration
	// WriteUpsertDuration is time spent in batched metadata/value upserts.
	WriteUpsertDuration time.Duration
}

// Build embeds active search documents and upserts derived vector state.
// Documents are processed one page (bounded by req.Limit) at a time; each
// page's metadata and value rows are written with one batched multi-row
// upsert instead of one round trip per document (#4430), collapsing what was
// previously 2*document_count sequential statements per scope sweep into
// 2*ceil(document_count/req.Limit).
func (b Builder) Build(ctx context.Context, req BuildRequest) (BuildResult, error) {
	req = normalizeBuildRequest(req)
	if err := b.validate(req); err != nil {
		return BuildResult{}, err
	}
	now := b.now()

	var result BuildResult
	var failures []error
	if pending, ok := b.Documents.(PendingDocumentStore); ok {
		loadStart := time.Now()
		rows, err := pending.ListPendingVectorDocuments(ctx, postgres.EshuSearchVectorDocumentFilter{
			EshuSearchDocumentFilter: postgres.EshuSearchDocumentFilter{
				ScopeID:      req.ScopeID,
				GenerationID: req.GenerationID,
				RepoID:       req.RepoID,
				SourceKinds:  req.SourceKinds,
				Limit:        req.Limit,
			},
			ProviderProfileID:  req.ProviderProfileID,
			SourceClass:        req.SourceClass,
			EmbeddingModelID:   req.EmbeddingModelID,
			VectorIndexVersion: req.VectorIndexVersion,
		})
		result.QueryLoadDuration += time.Since(loadStart)
		if err != nil {
			return result, fmt.Errorf("list pending search documents for vector build: %w", err)
		}
		if _, err := b.buildDocumentRows(ctx, req, now, rows, "", &result, &failures); err != nil {
			return result, err
		}
		return result, errors.Join(failures...)
	}

	generationID := ""
	for offset := 0; ; {
		loadStart := time.Now()
		rows, err := b.Documents.ListActiveDocuments(ctx, postgres.EshuSearchDocumentFilter{
			ScopeID:      req.ScopeID,
			GenerationID: generationID,
			RepoID:       req.RepoID,
			SourceKinds:  req.SourceKinds,
			Limit:        req.Limit,
			Offset:       offset,
		})
		result.QueryLoadDuration += time.Since(loadStart)
		if err != nil {
			return result, fmt.Errorf("list active search documents for vector build: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		nextGenerationID, err := b.buildDocumentRows(ctx, req, now, rows, generationID, &result, &failures)
		if err != nil {
			return result, err
		}
		if generationID == "" {
			generationID = nextGenerationID
		}

		offset += len(rows)
		if len(rows) < req.Limit {
			break
		}
	}
	return result, errors.Join(failures...)
}

func (b Builder) buildDocumentRows(
	ctx context.Context,
	req BuildRequest,
	now time.Time,
	rows []postgres.EshuSearchDocumentRow,
	generationID string,
	result *BuildResult,
	failures *[]error,
) (string, error) {
	if len(rows) == 0 {
		return generationID, nil
	}
	if generationID == "" {
		generationID = rows[0].GenerationID
	}

	metadataBatch := make([]postgres.EshuSearchVectorMetadata, 0, len(rows))
	valueBatch := make([]postgres.EshuSearchVectorValue, 0, len(rows))
	for _, row := range rows {
		if row.GenerationID != generationID {
			return generationID, fmt.Errorf("active search document generation changed from %q to %q", generationID, row.GenerationID)
		}
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

		valueBatch = append(valueBatch, postgres.EshuSearchVectorValue{
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
		})
		metadataBatch = append(metadataBatch, b.metadataRow(req, row, now, postgres.EshuSearchVectorBuildStateReady, "", &now))
		result.VectorCount++
	}

	writeStart := time.Now()
	if err := b.Values.UpsertBatch(ctx, dedupeValueBatch(valueBatch)); err != nil {
		return generationID, fmt.Errorf(
			"upsert vector value batch (scope=%s generation=%s rows=%d): %w",
			req.ScopeID, generationID, len(valueBatch), err)
	}
	if err := b.Metadata.UpsertBatch(ctx, dedupeMetadataBatch(metadataBatch)); err != nil {
		return generationID, fmt.Errorf(
			"upsert vector metadata batch (scope=%s generation=%s rows=%d): %w",
			req.ScopeID, generationID, len(metadataBatch), err)
	}
	result.WriteUpsertDuration += time.Since(writeStart)
	return generationID, nil
}

// dedupeValueBatch keeps the last row per vector-table identity, matching the
// last-write-wins outcome of the pre-#4430 sequential per-document Upsert
// calls. A single multi-row INSERT ... ON CONFLICT DO UPDATE statement errors
// if it contains two rows with the same conflict key. Deduping by the full
// conflict key keeps same-document duplicates within one scope safe without
// collapsing equal document IDs from different scopes in a batched sweep.
func dedupeValueBatch(rows []postgres.EshuSearchVectorValue) []postgres.EshuSearchVectorValue {
	if len(rows) < 2 {
		return rows
	}
	byIdentity := make(map[string]int, len(rows))
	deduped := make([]postgres.EshuSearchVectorValue, 0, len(rows))
	for _, row := range rows {
		key := vectorValueIdentity(row)
		if idx, ok := byIdentity[key]; ok {
			deduped[idx] = row
			continue
		}
		byIdentity[key] = len(deduped)
		deduped = append(deduped, row)
	}
	return deduped
}

// dedupeMetadataBatch is dedupeValueBatch's counterpart for metadata rows. See
// dedupeValueBatch for why this is required once upserts are batched.
func dedupeMetadataBatch(rows []postgres.EshuSearchVectorMetadata) []postgres.EshuSearchVectorMetadata {
	if len(rows) < 2 {
		return rows
	}
	byIdentity := make(map[string]int, len(rows))
	deduped := make([]postgres.EshuSearchVectorMetadata, 0, len(rows))
	for _, row := range rows {
		key := vectorMetadataIdentity(row)
		if idx, ok := byIdentity[key]; ok {
			deduped[idx] = row
			continue
		}
		byIdentity[key] = len(deduped)
		deduped = append(deduped, row)
	}
	return deduped
}

func vectorValueIdentity(row postgres.EshuSearchVectorValue) string {
	return strings.Join([]string{
		row.ScopeID,
		row.GenerationID,
		row.DocumentID,
		row.ProviderProfileID,
		row.SourceClass,
		row.EmbeddingModelID,
		row.VectorIndexVersion,
	}, "\x00")
}

func vectorMetadataIdentity(row postgres.EshuSearchVectorMetadata) string {
	return strings.Join([]string{
		row.ScopeID,
		row.GenerationID,
		row.DocumentID,
		row.ProviderProfileID,
		row.SourceClass,
		row.EmbeddingModelID,
		row.VectorIndexVersion,
	}, "\x00")
}

func (b Builder) embed(ctx context.Context, doc searchdocs.Document) ([]float64, string, error) {
	vector, err := b.Embedder.Embed(ctx, searchhybrid.DocumentText(doc))
	if err != nil {
		return nil, FailureClassEmbedder, err
	}
	if err := validateVector(vector, b.Embedder.Dimensions()); err != nil {
		return nil, FailureClassInvalidVector, err
	}
	return vector, "", nil
}

// metadataRow builds one vector metadata row for the batched upsert. It does
// no I/O; callers accumulate rows per document page and write them with one
// batched Metadata.UpsertBatch call (#4430).
func (b Builder) metadataRow(
	req BuildRequest,
	row postgres.EshuSearchDocumentRow,
	now time.Time,
	state postgres.EshuSearchVectorBuildState,
	failureClass string,
	lastSuccessAt *time.Time,
) postgres.EshuSearchVectorMetadata {
	return postgres.EshuSearchVectorMetadata{
		ScopeID:              row.ScopeID,
		GenerationID:         row.GenerationID,
		DocumentID:           row.Document.ID,
		ProviderProfileID:    req.ProviderProfileID,
		SourceClass:          req.SourceClass,
		EmbeddingModelID:     req.EmbeddingModelID,
		EmbeddingDimensions:  b.Embedder.Dimensions(),
		EmbeddingContentHash: searchhybrid.DocumentContentHash(row.Document),
		VectorIndexVersion:   req.VectorIndexVersion,
		BuildState:           state,
		FailureClass:         failureClass,
		CreatedAt:            now,
		UpdatedAt:            now,
		LastSuccessAt:        lastSuccessAt,
	}
}

func (b Builder) validate(req BuildRequest) error {
	var problems []error
	if b.Documents == nil {
		problems = append(problems, errors.New("document store is required"))
	}
	if b.Metadata == nil {
		problems = append(problems, errors.New("metadata store is required"))
	}
	if b.Values == nil {
		problems = append(problems, errors.New("value store is required"))
	}
	if b.Embedder == nil {
		problems = append(problems, errors.New("embedder is required"))
	} else if b.Embedder.Dimensions() <= 0 {
		problems = append(problems, errors.New("embedder dimensions must be positive"))
	}
	if req.ScopeID == "" {
		problems = append(problems, errors.New("scope id is required"))
	}
	if req.ProviderProfileID == "" {
		problems = append(problems, errors.New("provider profile id is required"))
	}
	if req.SourceClass == "" {
		problems = append(problems, errors.New("source class is required"))
	}
	if req.EmbeddingModelID == "" {
		problems = append(problems, errors.New("embedding model id is required"))
	}
	if req.VectorIndexVersion == "" {
		problems = append(problems, errors.New("vector index version is required"))
	}
	return errors.Join(problems...)
}

func (b Builder) now() time.Time {
	if b.Clock != nil {
		return b.Clock().UTC()
	}
	return time.Now().UTC()
}

func normalizeBuildRequest(req BuildRequest) BuildRequest {
	req.ScopeID = strings.TrimSpace(req.ScopeID)
	req.GenerationID = strings.TrimSpace(req.GenerationID)
	req.RepoID = strings.TrimSpace(req.RepoID)
	req.ProviderProfileID = strings.TrimSpace(req.ProviderProfileID)
	req.SourceClass = strings.TrimSpace(req.SourceClass)
	req.EmbeddingModelID = strings.TrimSpace(req.EmbeddingModelID)
	req.VectorIndexVersion = strings.TrimSpace(req.VectorIndexVersion)
	if req.Limit <= 0 || req.Limit > defaultBuildLimit {
		req.Limit = defaultBuildLimit
	}
	return req
}

func validateVector(vector []float64, dimensions int) error {
	if len(vector) != dimensions {
		return fmt.Errorf("vector length %d does not match dimensions %d", len(vector), dimensions)
	}
	for i, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("vector value at index %d is not finite", i)
		}
	}
	return nil
}
