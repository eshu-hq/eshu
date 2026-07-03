// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"go.opentelemetry.io/otel/codes"
)

type eshuSearchIndexDocumentWrite struct {
	DocumentID  string
	FactID      string
	RepoID      string
	SourceKind  string
	ContentHash string
	Document    searchdocs.Document
	Terms       map[string]int
	Length      int
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
		DocumentID:  strings.TrimSpace(doc.ID),
		FactID:      factID,
		RepoID:      strings.TrimSpace(doc.RepoID),
		SourceKind:  string(doc.SourceKind),
		ContentHash: searchhybrid.DocumentContentHash(doc),
		Document:    doc,
		Terms:       terms,
		Length:      length,
	}
}

// insertSearchIndexPage upserts one bounded page of index-document and term rows
// in O(1) bulk statements (document upsert, term upsert). The owning session
// clears generation term rows once before the first page so page writes do not
// depend on a document-keyed term refresh delete. Stale document retirement and
// stats are deferred to finalizeSearchIndex. It returns the summed document
// length for the page so Finalize can compute the average.
func (w PostgresEshuSearchDocumentWriter) insertSearchIndexPage(
	ctx context.Context,
	scopeID string,
	generationID string,
	documents []eshuSearchIndexDocumentWrite,
	now time.Time,
) (int, EshuSearchDocumentWriteTimings, error) {
	var timings EshuSearchDocumentWriteTimings
	if len(documents) == 0 {
		return 0, timings, nil
	}
	ctx, span := w.startSearchIndexWriteSpan(ctx, scopeID, generationID, len(documents))
	if span != nil {
		defer span.End()
	}
	start := time.Now()
	totalLength := 0
	finish := func(err error, operation string) (int, EshuSearchDocumentWriteTimings, error) {
		result := "success"
		if err != nil {
			result = "error"
			w.recordSearchIndexError(ctx, operation)
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
		}
		w.recordSearchIndexWriteDuration(ctx, "page_total", time.Since(start), result)
		return totalLength, timings, err
	}

	for _, doc := range documents {
		totalLength += doc.Length
	}

	docScopeIDs := make([]string, len(documents))
	docGenerationIDs := make([]string, len(documents))
	docDocumentIDs := make([]string, len(documents))
	docFactIDs := make([]string, len(documents))
	docRepoIDs := make([]string, len(documents))
	docSourceKinds := make([]string, len(documents))
	docContentHashes := make([]string, len(documents))
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
		docContentHashes[i] = doc.ContentHash
		docPayloads[i] = string(payload)
		docLengths[i] = doc.Length
		docUpdatedAts[i] = now
	}

	operationStarted := time.Now()
	if _, err := w.DB.ExecContext(
		ctx,
		eshuSearchIndexBatchDocumentUpsertQuery,
		docScopeIDs,
		docGenerationIDs,
		docDocumentIDs,
		docFactIDs,
		docRepoIDs,
		docSourceKinds,
		docContentHashes,
		docPayloads,
		docLengths,
		docUpdatedAts,
	); err != nil {
		timings.IndexDocumentUpsertDuration += time.Since(operationStarted)
		w.recordSearchIndexWriteDuration(ctx, "document_upsert", timings.IndexDocumentUpsertDuration, "error")
		return finish(fmt.Errorf("write eshu search index documents: %w", err), "document_upsert")
	}
	timings.IndexDocumentUpsertDuration += time.Since(operationStarted)
	w.recordSearchIndexWriteDuration(ctx, "document_upsert", timings.IndexDocumentUpsertDuration, "success")
	w.recordSearchIndexMutation(ctx, "document", "upsert", int64(len(documents)))

	termDocumentIDs, termValues, termKeys, termFrequencies := buildSearchIndexTermColumns(documents)

	if len(termValues) > 0 {
		operationStarted = time.Now()
		if err := w.writeSearchIndexTerms(ctx, scopeID, generationID, termDocumentIDs, termValues, termKeys, termFrequencies); err != nil {
			timings.IndexTermUpsertDuration += time.Since(operationStarted)
			w.recordSearchIndexWriteDuration(ctx, "term_upsert", timings.IndexTermUpsertDuration, "error")
			return finish(fmt.Errorf("write eshu search index terms: %w", err), "term_upsert")
		}
		timings.IndexTermUpsertDuration += time.Since(operationStarted)
		w.recordSearchIndexWriteDuration(ctx, "term_upsert", timings.IndexTermUpsertDuration, "success")
		w.recordSearchIndexMutation(ctx, "term", "upsert", int64(len(termValues)))
	}
	return finish(nil, "")
}

