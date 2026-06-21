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
// any prior document for the generation that is absent is retired. It is the
// single-shot façade over the streaming Begin/InsertPage/Finalize primitives.
type EshuSearchDocumentWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Documents    []searchdocs.Document
}

// EshuSearchDocumentWriteBegin opens a streaming write for one scope and
// generation. The same scope/generation identity is shared by every inserted
// page and by the single authoritative retire issued at Finalize.
type EshuSearchDocumentWriteBegin struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
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
// retires any document in the same generation that is no longer present. It is
// retained for callers (and tests) that already hold the full document set in
// memory; it is implemented in terms of the bounded streaming primitives so the
// retire-by-absence semantics stay identical to the streaming path. Upserts are
// idempotent by fact_id, so a retry of the same generation converges.
func (w PostgresEshuSearchDocumentWriter) WriteEshuSearchDocuments(
	ctx context.Context,
	write EshuSearchDocumentWrite,
) (EshuSearchDocumentWriteResult, error) {
	session, err := w.BeginEshuSearchDocumentWrite(ctx, EshuSearchDocumentWriteBegin{
		IntentID:     write.IntentID,
		ScopeID:      write.ScopeID,
		GenerationID: write.GenerationID,
		SourceSystem: write.SourceSystem,
	})
	if err != nil {
		return EshuSearchDocumentWriteResult{}, err
	}
	if len(write.Documents) > 0 {
		if err := session.InsertPage(ctx, write.Documents); err != nil {
			return EshuSearchDocumentWriteResult{}, err
		}
	}
	return session.Finalize(ctx)
}

// BeginEshuSearchDocumentWrite opens a bounded streaming write. Callers insert
// curated documents one page at a time with InsertPage (insert-only, no retire)
// and then call Finalize exactly once to run the authoritative retire and stats
// with the union keep-set accumulated across pages. This bounds peak memory to
// a single page while preserving the generation-authoritative retire semantics
// of the prior single-shot writer (issue #3440).
func (w PostgresEshuSearchDocumentWriter) BeginEshuSearchDocumentWrite(
	_ context.Context,
	begin EshuSearchDocumentWriteBegin,
) (SearchDocumentWriteSession, error) {
	if w.DB == nil {
		return nil, fmt.Errorf("eshu search document database is required")
	}
	scopeID := strings.TrimSpace(begin.ScopeID)
	generationID := strings.TrimSpace(begin.GenerationID)
	if scopeID == "" || generationID == "" {
		return nil, fmt.Errorf("eshu search document write requires scope and generation")
	}
	return &eshuSearchDocumentWriteSession{
		writer:       w,
		scopeID:      scopeID,
		generationID: generationID,
		sourceSystem: strings.TrimSpace(begin.SourceSystem),
		intentID:     strings.TrimSpace(begin.IntentID),
		now:          reducerWriterNow(w.Now),
		started:      time.Now(),
		keepFactIDs:  make([]string, 0),
		keepDocIDs:   make([]string, 0),
	}, nil
}

// SearchDocumentWriteSession is a bounded streaming write for one scope and
// generation: insert curated documents page by page, then Finalize once to run
// the authoritative retire and stats. If the stream errors mid-way, the caller
// must call Cancel instead of Finalize to remove every partially-inserted page
// so the scope is not left in a half-written queryable state.
type SearchDocumentWriteSession interface {
	// InsertPage upserts one bounded page of curated documents (facts + search
	// index rows). No retire runs here so earlier pages survive later inserts.
	InsertPage(ctx context.Context, documents []searchdocs.Document) error
	// Finalize runs the single generation-authoritative retire over the union of
	// every inserted page's keys and upserts the search-index stats.
	Finalize(ctx context.Context) (EshuSearchDocumentWriteResult, error)
	// Cancel removes every document inserted by this session for the generation
	// (retire with empty keep-set) so a mid-stream error leaves no partial
	// search documents queryable. Cancel must be called instead of Finalize when
	// the stream errors after one or more InsertPage calls.
	Cancel(ctx context.Context) error
}

// eshuSearchDocumentWriteSession accumulates the cheap written-id keep-sets
// (fact ids and document ids, ~tens of bytes each) across pages while keeping
// only one page of content in memory at a time.
type eshuSearchDocumentWriteSession struct {
	writer       PostgresEshuSearchDocumentWriter
	scopeID      string
	generationID string
	sourceSystem string
	intentID     string
	now          time.Time
	started      time.Time

	keepFactIDs []string
	keepDocIDs  []string
	written     int
	totalLength int
}

