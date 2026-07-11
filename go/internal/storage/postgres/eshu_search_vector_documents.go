// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// EshuSearchVectorDocumentFilter bounds active search-document reads to
// documents whose vector sidecar row is missing or stale for one embedding
// tuple.
type EshuSearchVectorDocumentFilter struct {
	EshuSearchDocumentFilter
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
}

// EshuSearchVectorDocumentScope identifies one selected active scope for a
// batched pending-vector document read.
type EshuSearchVectorDocumentScope struct {
	ScopeID      string
	GenerationID string
	RepoID       string
}

// EshuSearchVectorDocumentBatchFilter bounds pending-vector document reads
// across several selected active scopes.
type EshuSearchVectorDocumentBatchFilter struct {
	Scopes             []EshuSearchVectorDocumentScope
	SourceKinds        []searchdocs.SourceKind
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	Limit              int
}

const eshuSearchVectorDocumentBatchMaxLimit = 10000

// ListPendingVectorDocuments returns one bounded page of active search
// documents that still need vector rows for the requested embedding tuple.
func (s EshuSearchDocumentStore) ListPendingVectorDocuments(
	ctx context.Context,
	filter EshuSearchVectorDocumentFilter,
) ([]EshuSearchDocumentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("eshu search document store database is required")
	}
	filter = normalizeEshuSearchVectorDocumentFilter(filter)
	if err := validateEshuSearchVectorDocumentFilter(filter); err != nil {
		return nil, err
	}

	query, args := buildEshuSearchVectorDocumentQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list pending eshu search vector documents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var documents []EshuSearchDocumentRow
	for rows.Next() {
		var row EshuSearchDocumentRow
		var payload []byte
		if err := rows.Scan(
			&row.FactID,
			&row.ScopeID,
			&row.GenerationID,
			&row.SourceSystem,
			&row.ObservedAt,
			&row.ContentHash,
			&payload,
		); err != nil {
			return nil, fmt.Errorf("scan pending eshu search vector document: %w", err)
		}
		if err := decodeEshuSearchDocumentPayload(payload, &row); err != nil {
			return nil, err
		}
		documents = append(documents, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending eshu search vector documents: %w", err)
	}
	return documents, nil
}

// ListPendingVectorDocumentsForScopes returns bounded pending-document pages
// for several selected active scopes using one Postgres query.
func (s EshuSearchDocumentStore) ListPendingVectorDocumentsForScopes(
	ctx context.Context,
	filter EshuSearchVectorDocumentBatchFilter,
) ([]EshuSearchDocumentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("eshu search document store database is required")
	}
	filter = normalizeEshuSearchVectorDocumentBatchFilter(filter)
	if err := validateEshuSearchVectorDocumentBatchFilter(filter); err != nil {
		return nil, err
	}

	query, args := buildEshuSearchVectorDocumentBatchQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list batched pending eshu search vector documents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var documents []EshuSearchDocumentRow
	for rows.Next() {
		var row EshuSearchDocumentRow
		var payload []byte
		if err := rows.Scan(
			&row.FactID,
			&row.ScopeID,
			&row.GenerationID,
			&row.SourceSystem,
			&row.ObservedAt,
			&row.ContentHash,
			&payload,
		); err != nil {
			return nil, fmt.Errorf("scan batched pending eshu search vector document: %w", err)
		}
		if err := decodeEshuSearchDocumentPayload(payload, &row); err != nil {
			return nil, err
		}
		documents = append(documents, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batched pending eshu search vector documents: %w", err)
	}
	return documents, nil
}