func (w PostgresEshuSearchDocumentWriter) writeSearchIndexTerms(
	ctx context.Context,
	scopeID string,
	generationID string,
	documentIDs []string,
	terms []string,
	termKeys []string,
	frequencies []int,
) error {
	if len(documentIDs) != len(terms) || len(termKeys) != len(terms) || len(frequencies) != len(terms) {
		return fmt.Errorf(
			"write eshu search index terms requires aligned slices: docs=%d terms=%d keys=%d freqs=%d",
			len(documentIDs),
			len(terms),
			len(termKeys),
			len(frequencies),
		)
	}
	if copier := w.searchIndexTermCopier(); copier != nil {
		copied, err := copier.CopySearchIndexTerms(
			ctx,
			scopeID,
			generationID,
			documentIDs,
			terms,
			termKeys,
			frequencies,
		)
		if err != nil {
			if isSearchIndexTermCopyUnsupported(err) {
				return w.writeSearchIndexTermsWithInsert(ctx, scopeID, generationID, documentIDs, terms, termKeys, frequencies)
			}
			return err
		}
		if copied != int64(len(terms)) {
			return fmt.Errorf("copied %d eshu search index terms, want %d", copied, len(terms))
		}
		return nil
	}
	return w.writeSearchIndexTermsWithInsert(ctx, scopeID, generationID, documentIDs, terms, termKeys, frequencies)
}

func (w PostgresEshuSearchDocumentWriter) writeSearchIndexTermsWithInsert(
	ctx context.Context,
	scopeID string,
	generationID string,
	documentIDs []string,
	terms []string,
	termKeys []string,
	frequencies []int,
) error {
	_, err := w.DB.ExecContext(
		ctx,
		eshuSearchIndexBatchTermUpsertQuery,
		scopeID,
		generationID,
		documentIDs,
		terms,
		termKeys,
		frequencies,
	)
	return err
}

func isSearchIndexTermCopyUnsupported(err error) bool {
	var unsupported interface {
		UnsupportedSearchIndexTermCopy() bool
	}
	return errors.As(err, &unsupported) && unsupported.UnsupportedSearchIndexTermCopy()
}

func (w PostgresEshuSearchDocumentWriter) searchIndexTermCopier() eshuSearchIndexTermCopier {
	if w.SearchTermCopier != nil {
		return w.SearchTermCopier
	}
	copier, ok := w.DB.(eshuSearchIndexTermCopier)
	if !ok {
		return nil
	}
	return copier
}