// InsertPage upserts one page of curated documents and accumulates their keys.
func (s *eshuSearchDocumentWriteSession) InsertPage(ctx context.Context, documents []searchdocs.Document) error {
	if len(documents) == 0 {
		return nil
	}
	indexRows, factIDs, err := s.writer.insertSearchDocumentFacts(
		ctx, s.scopeID, s.generationID, s.sourceSystem, s.intentID, s.now, documents,
	)
	if err != nil {
		return err
	}
	pageLength, err := s.writer.insertSearchIndexPage(ctx, s.scopeID, s.generationID, indexRows, s.now)
	if err != nil {
		return err
	}
	for i := range factIDs {
		s.keepFactIDs = append(s.keepFactIDs, factIDs[i])
		s.keepDocIDs = append(s.keepDocIDs, indexRows[i].DocumentID)
	}
	s.written += len(factIDs)
	s.totalLength += pageLength
	return nil
}

// Finalize issues the single authoritative retire over the union keep-set and
// upserts the search-index stats for the whole generation.
func (s *eshuSearchDocumentWriteSession) Finalize(ctx context.Context) (EshuSearchDocumentWriteResult, error) {
	retired, err := s.writer.retireSearchDocumentFacts(ctx, s.scopeID, s.generationID, s.keepFactIDs)
	if err != nil {
		return EshuSearchDocumentWriteResult{}, err
	}
	if err := s.writer.finalizeSearchIndex(ctx, s.scopeID, s.generationID, s.keepDocIDs, s.written, s.totalLength, s.now); err != nil {
		return EshuSearchDocumentWriteResult{}, err
	}
	return EshuSearchDocumentWriteResult{CanonicalWrites: s.written, Retired: retired}, nil
}

// Cancel removes every document inserted by this session for the generation by
// running the authoritative retire with an empty keep-set. It is called instead
// of Finalize when the stream errors after one or more InsertPage calls so the
// scope is not left in a half-written queryable state. An empty keep-set retires
// all fact_records, eshu_search_index_documents, and eshu_search_index_terms
// for the (scope_id, generation_id) pair, which is the same operation performed
// by Finalize when no documents are produced.
func (s *eshuSearchDocumentWriteSession) Cancel(ctx context.Context) error {
	if _, err := s.writer.retireSearchDocumentFacts(ctx, s.scopeID, s.generationID, nil); err != nil {
		return fmt.Errorf("cancel eshu search document partial write: %w", err)
	}
	// Retire index rows with an empty keep-set (delete all for this generation).
	if _, err := s.writer.DB.ExecContext(ctx, eshuSearchIndexRetireTermsQuery, s.scopeID, s.generationID, []string{}); err != nil {
		return fmt.Errorf("cancel eshu search index terms: %w", err)
	}
	if _, err := s.writer.DB.ExecContext(ctx, eshuSearchIndexRetireDocumentsQuery, s.scopeID, s.generationID, []string{}); err != nil {
		return fmt.Errorf("cancel eshu search index documents: %w", err)
	}
	return nil
}

// insertSearchDocumentFacts bulk-upserts one page of curated documents as derived
// facts in a single unnest round-trip and returns the parallel index-write rows
// plus the fact ids that must survive retire.
func (w PostgresEshuSearchDocumentWriter) insertSearchDocumentFacts(
	ctx context.Context,
	scopeID string,
	generationID string,
	sourceSystem string,
	intentID string,
	now time.Time,
	documents []searchdocs.Document,
) ([]eshuSearchIndexDocumentWrite, []string, error) {
	count := len(documents)
	factIDs := make([]string, 0, count)
	scopeIDs := make([]string, 0, count)
	generationIDs := make([]string, 0, count)
	factKinds := make([]string, 0, count)
	stableKeys := make([]string, 0, count)
	collectorKinds := make([]string, 0, count)
	sourceConfidences := make([]string, 0, count)
	sourceSystems := make([]string, 0, count)
	sourceFactKeys := make([]string, 0, count)
	sourceURIs := make([]*string, 0, count)
	sourceRecordIDs := make([]*string, 0, count)
	observedAts := make([]time.Time, 0, count)
	ingestedAts := make([]time.Time, 0, count)
	isTombstones := make([]bool, 0, count)
	payloads := make([]string, 0, count)
	indexRows := make([]eshuSearchIndexDocumentWrite, 0, count)

	collectorKind := reducerFactCollectorKind(sourceSystem)
	writeMeta := EshuSearchDocumentWrite{ScopeID: scopeID, GenerationID: generationID, SourceSystem: sourceSystem}

	for _, doc := range documents {
		documentID := strings.TrimSpace(doc.ID)
		if documentID == "" {
			return nil, nil, fmt.Errorf("eshu search document requires a document id")
		}
		factID := eshuSearchDocumentFactID(scopeID, generationID, documentID)
		payloadJSON, err := json.Marshal(eshuSearchDocumentPayload(writeMeta, doc, factID))
		if err != nil {
			return nil, nil, fmt.Errorf("marshal eshu search document payload: %w", err)
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

		indexRows = append(indexRows, newEshuSearchIndexDocumentWrite(scopeID, generationID, factID, doc))
	}

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
		return nil, nil, fmt.Errorf("write eshu search document facts: %w", err)
	}
	return indexRows, factIDs, nil
}