func buildEshuSearchVectorDocumentQuery(filter EshuSearchVectorDocumentFilter) (string, []any) {
	args := []any{}
	conditions := []string{}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	conditions = append(conditions, "doc.scope_id = "+addArg(filter.ScopeID))
	if filter.GenerationID != "" {
		conditions = append(conditions, "doc.generation_id = "+addArg(filter.GenerationID))
	}
	if filter.RepoID != "" {
		conditions = append(conditions, "doc.repo_id = "+addArg(filter.RepoID))
	}
	if len(filter.SourceKinds) > 0 {
		placeholders := make([]string, 0, len(filter.SourceKinds))
		for _, kind := range filter.SourceKinds {
			placeholders = append(placeholders, addArg(string(kind)))
		}
		conditions = append(conditions, "doc.source_kind IN ("+strings.Join(placeholders, ", ")+")")
	}
	if len(filter.Languages) > 0 {
		langs := make([]string, 0, len(filter.Languages))
		for _, lang := range filter.Languages {
			if lang != "" {
				langs = append(langs, "language:"+lang)
			}
		}
		if len(langs) > 0 {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM jsonb_array_elements_text(doc.document->'Labels') AS lbl WHERE lbl = ANY("+addArg(langs)+"::text[]))")
		}
	}

	providerArg := addArg(filter.ProviderProfileID)
	sourceClassArg := addArg(filter.SourceClass)
	modelArg := addArg(filter.EmbeddingModelID)
	versionArg := addArg(filter.VectorIndexVersion)

	var builder strings.Builder
	builder.WriteString("SELECT\n    doc.fact_id,\n    doc.scope_id,\n    doc.generation_id,\n    scope.source_system,\n    doc.updated_at,\n    doc.content_hash,\n    jsonb_build_object('document', doc.document)\n")
	builder.WriteString("FROM eshu_search_index_documents AS doc\n")
	builder.WriteString("JOIN ingestion_scopes AS scope\n")
	builder.WriteString("  ON scope.scope_id = doc.scope_id\n")
	builder.WriteString(" AND scope.active_generation_id = doc.generation_id\n")
	builder.WriteString("WHERE ")
	builder.WriteString(strings.Join(conditions, "\n  AND "))
	builder.WriteString("\n  AND scope.scope_kind = 'repository'\n")
	builder.WriteString("  AND NOT EXISTS (\n")
	builder.WriteString("    SELECT 1\n")
	builder.WriteString("    FROM eshu_search_vector_metadata meta\n")
	builder.WriteString("    LEFT JOIN eshu_search_vector_values value\n")
	builder.WriteString("      ON value.scope_id = meta.scope_id AND value.generation_id = meta.generation_id\n")
	builder.WriteString("     AND value.document_id = meta.document_id AND value.provider_profile_id = meta.provider_profile_id\n")
	builder.WriteString("     AND value.source_class = meta.source_class AND value.embedding_model_id = meta.embedding_model_id\n")
	builder.WriteString("     AND value.vector_index_version = meta.vector_index_version\n")
	builder.WriteString("     AND value.embedding_content_hash = meta.embedding_content_hash\n")
	builder.WriteString("    WHERE meta.scope_id = doc.scope_id\n")
	builder.WriteString("      AND meta.generation_id = doc.generation_id\n")
	builder.WriteString("      AND meta.document_id = doc.document_id\n")
	builder.WriteString("      AND meta.provider_profile_id = " + providerArg + "\n")
	builder.WriteString("      AND meta.source_class = " + sourceClassArg + "\n")
	builder.WriteString("      AND meta.embedding_model_id = " + modelArg + "\n")
	builder.WriteString("      AND meta.vector_index_version = " + versionArg + "\n")
	builder.WriteString("      AND meta.embedding_content_hash = doc.content_hash\n")
	builder.WriteString("      AND (meta.build_state = 'disabled' OR (meta.build_state = 'ready' AND value.document_id IS NOT NULL))\n")
	builder.WriteString("  )\n")
	limit := addArg(filter.Limit)
	_, _ = fmt.Fprintf(&builder, "LIMIT %s\n", limit)
	return builder.String(), args
}

