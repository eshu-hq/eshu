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
// retires any document in the same generation that is no longer present. All
// fact inserts for the scope are issued as a single bulk unnest statement so
// whale repositories (100K+ entities) do not monopolise reducer workers with
// O(N) serial round-trips. Upserts are idempotent by fact_id, so a retry of
// the same generation converges.
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

	// Accumulate all fact-row columns into parallel slices for the single
	// unnest-based bulk insert below. Keeping the payload column as a []string
	// of JSON text lets Postgres cast via ::jsonb in the query.
	factIDs := make([]string, 0, len(write.Documents))
	scopeIDs := make([]string, 0, len(write.Documents))
	generationIDs := make([]string, 0, len(write.Documents))
	factKinds := make([]string, 0, len(write.Documents))
	stableKeys := make([]string, 0, len(write.Documents))
	collectorKinds := make([]string, 0, len(write.Documents))
	sourceConfidences := make([]string, 0, len(write.Documents))
	sourceSystems := make([]string, 0, len(write.Documents))
	sourceFactKeys := make([]string, 0, len(write.Documents))
	sourceURIs := make([]*string, 0, len(write.Documents))
	sourceRecordIDs := make([]*string, 0, len(write.Documents))
	observedAts := make([]time.Time, 0, len(write.Documents))
	ingestedAts := make([]time.Time, 0, len(write.Documents))
	isTombstones := make([]bool, 0, len(write.Documents))
	payloads := make([]string, 0, len(write.Documents))

	collectorKind := reducerFactCollectorKind(write.SourceSystem)
	sourceSystem := strings.TrimSpace(write.SourceSystem)
	intentID := strings.TrimSpace(write.IntentID)

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

		factIDs = append(factIDs, factID)
		scopeIDs = append(scopeIDs, scopeID)
		generationIDs = append(generationIDs, generationID)
		factKinds = append(factKinds, EshuSearchDocumentFactKind)
		stableKeys = append(stableKeys, eshuSearchDocumentStableFactKey(scopeID, generationID, documentID))
		collectorKinds = append(collectorKinds, collectorKind)
		sourceConfidences = append(sourceConfidences, string(facts.SourceConfidenceInferred))
		sourceSystems = append(sourceSystems, sourceSystem)
		sourceFactKeys = append(sourceFactKeys, intentID)
		sourceURIs = append(sourceURIs, nil)
		sourceRecordIDs = append(sourceRecordIDs, nil)
		observedAts = append(observedAts, now)
		ingestedAts = append(ingestedAts, now)
		isTombstones = append(isTombstones, false)
		payloads = append(payloads, string(payloadJSON))

		writtenIDs = append(writtenIDs, factID)
		indexRows = append(indexRows, newEshuSearchIndexDocumentWrite(scopeID, generationID, factID, doc))
	}

	// Single bulk fact insert for all documents in this scope. When there are
	// no documents the retirement query below still runs to clear stale facts.
	if len(factIDs) > 0 {
		if _, err := w.DB.ExecContext(
			ctx,
			eshuSearchDocumentBatchFactInsertQuery,
			factIDs,
			scopeIDs,
			generationIDs,
			factKinds,
			stableKeys,
			collectorKinds,
			sourceConfidences,
			sourceSystems,
			sourceFactKeys,
			sourceURIs,
			sourceRecordIDs,
			observedAts,
			ingestedAts,
			isTombstones,
			payloads,
		); err != nil {
			return EshuSearchDocumentWriteResult{}, fmt.Errorf("write eshu search document facts: %w", err)
		}
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

// writeEshuSearchIndexDocuments persists all index-document and term rows for
// the scope in O(1) bulk statements. The previous per-document loop issued
// 3×N round-trips (doc upsert, term delete, term upsert per doc); this
// implementation issues exactly six statements regardless of document count:
//
//  1. Retire stale terms (documents absent from the written set).
//  2. Retire stale index documents (absent from the written set).
//  3. Bulk upsert all index documents in one unnest call.
//  4. Refresh current document terms (delete-then-insert replaces stale terms
//     for documents that are being re-written; single ANY-array delete).
//  5. Bulk upsert all terms in one unnest call.
//  6. Upsert stats (always one call).
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

	// 1. Retire stale terms (docs absent from this write).
	retireTermsResult, err := w.DB.ExecContext(ctx, eshuSearchIndexRetireTermsQuery, scopeID, generationID, documentIDs)
	if err != nil {
		return finish(fmt.Errorf("retire stale eshu search index terms: %w", err), "term_retire")
	}
	if affected := rowsAffected(retireTermsResult); affected > 0 {
		stats.TermRetires += affected
		w.recordSearchIndexMutation(ctx, "term", "retire", affected)
	}

	// 2. Retire stale index documents (docs absent from this write).
	retireDocumentsResult, err := w.DB.ExecContext(ctx, eshuSearchIndexRetireDocumentsQuery, scopeID, generationID, documentIDs)
	if err != nil {
		return finish(fmt.Errorf("retire stale eshu search index documents: %w", err), "document_retire")
	}
	if affected := rowsAffected(retireDocumentsResult); affected > 0 {
		stats.DocumentRetires += affected
		w.recordSearchIndexMutation(ctx, "document", "retire", affected)
	}

	if len(documents) == 0 {
		// Nothing to write; still emit stats so the scope is marked done.
		if _, err := w.DB.ExecContext(ctx, eshuSearchIndexStatsUpsertQuery, scopeID, generationID, 0, 0.0, now); err != nil {
			return finish(fmt.Errorf("write eshu search index stats: %w", err), "stats_upsert")
		}
		return finish(nil, "")
	}

	// 3. Bulk upsert all index-document rows in one round-trip.
	docScopeIDs := make([]string, len(documents))
	docGenerationIDs := make([]string, len(documents))
	docDocumentIDs := make([]string, len(documents))
	docFactIDs := make([]string, len(documents))
	docRepoIDs := make([]string, len(documents))
	docSourceKinds := make([]string, len(documents))
	docPayloads := make([]string, len(documents))
	docLengths := make([]int, len(documents))
	docUpdatedAts := make([]time.Time, len(documents))

	for i, doc := range documents {
		payload, err := json.Marshal(doc.Document)
		if err != nil {
			return finish(fmt.Errorf("marshal eshu search index document: %w", err), "document_marshal")
		}
		docScopeIDs[i] = scopeID
		docGenerationIDs[i] = generationID
		docDocumentIDs[i] = doc.DocumentID
		docFactIDs[i] = doc.FactID
		docRepoIDs[i] = doc.RepoID
		docSourceKinds[i] = doc.SourceKind
		docPayloads[i] = string(payload)
		docLengths[i] = doc.Length
		docUpdatedAts[i] = now
	}

	if _, err := w.DB.ExecContext(
		ctx,
		eshuSearchIndexBatchDocumentUpsertQuery,
		docScopeIDs,
		docGenerationIDs,
		docDocumentIDs,
		docFactIDs,
		docRepoIDs,
		docSourceKinds,
		docPayloads,
		docLengths,
		docUpdatedAts,
	); err != nil {
		return finish(fmt.Errorf("write eshu search index documents: %w", err), "document_upsert")
	}
	stats.DocumentUpserts = int64(len(documents))
	w.recordSearchIndexMutation(ctx, "document", "upsert", int64(len(documents)))

	// 4. Delete current terms for all documents being re-written (single ANY call).
	deleteTermResult, err := w.DB.ExecContext(
		ctx,
		eshuSearchIndexRefreshDocumentTermsQuery,
		scopeID,
		generationID,
		documentIDs,
	)
	if err != nil {
		return finish(fmt.Errorf("refresh eshu search index terms: %w", err), "term_refresh")
	}
	if affected := rowsAffected(deleteTermResult); affected > 0 {
		stats.TermRetires += affected
		w.recordSearchIndexMutation(ctx, "term", "retire", affected)
	}

	// 5. Bulk upsert all terms across all documents in one round-trip.
	// Build parallel per-term arrays; terms are sorted per document for
	// deterministic ordering (matches the previous per-doc sort guarantee).
	// Arg layout mirrors eshuSearchIndexBatchTermUpsertQuery:
	//   $3=document_ids, $4=terms, $5=term_keys, $6=term_frequencies.
	var (
		termDocumentIDs []string
		termValues      []string
		termKeys        []string
		termFrequencies []int
	)
	for _, doc := range documents {
		if len(doc.Terms) == 0 {
			continue
		}
		sortedTerms, sortedTermKeys, sortedFrequencies := sortedSearchIndexTerms(doc.Terms)
		for j, term := range sortedTerms {
			termDocumentIDs = append(termDocumentIDs, doc.DocumentID)
			termValues = append(termValues, term)
			termKeys = append(termKeys, sortedTermKeys[j])
			termFrequencies = append(termFrequencies, sortedFrequencies[j])
		}
	}

	if len(termValues) > 0 {
		if _, err := w.DB.ExecContext(
			ctx,
			eshuSearchIndexBatchTermUpsertQuery,
			scopeID,
			generationID,
			termDocumentIDs,
			termValues,
			termKeys,
			termFrequencies,
		); err != nil {
			return finish(fmt.Errorf("write eshu search index terms: %w", err), "term_upsert")
		}
		stats.TermUpserts = int64(len(termValues))
		w.recordSearchIndexMutation(ctx, "term", "upsert", int64(len(termValues)))
	}

	// 6. Upsert stats — written AFTER documents so the scope becomes "done"
	// in the sweeper only when the full write has succeeded.
	averageLength := float64(totalLength) / float64(len(documents))
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
