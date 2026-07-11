// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// EshuSearchDocumentFactKind is the durable fact kind for curated search
// documents. It mirrors reducer.EshuSearchDocumentFactKind without importing the
// reducer package, keeping the storage layer free of a reducer dependency.
const EshuSearchDocumentFactKind = "reducer_eshu_search_document"

const (
	eshuSearchDocumentDefaultLimit = 100
	eshuSearchDocumentMaxLimit     = 500
)

// EshuSearchDocumentFilter bounds active search-document reads. The store
// rejects filters without a ScopeID and caps list pages.
type EshuSearchDocumentFilter struct {
	ScopeID      string
	GenerationID string
	RepoID       string
	SourceKinds  []searchdocs.SourceKind
	// Languages restricts the candidate set to documents whose Labels array
	// contains a "language:<lang>" entry for one of the requested values.
	// An empty slice means no language filter.
	Languages []string
	Limit     int
	Offset    int
}

// EshuSearchDocumentRow is one active curated search document loaded from
// fact_records for the scope's active generation or an explicitly anchored
// generation.
type EshuSearchDocumentRow struct {
	FactID       string
	ScopeID      string
	GenerationID string
	SourceSystem string
	ObservedAt   time.Time
	ContentHash  string
	Document     searchdocs.Document
}

// EshuSearchDocumentStore reads curated search documents from the shared fact
// store, scoped to each scope's active generation so superseded generations are
// excluded.
type EshuSearchDocumentStore struct {
	db ExecQueryer
}

// NewEshuSearchDocumentStore builds a search-document reader over db.
func NewEshuSearchDocumentStore(db ExecQueryer) EshuSearchDocumentStore {
	return EshuSearchDocumentStore{db: db}
}

// ListActiveDocuments returns the curated documents for the scope's active
// generation, bounded by the filter. When GenerationID is set, the query reads
// that exact generation so paged readers can stay anchored after their first
// active-generation lookup.
func (s EshuSearchDocumentStore) ListActiveDocuments(
	ctx context.Context,
	filter EshuSearchDocumentFilter,
) ([]EshuSearchDocumentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("eshu search document store database is required")
	}
	filter = normalizeEshuSearchDocumentFilter(filter)
	if strings.TrimSpace(filter.ScopeID) == "" {
		return nil, fmt.Errorf("eshu search document filter requires a scope id")
	}

	query, args := buildEshuSearchDocumentQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active eshu search documents: %w", err)
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
			return nil, fmt.Errorf("scan active eshu search document: %w", err)
		}
		if err := decodeEshuSearchDocumentPayload(payload, &row); err != nil {
			return nil, err
		}
		documents = append(documents, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active eshu search documents: %w", err)
	}
	return documents, nil
}

type eshuSearchDocumentPayload struct {
	ContentHash string              `json:"content_hash"`
	Document    searchdocs.Document `json:"document"`
}

func decodeEshuSearchDocumentPayload(payload []byte, row *EshuSearchDocumentRow) error {
	var decoded eshuSearchDocumentPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("decode eshu search document payload: %w", err)
	}
	if decoded.ContentHash != "" {
		row.ContentHash = decoded.ContentHash
	}
	row.Document = decoded.Document
	return nil
}

func buildEshuSearchDocumentQuery(filter EshuSearchDocumentFilter) (string, []any) {
	args := []any{EshuSearchDocumentFactKind}
	conditions := []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = false",
	}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	conditions = append(conditions, "fact.scope_id = "+addArg(filter.ScopeID))
	if filter.GenerationID != "" {
		conditions = append(conditions, "fact.generation_id = "+addArg(filter.GenerationID))
	}
	if filter.RepoID != "" {
		conditions = append(conditions, "fact.payload->>'repo_id' = "+addArg(filter.RepoID))
	}
	if len(filter.SourceKinds) > 0 {
		placeholders := make([]string, 0, len(filter.SourceKinds))
		for _, kind := range filter.SourceKinds {
			placeholders = append(placeholders, addArg(string(kind)))
		}
		conditions = append(conditions, "fact.payload->>'source_kind' IN ("+strings.Join(placeholders, ", ")+")")
	}
	if len(filter.Languages) > 0 {
		langs := make([]string, 0, len(filter.Languages))
		for _, lang := range filter.Languages {
			if lang != "" {
				langs = append(langs, "language:"+lang)
			}
		}
		if len(langs) > 0 {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM jsonb_array_elements_text(fact.payload->'document'->'Labels') AS lbl WHERE lbl = ANY("+addArg(langs)+"::text[]))")
		}
	}

	var builder strings.Builder
	builder.WriteString("SELECT\n    fact.fact_id,\n    fact.scope_id,\n    fact.generation_id,\n    fact.source_system,\n    fact.observed_at,\n    fact.payload\n")
	builder.WriteString("FROM fact_records AS fact\n")
	builder.WriteString("JOIN ingestion_scopes AS scope\n")
	builder.WriteString("  ON scope.scope_id = fact.scope_id\n")
	if filter.GenerationID == "" {
		builder.WriteString(" AND scope.active_generation_id = fact.generation_id\n")
	}
	builder.WriteString("WHERE ")
	builder.WriteString(strings.Join(conditions, "\n  AND "))
	builder.WriteString("\n")
	limit := addArg(filter.Limit)
	offset := addArg(filter.Offset)
	builder.WriteString("ORDER BY fact.observed_at DESC, fact.fact_id ASC\n")
	_, _ = fmt.Fprintf(&builder, "LIMIT %s OFFSET %s\n", limit, offset)
	return builder.String(), args
}

func normalizeEshuSearchDocumentFilter(filter EshuSearchDocumentFilter) EshuSearchDocumentFilter {
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.GenerationID = strings.TrimSpace(filter.GenerationID)
	filter.RepoID = strings.TrimSpace(filter.RepoID)
	if filter.Limit <= 0 {
		filter.Limit = eshuSearchDocumentDefaultLimit
	}
	if filter.Limit > eshuSearchDocumentMaxLimit {
		filter.Limit = eshuSearchDocumentMaxLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}