func buildEshuSearchVectorDocumentBatchQuery(filter EshuSearchVectorDocumentBatchFilter) (string, []any) {
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	scopeRows := make([]string, 0, len(filter.Scopes))
	for _, scope := range filter.Scopes {
		scopeRows = append(scopeRows, fmt.Sprintf("(%s, %s, %s)", addArg(scope.ScopeID), addArg(scope.GenerationID), addArg(scope.RepoID)))
	}

	providerArg := addArg(filter.ProviderProfileID)
	sourceClassArg := addArg(filter.SourceClass)
	modelArg := addArg(filter.EmbeddingModelID)
	versionArg := addArg(filter.VectorIndexVersion)
	limitArg := addArg(filter.Limit)

	sourceKindCondition := ""
	if len(filter.SourceKinds) > 0 {
		placeholders := make([]string, 0, len(filter.SourceKinds))
		for _, kind := range filter.SourceKinds {
			placeholders = append(placeholders, addArg(string(kind)))
		}
		sourceKindCondition = "\n      AND doc.source_kind IN (" + strings.Join(placeholders, ", ") + ")"
	}

	var builder strings.Builder
	builder.WriteString("WITH selected(scope_id, generation_id, repo_id) AS (VALUES ")
	builder.WriteString(strings.Join(scopeRows, ", "))
	builder.WriteString(")\n")
	builder.WriteString("SELECT pending.fact_id, pending.scope_id, pending.generation_id, pending.source_system, pending.updated_at, pending.content_hash, pending.payload\n")
	builder.WriteString("FROM selected\n")
	builder.WriteString("JOIN LATERAL (\n")
	builder.WriteString("  SELECT\n")
	builder.WriteString("      doc.fact_id,\n")
	builder.WriteString("      doc.scope_id,\n")
	builder.WriteString("      doc.generation_id,\n")
	builder.WriteString("      scope.source_system,\n")
	builder.WriteString("      doc.updated_at,\n")
	builder.WriteString("      doc.content_hash,\n")
	builder.WriteString("      jsonb_build_object('document', doc.document) AS payload\n")
	builder.WriteString("  FROM eshu_search_index_documents AS doc\n")
	builder.WriteString("  JOIN ingestion_scopes AS scope\n")
	builder.WriteString("    ON scope.scope_id = doc.scope_id\n")
	builder.WriteString("   AND scope.active_generation_id = doc.generation_id\n")
	builder.WriteString("  WHERE doc.scope_id = selected.scope_id\n")
	builder.WriteString("    AND doc.generation_id = selected.generation_id\n")
	builder.WriteString("    AND (selected.repo_id = '' OR doc.repo_id = selected.repo_id)\n")
	builder.WriteString("    AND scope.scope_kind = 'repository'")
	builder.WriteString(sourceKindCondition)
	builder.WriteString("\n    AND NOT EXISTS (\n")
	builder.WriteString("      SELECT 1\n")
	builder.WriteString("      FROM eshu_search_vector_metadata meta\n")
	builder.WriteString("      LEFT JOIN eshu_search_vector_values value\n")
	builder.WriteString("        ON value.scope_id = meta.scope_id AND value.generation_id = meta.generation_id\n")
	builder.WriteString("       AND value.document_id = meta.document_id AND value.provider_profile_id = meta.provider_profile_id\n")
	builder.WriteString("       AND value.source_class = meta.source_class AND value.embedding_model_id = meta.embedding_model_id\n")
	builder.WriteString("       AND value.vector_index_version = meta.vector_index_version\n")
	builder.WriteString("       AND value.embedding_content_hash = meta.embedding_content_hash\n")
	builder.WriteString("      WHERE meta.scope_id = doc.scope_id\n")
	builder.WriteString("        AND meta.generation_id = doc.generation_id\n")
	builder.WriteString("        AND meta.document_id = doc.document_id\n")
	builder.WriteString("        AND meta.provider_profile_id = " + providerArg + "\n")
	builder.WriteString("        AND meta.source_class = " + sourceClassArg + "\n")
	builder.WriteString("        AND meta.embedding_model_id = " + modelArg + "\n")
	builder.WriteString("        AND meta.vector_index_version = " + versionArg + "\n")
	builder.WriteString("        AND meta.embedding_content_hash = doc.content_hash\n")
	builder.WriteString("        AND (meta.build_state = 'disabled' OR (meta.build_state = 'ready' AND value.document_id IS NOT NULL))\n")
	builder.WriteString("    )\n")
	builder.WriteString("  LIMIT " + limitArg + "\n")
	builder.WriteString(") pending ON TRUE\n")
	return builder.String(), args
}

