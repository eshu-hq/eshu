package reducer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// eshuSearchDocumentRetireQuery removes search-document facts in one generation
// that are not in the freshly written set, so a source row dropped within a
// generation retires its document. An empty written set matches the empty array
// and retires every document for the generation.
const eshuSearchDocumentRetireQuery = `
DELETE FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND fact_id <> ALL($4::text[])
`

const eshuSearchIndexRetireTermsQuery = `
DELETE FROM eshu_search_index_terms
WHERE scope_id = $1
  AND generation_id = $2
  AND document_id <> ALL($3::text[])
`

const eshuSearchIndexRetireDocumentsQuery = `
DELETE FROM eshu_search_index_documents
WHERE scope_id = $1
  AND generation_id = $2
  AND document_id <> ALL($3::text[])
`

const eshuSearchIndexDocumentUpsertQuery = `
INSERT INTO eshu_search_index_documents (
    scope_id,
    generation_id,
    document_id,
    fact_id,
    repo_id,
    source_kind,
    document,
    document_length,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (scope_id, generation_id, document_id) DO UPDATE SET
    fact_id = EXCLUDED.fact_id,
    repo_id = EXCLUDED.repo_id,
    source_kind = EXCLUDED.source_kind,
    document = EXCLUDED.document,
    document_length = EXCLUDED.document_length,
    updated_at = EXCLUDED.updated_at
`

const eshuSearchIndexDeleteDocumentTermsQuery = `
DELETE FROM eshu_search_index_terms
WHERE scope_id = $1
  AND generation_id = $2
  AND document_id = $3
`

const eshuSearchIndexTermUpsertQuery = `
INSERT INTO eshu_search_index_terms (
    scope_id,
    generation_id,
    document_id,
    term_key,
    term,
    term_frequency
)
SELECT $1, $2, $3, term_key, term, term_frequency
FROM unnest($4::text[], $5::text[], $6::int[]) AS terms(term, term_key, term_frequency)
ON CONFLICT (scope_id, generation_id, term_key, document_id) DO UPDATE SET
    term = EXCLUDED.term,
    term_frequency = EXCLUDED.term_frequency
`

const eshuSearchIndexStatsUpsertQuery = `
INSERT INTO eshu_search_index_stats (
    scope_id,
    generation_id,
    document_count,
    average_document_length,
    updated_at
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (scope_id, generation_id) DO UPDATE SET
    document_count = EXCLUDED.document_count,
    average_document_length = EXCLUDED.average_document_length,
    updated_at = EXCLUDED.updated_at
`

type eshuSearchDocumentExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// EshuSearchDocumentWrite is the complete curated document set for one scope and
// generation. The writer treats it as authoritative: documents are upserted and
// any prior document for the generation that is absent is retired.
type EshuSearchDocumentWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Documents    []searchdocs.Document
}

// EshuSearchDocumentWriteResult reports how many documents were written and how
// many stale documents were retired.
type EshuSearchDocumentWriteResult struct {
	CanonicalWrites int
	Retired         int
}

// PostgresEshuSearchDocumentWriter persists curated search documents into the
// shared fact store as derived, generation-scoped records.
type PostgresEshuSearchDocumentWriter struct {
	DB          eshuSearchDocumentExecer
	Now         func() time.Time
	Instruments *telemetry.Instruments
	Tracer      trace.Tracer
}

