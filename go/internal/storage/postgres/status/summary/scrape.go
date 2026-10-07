// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"time"
)

const (
	// SourceLastRow is the last stored row this process served as fresh,
	// served again because the current row could not be. It is stale by
	// definition and its age keeps growing with the database clock.
	SourceLastRow Source = "last_row"
	// SourceZero is the empty summary, served when the current row could not
	// be and the process has served no fresh row yet.
	SourceZero Source = "zero"
)

// ScrapeHooks are the model-specific steps of ModelReader.ReadScrape.
type ScrapeHooks[T any] struct {
	// Select runs Select on the reader's queryer, with the caller's read
	// labeling around it.
	Select func(ctx context.Context) (Selection, error)
	// Decode decodes entries with the live read's decoder. Decode(nil) is the
	// zero summary.
	Decode func([]Entry) (T, error)
	// Observe records one scrape decision.
	Observe func(ctx context.Context, o ScrapeObservation)
}

// ScrapeResult is a decoded model, where it came from, and how old it is.
type ScrapeResult[T any] struct {
	Value T
	// Source is SourceModel, SourceLastRow, or SourceZero; Reason explains a
	// non-model source with the Select reason that rejected the row.
	Source Source
	Reason Reason
	// AsOf is the as_of of the row served; zero for SourceZero.
	AsOf time.Time
	// Age is the database clock at the read minus AsOf, never negative; zero
	// for SourceZero.
	Age time.Duration
	// Stale is true unless Source is SourceModel.
	Stale bool
}

// Enabled reports whether the reader is on. A nil reader is off.
func (r *ModelReader[T]) Enabled() bool {
	return r != nil && r.Config.Enabled
}

// ReadScrape answers a poll that must never run the live statement (the
// runtime /metrics scrape). A row that passes every fence is served and
// remembered. Anything else is served from the newest row this process can
// decode (a fresh row it served, or a stale row Select decoded under
// DecodeStale), advanced by its age at this read, or from the zero summary when
// it has none, and is marked stale; the live statement is never an option,
// because a scrape from every process would turn a stopped writer into a herd
// of expensive statements. A database error is returned: it is neither a stale
// serve nor a live run. Call it only when Enabled; a reader that is off has no
// stored row to serve. The value is decoded fresh on every call, so concurrent
// scrapes share nothing but the remembered entries.
func (r *ModelReader[T]) ReadScrape(ctx context.Context, h ScrapeHooks[T]) (ScrapeResult[T], error) {
	selection, err := h.Select(ctx)
	if err != nil {
		return ScrapeResult[T]{}, err
	}
	reason := selection.Reason
	if selection.Source == SourceModel {
		value, err := h.Decode(selection.Entries)
		if err == nil {
			r.last.remember(selection.Stored, selection.AsOf.UTC())
			result := ScrapeResult[T]{Value: value, Source: SourceModel, Reason: ReasonFresh, AsOf: selection.AsOf.UTC(), Age: selection.Age}
			h.Observe(ctx, scrapeObservation(result))
			return result, nil
		}
		reason = ReasonDecode
	}
	// A stale row this process has not served is still the newest row it can
	// decode: keep it as the last row when it is newer than the one held and the
	// production decoder accepts it, so a restart during a writer outage exports
	// the stored counts with their true age instead of zeros.
	if selection.Stored != nil && r.last.newer(selection.AsOf.UTC()) {
		if _, err := h.Decode(selection.Stored); err == nil {
			r.last.remember(selection.Stored, selection.AsOf.UTC())
		}
	}
	result := r.serveLastRow(h, selection.Now, reason)
	h.Observe(ctx, scrapeObservation(result))
	return result, nil
}

// serveLastRow serves the held row advanced to now, or the zero summary.
func (r *ModelReader[T]) serveLastRow(h ScrapeHooks[T], now time.Time, reason Reason) ScrapeResult[T] {
	if entries, asOf, ok := r.last.recall(); ok {
		age := max(now.Sub(asOf), 0)
		if aged, err := AddAge(entries, age); err == nil {
			if value, err := h.Decode(aged); err == nil {
				return ScrapeResult[T]{Value: value, Source: SourceLastRow, Reason: reason, AsOf: asOf, Age: age, Stale: true}
			}
		}
	}
	// No held row, or one that no longer decodes: the empty summary cannot fail.
	value, _ := h.Decode(nil)
	return ScrapeResult[T]{Value: value, Source: SourceZero, Reason: reason, Stale: true}
}

func scrapeObservation[T any](result ScrapeResult[T]) ScrapeObservation {
	return ScrapeObservation{Source: result.Source, Reason: result.Reason, AsOf: result.AsOf, Age: result.Age}
}