func normalizeEshuSearchVectorDocumentFilter(filter EshuSearchVectorDocumentFilter) EshuSearchVectorDocumentFilter {
	filter.EshuSearchDocumentFilter = normalizeEshuSearchDocumentFilter(filter.EshuSearchDocumentFilter)
	filter.ProviderProfileID = strings.TrimSpace(filter.ProviderProfileID)
	filter.SourceClass = strings.TrimSpace(filter.SourceClass)
	filter.EmbeddingModelID = strings.TrimSpace(filter.EmbeddingModelID)
	filter.VectorIndexVersion = strings.TrimSpace(filter.VectorIndexVersion)
	filter.Offset = 0
	return filter
}

func validateEshuSearchVectorDocumentFilter(filter EshuSearchVectorDocumentFilter) error {
	var problems []error
	if strings.TrimSpace(filter.ScopeID) == "" {
		problems = append(problems, errors.New("eshu search vector document filter requires scope id"))
	}
	if filter.ProviderProfileID == "" {
		problems = append(problems, errors.New("eshu search vector document filter requires provider profile id"))
	}
	if filter.SourceClass == "" {
		problems = append(problems, errors.New("eshu search vector document filter requires source class"))
	}
	if filter.EmbeddingModelID == "" {
		problems = append(problems, errors.New("eshu search vector document filter requires embedding model id"))
	}
	if filter.VectorIndexVersion == "" {
		problems = append(problems, errors.New("eshu search vector document filter requires vector index version"))
	}
	return errors.Join(problems...)
}

func normalizeEshuSearchVectorDocumentBatchFilter(filter EshuSearchVectorDocumentBatchFilter) EshuSearchVectorDocumentBatchFilter {
	normalized := filter
	normalized.ProviderProfileID = strings.TrimSpace(filter.ProviderProfileID)
	normalized.SourceClass = strings.TrimSpace(filter.SourceClass)
	normalized.EmbeddingModelID = strings.TrimSpace(filter.EmbeddingModelID)
	normalized.VectorIndexVersion = strings.TrimSpace(filter.VectorIndexVersion)
	if normalized.Limit <= 0 {
		normalized.Limit = eshuSearchDocumentMaxLimit
	} else if normalized.Limit > eshuSearchVectorDocumentBatchMaxLimit {
		normalized.Limit = eshuSearchVectorDocumentBatchMaxLimit
	}
	normalized.Scopes = make([]EshuSearchVectorDocumentScope, 0, len(filter.Scopes))
	for _, scope := range filter.Scopes {
		scope.ScopeID = strings.TrimSpace(scope.ScopeID)
		scope.GenerationID = strings.TrimSpace(scope.GenerationID)
		scope.RepoID = strings.TrimSpace(scope.RepoID)
		normalized.Scopes = append(normalized.Scopes, scope)
	}
	return normalized
}

func validateEshuSearchVectorDocumentBatchFilter(filter EshuSearchVectorDocumentBatchFilter) error {
	var problems []error
	if len(filter.Scopes) == 0 {
		problems = append(problems, errors.New("eshu search vector document batch filter requires scopes"))
	}
	if len(filter.Scopes) > eshuSearchVectorPendingMaxLimit {
		problems = append(problems, fmt.Errorf("eshu search vector document batch filter allows at most %d scopes", eshuSearchVectorPendingMaxLimit))
	}
	for i, scope := range filter.Scopes {
		if scope.ScopeID == "" {
			problems = append(problems, fmt.Errorf("eshu search vector document batch filter scope %d requires scope id", i))
		}
		if scope.GenerationID == "" {
			problems = append(problems, fmt.Errorf("eshu search vector document batch filter scope %d requires generation id", i))
		}
	}
	if filter.ProviderProfileID == "" {
		problems = append(problems, errors.New("eshu search vector document batch filter requires provider profile id"))
	}
	if filter.SourceClass == "" {
		problems = append(problems, errors.New("eshu search vector document batch filter requires source class"))
	}
	if filter.EmbeddingModelID == "" {
		problems = append(problems, errors.New("eshu search vector document batch filter requires embedding model id"))
	}
	if filter.VectorIndexVersion == "" {
		problems = append(problems, errors.New("eshu search vector document batch filter requires vector index version"))
	}
	return errors.Join(problems...)
}
