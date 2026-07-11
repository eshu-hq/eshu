// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

type eshuSearchDocumentExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type eshuSearchIndexTermCopier interface {
	CopySearchIndexTerms(
		context.Context,
		string,
		string,
		[]string,
		[]string,
		[]string,
		[]int,
	) (int64, error)
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
	Timings         EshuSearchDocumentWriteTimings
}

// SearchDocumentProjectionStateWriter owns the search-document projection-state
// lifecycle: BeginBuilding before mutation, FinalizeReady after successful
// completion, and MarkFailed on cancel. Nil disables the lifecycle (keeps every
// existing test and local-profile wiring byte-identical). Define this consumer
// interface in the reducer package so the reducer never imports storage/postgres.
type SearchDocumentProjectionStateWriter interface {
	BeginBuilding(ctx context.Context, scopeID, generationID string) (revision, fence int64, err error)
	FinalizeReady(ctx context.Context, scopeID, generationID string, revision, fence, documentCount int64) (bool, error)
	MarkFailed(ctx context.Context, scopeID, generationID string, revision, fence int64) (bool, error)
}

// PostgresEshuSearchDocumentWriter persists curated search documents into the
// shared fact store as derived, generation-scoped records.
type PostgresEshuSearchDocumentWriter struct {
	DB               eshuSearchDocumentExecer
	SearchTermCopier eshuSearchIndexTermCopier
	Now              func() time.Time
	Instruments      *telemetry.Instruments
	Tracer           trace.Tracer
	// ProjectionState optionally wires the #4233 per-scope projection-state
	// lifecycle. Nil disables it, keeping every existing test and local-profile
	// wiring byte-identical.
	ProjectionState SearchDocumentProjectionStateWriter
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
			if cancelErr := session.Cancel(ctx); cancelErr != nil {
				return EshuSearchDocumentWriteResult{}, fmt.Errorf("insert eshu search documents page: %w; cancel partial write: %v", err, cancelErr)
			}
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
	ctx context.Context,
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
	// #4233: invalidate before mutate — BeginBuilding must succeed BEFORE any
	// DELETE/INSERT mutation, so a failed begin does not leave partial rows.
	var projRev, projFence int64
	if w.ProjectionState != nil {
		var err error
		projRev, projFence, err = w.ProjectionState.BeginBuilding(ctx, scopeID, generationID)
		if err != nil {
			return nil, fmt.Errorf("begin building eshu search document projection state: %w", err)
		}
	}
	return &eshuSearchDocumentWriteSession{
		writer:             w,
		scopeID:            scopeID,
		generationID:       generationID,
		sourceSystem:       strings.TrimSpace(begin.SourceSystem),
		intentID:           strings.TrimSpace(begin.IntentID),
		now:                reducerWriterNow(w.Now),
		started:            time.Now(),
		keepFactIDs:        make([]string, 0),
		keepDocIDs:         make([]string, 0),
		projectionRevision: projRev,
		projectionFence:    projFence,
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

	keepFactIDs         []string
	keepDocIDs          []string
	written             int
	totalLength         int
	timings             EshuSearchDocumentWriteTimings
	didInitialTermClear bool

	// projectionRevision / projectionFence hold the revision and fence values
	// returned by SearchDocumentProjectionStateWriter.BeginBuilding at session
	// start, for the FinalizeReady / MarkFailed CAS at session end (#4233).
	projectionRevision int64
	projectionFence    int64
}

// InsertPage upserts one page of curated documents and accumulates their keys.
func (s *eshuSearchDocumentWriteSession) InsertPage(ctx context.Context, documents []searchdocs.Document) error {
	if len(documents) == 0 {
		return nil
	}
	if err := s.clearInitialSearchIndexTerms(ctx); err != nil {
		return err
	}
	factStarted := time.Now()
	indexRows, factIDs, err := s.writer.insertSearchDocumentFacts(
		ctx, s.scopeID, s.generationID, s.sourceSystem, s.intentID, s.now, documents,
	)
	s.timings.FactUpsertDuration += time.Since(factStarted)
	if err != nil {
		return err
	}
	pageLength, pageTimings, err := s.writer.insertSearchIndexPage(ctx, s.scopeID, s.generationID, indexRows, s.now)
	s.timings.add(pageTimings)
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
	if err := s.clearInitialSearchIndexTerms(ctx); err != nil {
		return EshuSearchDocumentWriteResult{}, err
	}
	factRetireStarted := time.Now()
	retired, err := s.writer.retireSearchDocumentFacts(ctx, s.scopeID, s.generationID, s.keepFactIDs)
	s.timings.FactRetireDuration += time.Since(factRetireStarted)
	if err != nil {
		return EshuSearchDocumentWriteResult{}, err
	}
	finalizeTimings, err := s.writer.finalizeSearchIndex(ctx, s.scopeID, s.generationID, s.keepDocIDs, s.written, s.totalLength, s.now)
	s.timings.add(finalizeTimings)
	if err != nil {
		return EshuSearchDocumentWriteResult{}, err
	}
	// #4233: after the authoritative retire + stats succeed, publish the
	// projection state as ready. A false CAS result (stale/superseded) must
	// NOT fail the write — a superseding generation legitimately owns the row.
	if s.writer.ProjectionState != nil {
		documentCount := int64(len(uniqueSortedStrings(s.keepDocIDs)))
		ok, err := s.writer.ProjectionState.FinalizeReady(ctx, s.scopeID, s.generationID, s.projectionRevision, s.projectionFence, documentCount)
		if err != nil {
			return EshuSearchDocumentWriteResult{}, fmt.Errorf("finalize ready eshu search document projection state: %w", err)
		}
		if !ok {
			// Structured log key for stale CAS — no new metric instrument.
			slog.Warn("search document projection ready skipped stale",
				"scope_id", s.scopeID,
				"generation_id", s.generationID,
				"projection_revision", s.projectionRevision,
				"build_fence", s.projectionFence,
				"document_count", s.written,
			)
		}
	}
	return EshuSearchDocumentWriteResult{CanonicalWrites: s.written, Retired: retired, Timings: s.timings}, nil
}

// Cancel removes every document inserted by this session for the generation by
// running the authoritative retire with an empty keep-set. It is called instead
// of Finalize when the stream errors after one or more InsertPage calls so the
// scope is not left in a half-written queryable state. An empty keep-set retires
// all fact_records, eshu_search_index_documents, and eshu_search_index_terms
// for the (scope_id, generation_id) pair, which is the same operation performed
// by Finalize when no documents are produced.
func (s *eshuSearchDocumentWriteSession) Cancel(ctx context.Context) error {
	// #4233: best-effort MarkFailed — log on error, do not mask the original
	// cancel reason.
	if s.writer.ProjectionState != nil {
		if _, err := s.writer.ProjectionState.MarkFailed(ctx, s.scopeID, s.generationID, s.projectionRevision, s.projectionFence); err != nil {
			slog.Warn("search document projection mark failed error",
				"scope_id", s.scopeID,
				"generation_id", s.generationID,
				"error", err,
			)
		}
	}
	if _, err := s.writer.retireSearchDocumentFacts(ctx, s.scopeID, s.generationID, nil); err != nil {
		return fmt.Errorf("cancel eshu search document partial write: %w", err)
	}
	// Retire index rows with an empty keep-set (delete all for this generation).
	if err := s.clearSearchIndexTerms(ctx); err != nil {
		return fmt.Errorf("cancel eshu search index terms: %w", err)
	}
	if _, err := s.writer.DB.ExecContext(ctx, eshuSearchIndexRetireDocumentsQuery, s.scopeID, s.generationID, []string{}); err != nil {
		return fmt.Errorf("cancel eshu search index documents: %w", err)
	}
	return nil
}

func (s *eshuSearchDocumentWriteSession) clearInitialSearchIndexTerms(ctx context.Context) error {
	if s.didInitialTermClear {
		return nil
	}
	if err := s.clearSearchIndexTerms(ctx); err != nil {
		return err
	}
	s.didInitialTermClear = true
	return nil
}

func (s *eshuSearchDocumentWriteSession) clearSearchIndexTerms(ctx context.Context) error {
	started := time.Now()
	result, err := s.writer.DB.ExecContext(ctx, eshuSearchIndexClearGenerationTermsQuery, s.scopeID, s.generationID)
	if err != nil {
		s.timings.IndexTermRefreshDuration += time.Since(started)
		s.writer.recordSearchIndexWriteDuration(ctx, "term_refresh", s.timings.IndexTermRefreshDuration, "error")
		return fmt.Errorf("clear eshu search index terms: %w", err)
	}
	s.timings.IndexTermRefreshDuration += time.Since(started)
	s.writer.recordSearchIndexWriteDuration(ctx, "term_refresh", s.timings.IndexTermRefreshDuration, "success")
	if affected := rowsAffected(result); affected > 0 {
		s.writer.recordSearchIndexMutation(ctx, "term", "retire", affected)
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
