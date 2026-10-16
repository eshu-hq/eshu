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
	// AttrReason is the reason label value: ReasonNone on a served page, the
	// error reason on a 400 or 409.
	AttrReason attribute.Key = "search.rank_cursor.reason"
	// AttrPosition is the number of rows the walk has served through this
	// page (0 on an error).
	AttrPosition attribute.Key = "search.rank_cursor.position"
	// AttrLane is the lane that served the page; set on served pages only.
	AttrLane attribute.Key = "search.rank_cursor.lane"
	// AttrOrder is the order of the served page; set on served pages only.
	AttrOrder attribute.Key = "search.rank_cursor.order"
)

// reasons is the closed reason label set.
var reasons = map[string]struct{}{
	ReasonNone: {}, ReasonMalformed: {}, ReasonToolMismatch: {}, ReasonInvalidLimit: {},
	ReasonBasisChanged: {}, ReasonOrderChanged: {}, ReasonRankerDisabled: {},
	ReasonScopeNotSingleRepo: {}, ReasonFileSearch: {}, ReasonOffsetConflict: {}, ReasonCursorConflict: {},
}

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
			metric.WithDescription("Rank-walk (rank_cursor) requests on find_code and search_entity_content by tool, outcome (served, window_end, basis_mismatch, invalid, unsupported), and reason (none, basis_changed, order_changed, or the 400 reason)"),
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

func boundedReason(reason string) string {
	if _, ok := reasons[reason]; ok {
		return reason
	}
	return ReasonNone
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

// record counts one rank-walk request and tags the request span in ctx. It
// never records the query text, a repository ID, or a cursor value.
func record(ctx context.Context, tool, outcome, reason string, position int, attrs ...attribute.KeyValue) {
	outcome, reason = boundedOutcome(outcome), boundedReason(reason)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(AttrOutcome.String(outcome), AttrReason.String(reason), AttrPosition.Int(position))
	span.SetAttributes(attrs...)
	initInstruments()
	if outcomeCounter == nil {
		return
	}
	outcomeCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tool", boundedTool(tool)),
		attribute.String("outcome", outcome),
		attribute.String("reason", reason),
	))
}

// RecordPage records a served page: outcome served or window_end, reason
// none, and the page's position, lane, and order on the request span.
func RecordPage(ctx context.Context, tool string, page Page) {
	outcome := OutcomeServed
	if page.WindowEnd {
		outcome = OutcomeWindowEnd
	}
	lane, order := page.Lane, page.Order
	if _, ok := laneCodes[lane]; !ok {
		lane = "unknown"
	}
	if order != OrderGraphName && order != OrderHybrid && order != OrderLexical {
		order = "unknown"
	}
	record(ctx, tool, outcome, ReasonNone, page.Position, AttrLane.String(lane), AttrOrder.String(order))
}

// RecordError records a failed rank-walk request: the outcome derived from
// err's code and err's reason. A non-rank error records nothing; the route's
// own failure telemetry covers it.
func RecordError(ctx context.Context, tool string, err error) {
	var rankErr *Error
	if !errors.As(err, &rankErr) {
		return
	}
	record(ctx, tool, errorOutcomes[rankErr.Code], rankErr.Reason, 0)
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
