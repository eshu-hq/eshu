// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CursorOutcomeMetric counts rank-walk requests by tool and outcome.
const CursorOutcomeMetric = "eshu_dp_query_rank_cursor_total"

// Outcome label values (a closed set).
const (
	// OutcomeServed: a page was served and the walk continues.
	OutcomeServed = "served"
	// OutcomeWindowEnd: the page served the last window row.
	OutcomeWindowEnd = "window_end"
	// OutcomeBasisMismatch: HTTP 409; the window or ranked order changed.
	OutcomeBasisMismatch = "basis_mismatch"
	// OutcomeInvalid: HTTP 400; the cursor or request was malformed.
	OutcomeInvalid = "invalid"
	// OutcomeUnsupported: HTTP 400; the request shape cannot be walked.
	OutcomeUnsupported = "unsupported"
)

var outcomes = map[string]struct{}{
	OutcomeServed:        {},
	OutcomeWindowEnd:     {},
	OutcomeBasisMismatch: {},
	OutcomeInvalid:       {},
	OutcomeUnsupported:   {},
}

// errorOutcomes maps each rank error code to its outcome label. A code
// missing here records OutcomeInvalid through boundedOutcome.
var errorOutcomes = map[querycontract.ErrorCode]string{
	ErrorCodeInvalid:       OutcomeInvalid,
	ErrorCodeUnsupported:   OutcomeUnsupported,
	ErrorCodeBasisMismatch: OutcomeBasisMismatch,
}

// Span attributes set on the request span of every rank-walk request.
const (
	// AttrOutcome is the outcome label value.
	AttrOutcome attribute.Key = "search.rank_cursor.outcome"
	// AttrReason is the error reason, set only on a 400 or 409.
	AttrReason attribute.Key = "search.rank_cursor.reason"
	// AttrServed is the number of rows the walk has served through this
	// page.
	AttrServed attribute.Key = "search.rank_cursor.served"
)

var (
	instrumentsOnce sync.Once
	outcomeCounter  metric.Int64Counter
)

// initInstruments registers the outcome counter once against the global
// meter provider. A registration error leaves the counter nil, so a
// telemetry fault never fails a read.
func initInstruments() {
	instrumentsOnce.Do(func() {
		// The literal name, not CursorOutcomeMetric, is what the
		// telemetry-coverage gate reads to prove the metric is registered.
		counter, err := otel.Meter("eshu/go/internal/query").Int64Counter(
			"eshu_dp_query_rank_cursor_total",
			metric.WithDescription("Rank-walk (rank_cursor) requests on find_code and search_entity_content by tool and outcome: served, window_end, basis_mismatch (409), invalid (400), unsupported (400)"),
		)
		if err == nil {
			outcomeCounter = counter
		}
	})
}

// resetInstrumentsForTest rebinds the counter so a test can register it
// against its own meter provider.
func resetInstrumentsForTest() {
	instrumentsOnce = sync.Once{}
	outcomeCounter = nil
}

func boundedOutcome(outcome string) string {
	if _, ok := outcomes[outcome]; ok {
		return outcome
	}
	return OutcomeInvalid
}

// ToolSearchFileContent labels a refused rank_cursor on search_file_content.
// It is a telemetry label only: the file route cannot walk.
const ToolSearchFileContent = "search_file_content"

func boundedTool(tool string) string {
	if _, ok := toolCodes[tool]; ok || tool == ToolSearchFileContent {
		return tool
	}
	return "unknown"
}

// RecordOutcome counts one rank-walk request and tags the request span in
// ctx with the outcome and the rows served so far. It never records the
// query text or a cursor value.
func RecordOutcome(ctx context.Context, tool, outcome string, servedRows int) {
	outcome = boundedOutcome(outcome)
	trace.SpanFromContext(ctx).SetAttributes(AttrOutcome.String(outcome), AttrServed.Int(servedRows))
	initInstruments()
	if outcomeCounter == nil {
		return
	}
	outcomeCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tool", boundedTool(tool)),
		attribute.String("outcome", outcome),
	))
}

// RecordError records the outcome for a failed rank-walk request: the
// outcome derived from err's code, and the reason on the request span. A
// non-rank error records nothing; the route's own failure telemetry covers
// it.
func RecordError(ctx context.Context, tool string, err error) {
	var rankErr *Error
	if !errors.As(err, &rankErr) {
		return
	}
	trace.SpanFromContext(ctx).SetAttributes(AttrReason.String(rankErr.Reason))
	RecordOutcome(ctx, tool, errorOutcomes[rankErr.Code], 0)
}

// Answer records the outcome of a rank-walk failure and answers the
// request with it (see WriteError). It reports false, recording and writing
// nothing, when err is not a *Error.
func Answer(w http.ResponseWriter, r *http.Request, tool string, err error) bool {
	var rankErr *Error
	if err == nil || !errors.As(err, &rankErr) {
		return false
	}
	RecordError(r.Context(), tool, rankErr)
	return WriteError(w, r, rankErr)
}

// PageOutcome returns the outcome label for a served page.
func PageOutcome(page Page) string {
	if page.WindowEnd {
		return OutcomeWindowEnd
	}
	return OutcomeServed
}