// retireSearchDocumentFacts runs the single generation-authoritative fact retire
// over the union keep-set. An empty keep-set retires every document for the
// generation, which is the correct stale-clearing behavior for an empty write.
func (w PostgresEshuSearchDocumentWriter) retireSearchDocumentFacts(
	ctx context.Context,
	scopeID string,
	generationID string,
	keepFactIDs []string,
) (int, error) {
	retireResult, err := w.DB.ExecContext(
		ctx,
		eshuSearchDocumentRetireQuery,
		EshuSearchDocumentFactKind,
		scopeID,
		generationID,
		keepFactIDs,
	)
	if err != nil {
		return 0, fmt.Errorf("retire stale eshu search documents: %w", err)
	}
	retired := 0
	if retireResult != nil {
		if affected, affErr := retireResult.RowsAffected(); affErr == nil && affected > 0 {
			retired = int(affected)
		}
	}
	return retired, nil
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

// insertSearchIndexPage upserts one bounded page of index-document and term rows
// in O(1) bulk statements (doc upsert, per-doc term refresh-delete, term
// upsert). It issues NO retire-by-absence statement so earlier pages survive.
// Stale-row retirement and stats are deferred to finalizeSearchIndex. It returns
// the summed document length for the page so Finalize can compute the average.
func (w PostgresEshuSearchDocumentWriter) insertSearchIndexPage(
	ctx context.Context,
	scopeID string,
	generationID string,
	documents []eshuSearchIndexDocumentWrite,
	now time.Time,
) (int, error) {
	if len(documents) == 0 {
		return 0, nil
	}
	ctx, span := w.startSearchIndexWriteSpan(ctx, scopeID, generationID, len(documents))
	if span != nil {
		defer span.End()
	}
	start := time.Now()
	totalLength := 0
	finish := func(err error, operation string) (int, error) {
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
		return totalLength, err
	}

	documentIDs := make([]string, 0, len(documents))
	for _, doc := range documents {
		documentIDs = append(documentIDs, doc.DocumentID)
		totalLength += doc.Length
	}

	// 1. Bulk upsert all index-document rows in one round-trip.
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
	w.recordSearchIndexMutation(ctx, "document", "upsert", int64(len(documents)))

	// 2. Delete current terms for the documents being re-written (single ANY call).
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
		w.recordSearchIndexMutation(ctx, "term", "retire", affected)
	}

	// 3. Bulk upsert all terms across all documents in one round-trip.
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
		w.recordSearchIndexMutation(ctx, "term", "upsert", int64(len(termValues)))
	}
	return finish(nil, "")
}

// finalizeSearchIndex runs the single authoritative search-index retire (stale
// terms then stale documents absent from the union keep-set) and upserts the
// stats row. It mirrors the deferred retire of the fact store so the read model
// converges only after every page has been inserted. An empty keep-set retires
// all index rows for the generation, matching the empty-write contract.
func (w PostgresEshuSearchDocumentWriter) finalizeSearchIndex(
	ctx context.Context,
	scopeID string,
	generationID string,
	keepDocumentIDs []string,
	documentCount int,
	totalLength int,
	now time.Time,
) error {
	ctx, span := w.startSearchIndexWriteSpan(ctx, scopeID, generationID, len(keepDocumentIDs))
	if span != nil {
		defer span.End()
	}
	start := time.Now()
	finish := func(err error, operation string) error {
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
		return err
	}

	// 1. Retire stale terms (docs absent from the union keep-set).
	retireTermsResult, err := w.DB.ExecContext(ctx, eshuSearchIndexRetireTermsQuery, scopeID, generationID, keepDocumentIDs)
	if err != nil {
		return finish(fmt.Errorf("retire stale eshu search index terms: %w", err), "term_retire")
	}
	if affected := rowsAffected(retireTermsResult); affected > 0 {
		w.recordSearchIndexMutation(ctx, "term", "retire", affected)
	}

	// 2. Retire stale index documents (docs absent from the union keep-set).
	retireDocumentsResult, err := w.DB.ExecContext(ctx, eshuSearchIndexRetireDocumentsQuery, scopeID, generationID, keepDocumentIDs)
	if err != nil {
		return finish(fmt.Errorf("retire stale eshu search index documents: %w", err), "document_retire")
	}
	if affected := rowsAffected(retireDocumentsResult); affected > 0 {
		w.recordSearchIndexMutation(ctx, "document", "retire", affected)
	}

	// 3. Upsert stats — written last so the scope becomes "done" in the sweeper
	// only when the full streamed write has succeeded.
	averageLength := 0.0
	if documentCount > 0 {
		averageLength = float64(totalLength) / float64(documentCount)
	}
	if _, err := w.DB.ExecContext(ctx, eshuSearchIndexStatsUpsertQuery, scopeID, generationID, documentCount, averageLength, now); err != nil {
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