// WriteEshuSearchDocuments upserts each curated document as a derived fact and
// retires any document in the same generation that is no longer present. Upserts
// are idempotent by fact_id, so a retry of the same generation converges.
func (w PostgresEshuSearchDocumentWriter) WriteEshuSearchDocuments(
	ctx context.Context,
	write EshuSearchDocumentWrite,
) (EshuSearchDocumentWriteResult, error) {
	if w.DB == nil {
		return EshuSearchDocumentWriteResult{}, fmt.Errorf("eshu search document database is required")
	}
	scopeID := strings.TrimSpace(write.ScopeID)
	generationID := strings.TrimSpace(write.GenerationID)
	if scopeID == "" || generationID == "" {
		return EshuSearchDocumentWriteResult{}, fmt.Errorf("eshu search document write requires scope and generation")
	}

	now := reducerWriterNow(w.Now)
	writtenIDs := make([]string, 0, len(write.Documents))
	indexRows := make([]eshuSearchIndexDocumentWrite, 0, len(write.Documents))
	for _, doc := range write.Documents {
		documentID := strings.TrimSpace(doc.ID)
		if documentID == "" {
			return EshuSearchDocumentWriteResult{}, fmt.Errorf("eshu search document requires a document id")
		}
		factID := eshuSearchDocumentFactID(scopeID, generationID, documentID)
		payloadJSON, err := json.Marshal(eshuSearchDocumentPayload(write, doc, factID))
		if err != nil {
			return EshuSearchDocumentWriteResult{}, fmt.Errorf("marshal eshu search document payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			factID,
			scopeID,
			generationID,
			EshuSearchDocumentFactKind,
			eshuSearchDocumentStableFactKey(scopeID, generationID, documentID),
			reducerFactCollectorKind(write.SourceSystem),
			facts.SourceConfidenceInferred,
			strings.TrimSpace(write.SourceSystem),
			strings.TrimSpace(write.IntentID),
			nil,
			nil,
			now,
			now,
			false,
			payloadJSON,
		); err != nil {
			return EshuSearchDocumentWriteResult{}, fmt.Errorf("write eshu search document fact: %w", err)
		}
		writtenIDs = append(writtenIDs, factID)
		indexRows = append(indexRows, newEshuSearchIndexDocumentWrite(scopeID, generationID, factID, doc))
	}

	retireResult, err := w.DB.ExecContext(
		ctx,
		eshuSearchDocumentRetireQuery,
		EshuSearchDocumentFactKind,
		scopeID,
		generationID,
		writtenIDs,
	)
	if err != nil {
		return EshuSearchDocumentWriteResult{}, fmt.Errorf("retire stale eshu search documents: %w", err)
	}
	retired := 0
	if retireResult != nil {
		if affected, affErr := retireResult.RowsAffected(); affErr == nil && affected > 0 {
			retired = int(affected)
		}
	}
	if _, err := w.writeEshuSearchIndexDocuments(ctx, scopeID, generationID, indexRows, now); err != nil {
		return EshuSearchDocumentWriteResult{}, err
	}

	return EshuSearchDocumentWriteResult{CanonicalWrites: len(writtenIDs), Retired: retired}, nil
}

type eshuSearchIndexDocumentWrite struct {
	DocumentID string
	FactID     string
	RepoID     string
	SourceKind string
	Document   searchdocs.Document
	Terms      map[string]int
	Length     int
}

func newEshuSearchIndexDocumentWrite(
	scopeID string,
	generationID string,
	factID string,
	doc searchdocs.Document,
) eshuSearchIndexDocumentWrite {
	terms := searchhybrid.DocumentTerms(doc)
	length := 0
	for _, count := range terms {
		length += count
	}
	return eshuSearchIndexDocumentWrite{
		DocumentID: strings.TrimSpace(doc.ID),
		FactID:     factID,
		RepoID:     strings.TrimSpace(doc.RepoID),
		SourceKind: string(doc.SourceKind),
		Document:   doc,
		Terms:      terms,
		Length:     length,
	}
}

type eshuSearchIndexWriteStats struct {
	DocumentUpserts int64
	DocumentRetires int64
	TermUpserts     int64
	TermRetires     int64
}

func (w PostgresEshuSearchDocumentWriter) writeEshuSearchIndexDocuments(
	ctx context.Context,
	scopeID string,
	generationID string,
	documents []eshuSearchIndexDocumentWrite,
	now time.Time,
) (eshuSearchIndexWriteStats, error) {
	ctx, span := w.startSearchIndexWriteSpan(ctx, scopeID, generationID, len(documents))
	if span != nil {
		defer span.End()
	}
	start := time.Now()
	stats := eshuSearchIndexWriteStats{}
	finish := func(err error, operation string) (eshuSearchIndexWriteStats, error) {
		result := "success"
		if err != nil {
			result = "error"
			w.recordSearchIndexError(ctx, operation)
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
		}
		w.recordSearchIndexWriteDuration(ctx, time.Since(start), result)
		return stats, err
	}

	documentIDs := make([]string, 0, len(documents))
	totalLength := 0
	for _, doc := range documents {
		documentIDs = append(documentIDs, doc.DocumentID)
		totalLength += doc.Length
	}
	retireTermsResult, err := w.DB.ExecContext(ctx, eshuSearchIndexRetireTermsQuery, scopeID, generationID, documentIDs)
	if err != nil {
		return finish(fmt.Errorf("retire stale eshu search index terms: %w", err), "term_retire")
	}
	if affected := rowsAffected(retireTermsResult); affected > 0 {
		stats.TermRetires += affected
		w.recordSearchIndexMutation(ctx, "term", "retire", affected)
	}
	retireDocumentsResult, err := w.DB.ExecContext(ctx, eshuSearchIndexRetireDocumentsQuery, scopeID, generationID, documentIDs)
	if err != nil {
		return finish(fmt.Errorf("retire stale eshu search index documents: %w", err), "document_retire")
	}
	if affected := rowsAffected(retireDocumentsResult); affected > 0 {
		stats.DocumentRetires += affected
		w.recordSearchIndexMutation(ctx, "document", "retire", affected)
	}
	for _, doc := range documents {
		payload, err := json.Marshal(doc.Document)
		if err != nil {
			return finish(fmt.Errorf("marshal eshu search index document: %w", err), "document_marshal")
		}
		if _, err := w.DB.ExecContext(
			ctx,
			eshuSearchIndexDocumentUpsertQuery,
			scopeID,
			generationID,
			doc.DocumentID,
			doc.FactID,
			doc.RepoID,
			doc.SourceKind,
			payload,
			doc.Length,
			now,
		); err != nil {
			return finish(fmt.Errorf("write eshu search index document: %w", err), "document_upsert")
		}
		stats.DocumentUpserts++
		w.recordSearchIndexMutation(ctx, "document", "upsert", 1)
		deleteTermResult, err := w.DB.ExecContext(
			ctx,
			eshuSearchIndexDeleteDocumentTermsQuery,
			scopeID,
			generationID,
			doc.DocumentID,
		)
		if err != nil {
			return finish(fmt.Errorf("refresh eshu search index terms: %w", err), "term_refresh")
		}
		if affected := rowsAffected(deleteTermResult); affected > 0 {
			stats.TermRetires += affected
			w.recordSearchIndexMutation(ctx, "term", "retire", affected)
		}
		terms, termKeys, frequencies := sortedSearchIndexTerms(doc.Terms)
		if len(terms) == 0 {
			continue
		}
		if _, err := w.DB.ExecContext(
			ctx,
			eshuSearchIndexTermUpsertQuery,
			scopeID,
			generationID,
			doc.DocumentID,
			terms,
			termKeys,
			frequencies,
		); err != nil {
			return finish(fmt.Errorf("write eshu search index terms: %w", err), "term_upsert")
		}
		stats.TermUpserts += int64(len(terms))
		w.recordSearchIndexMutation(ctx, "term", "upsert", int64(len(terms)))
	}
	averageLength := 0.0
	if len(documents) > 0 {
		averageLength = float64(totalLength) / float64(len(documents))
	}
	if _, err := w.DB.ExecContext(
		ctx,
		eshuSearchIndexStatsUpsertQuery,
		scopeID,
		generationID,
		len(documents),
		averageLength,
		now,
	); err != nil {
		return finish(fmt.Errorf("write eshu search index stats: %w", err), "stats_upsert")
	}
	return finish(nil, "")
}

func sortedSearchIndexTerms(terms map[string]int) ([]string, []string, []int) {
	keys := make([]string, 0, len(terms))
	for term := range terms {
		keys = append(keys, term)
	}
	sort.Strings(keys)
	termKeys := make([]string, 0, len(keys))
	frequencies := make([]int, 0, len(keys))
	for _, term := range keys {
		termKeys = append(termKeys, searchhybrid.TermKey(term))
		frequencies = append(frequencies, terms[term])
	}
	return keys, termKeys, frequencies
}

// eshuSearchDocumentFactID derives the deterministic fact id for one document.
func eshuSearchDocumentFactID(scopeID string, generationID string, documentID string) string {
	return EshuSearchDocumentFactKind + ":" + facts.StableID(
		EshuSearchDocumentFactKind,
		eshuSearchDocumentIdentity(scopeID, generationID, documentID),
	)
}

// eshuSearchDocumentStableFactKey is the human-traceable uniqueness key.
func eshuSearchDocumentStableFactKey(scopeID string, generationID string, documentID string) string {
	return strings.Join([]string{
		"eshu_search_document",
		scopeID,
		generationID,
		documentID,
	}, ":")
}

func eshuSearchDocumentIdentity(scopeID string, generationID string, documentID string) map[string]any {
	return map[string]any{
		"scope_id":      scopeID,
		"generation_id": generationID,
		"document_id":   documentID,
	}
}

// eshuSearchDocumentPayload is the JSON fact payload for one curated document.
// It records the durable identity alongside the document so a reader can both
// filter and reconstruct the document without a join to the source tables.
func eshuSearchDocumentPayload(write EshuSearchDocumentWrite, doc searchdocs.Document, factID string) map[string]any {
	return map[string]any{
		"reducer_domain": string(DomainEshuSearchDocument),
		"scope_id":       strings.TrimSpace(write.ScopeID),
		"generation_id":  strings.TrimSpace(write.GenerationID),
		"fact_id":        factID,
		"content_hash":   searchhybrid.DocumentContentHash(doc),
		"document_id":    strings.TrimSpace(doc.ID),
		"repo_id":        strings.TrimSpace(doc.RepoID),
		"source_kind":    string(doc.SourceKind),
		"document":       doc,
	}
}
