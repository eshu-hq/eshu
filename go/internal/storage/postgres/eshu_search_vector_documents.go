// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	builder.WriteString("SELECT\n    doc.fact_id,\n    doc.scope_id,\n    doc.generation_id,\n    scope.source_system,\n    doc.updated_at,\n    jsonb_build_object('document', doc.document)\n")
	builder.WriteString("FROM eshu_search_index_documents AS doc\n")
	builder.WriteString("JOIN ingestion_scopes AS scope\n")
	builder.WriteString("  ON scope.scope_id = doc.scope_id\n")
	if filter.GenerationID == "" {
		builder.WriteString(" AND scope.active_generation_id = doc.generation_id\n")
	}
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