// finalizeSearchIndex runs the single authoritative search-index document retire
// and upserts the stats row. Term rows were generation-cleared before page
// inserts, so finalize does not need a document-keyed term retire. It mirrors
// the deferred retire of the fact store so the read model converges only after
// every page has been inserted.
func (w PostgresEshuSearchDocumentWriter) finalizeSearchIndex(
	ctx context.Context,
	scopeID string,
	generationID string,
	keepDocumentIDs []string,
	documentCount int,
	totalLength int,
	now time.Time,
) (EshuSearchDocumentWriteTimings, error) {
	var timings EshuSearchDocumentWriteTimings
	ctx, span := w.startSearchIndexWriteSpan(ctx, scopeID, generationID, len(keepDocumentIDs))
	if span != nil {
		defer span.End()
	}
	start := time.Now()
	finish := func(err error, operation string) (EshuSearchDocumentWriteTimings, error) {
		result := "success"
		if err != nil {
			result = "error"
			w.recordSearchIndexError(ctx, operation)
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
		}
		w.recordSearchIndexWriteDuration(ctx, "finalize_total", time.Since(start), result)
		return timings, err
	}

	operationStarted := time.Now()
	retireDocumentsResult, err := w.DB.ExecContext(ctx, eshuSearchIndexRetireDocumentsQuery, scopeID, generationID, keepDocumentIDs)
	timings.IndexDocumentRetireDuration += time.Since(operationStarted)
	if err != nil {
		w.recordSearchIndexWriteDuration(ctx, "document_retire", timings.IndexDocumentRetireDuration, "error")
		return finish(fmt.Errorf("retire stale eshu search index documents: %w", err), "document_retire")
	}
	w.recordSearchIndexWriteDuration(ctx, "document_retire", timings.IndexDocumentRetireDuration, "success")
	if affected := rowsAffected(retireDocumentsResult); affected > 0 {
		w.recordSearchIndexMutation(ctx, "document", "retire", affected)
	}

	averageLength := 0.0
	if documentCount > 0 {
		averageLength = float64(totalLength) / float64(documentCount)
	}
	operationStarted = time.Now()
	if _, err := w.DB.ExecContext(ctx, eshuSearchIndexStatsUpsertQuery, scopeID, generationID, documentCount, averageLength, now); err != nil {
		timings.IndexStatsUpsertDuration += time.Since(operationStarted)
		w.recordSearchIndexWriteDuration(ctx, "stats_upsert", timings.IndexStatsUpsertDuration, "error")
		return finish(fmt.Errorf("write eshu search index stats: %w", err), "stats_upsert")
	}
	timings.IndexStatsUpsertDuration += time.Since(operationStarted)
	w.recordSearchIndexWriteDuration(ctx, "stats_upsert", timings.IndexStatsUpsertDuration, "success")
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

type searchIndexTermColumn struct {
	documentID string
	term       string
	frequency  int
}

func buildSearchIndexTermColumns(documents []eshuSearchIndexDocumentWrite) ([]string, []string, []string, []int) {
	if len(documents) == 0 {
		return nil, nil, nil, nil
	}
	docOrder := make([]int, 0, len(documents))
	totalTerms := 0
	for i, doc := range documents {
		if len(doc.Terms) == 0 {
			continue
		}
		docOrder = append(docOrder, i)
		totalTerms += len(doc.Terms)
	}
	if totalTerms == 0 {
		return nil, nil, nil, nil
	}
	sort.Slice(docOrder, func(i int, j int) bool {
		return documents[docOrder[i]].DocumentID < documents[docOrder[j]].DocumentID
	})

	buckets := make(map[string][]searchIndexTermColumn)
	for _, idx := range docOrder {
		doc := documents[idx]
		for term, frequency := range doc.Terms {
			termKey := searchhybrid.TermKey(term)
			buckets[termKey] = append(buckets[termKey], searchIndexTermColumn{
				documentID: doc.DocumentID,
				term:       term,
				frequency:  frequency,
			})
		}
	}

	sortedTermKeys := make([]string, 0, len(buckets))
	for termKey := range buckets {
		sortedTermKeys = append(sortedTermKeys, termKey)
	}
	sort.Strings(sortedTermKeys)

	documentIDs := make([]string, 0, totalTerms)
	terms := make([]string, 0, totalTerms)
	termKeys := make([]string, 0, totalTerms)
	frequencies := make([]int, 0, totalTerms)
	for _, termKey := range sortedTermKeys {
		for _, row := range buckets[termKey] {
			documentIDs = append(documentIDs, row.documentID)
			terms = append(terms, row.term)
			termKeys = append(termKeys, termKey)
			frequencies = append(frequencies, row.frequency)
		}
	}
	return documentIDs, terms, termKeys, frequencies
}
