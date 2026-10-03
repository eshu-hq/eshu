// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// stageFailedErrorMaxBytes bounds the error attribute of the stage_failed
// event (#7546). The guarded reader's errors already carry a fixed site
// string, so the cap is a backstop against an unbounded driver message, not a
// routine truncation.
const stageFailedErrorMaxBytes = 256

// stageFailedSpanStatus is the fixed, bounded status description set on the
// handler span for a handler-owned 5xx; the error itself is recorded as a span
// event, never in the status text.
const stageFailedSpanStatus = "supply-chain impact findings stage failed"

// supplyChainQueryStageTimer emits per-backing-read stage timings for
// supply-chain query routes, mirroring the repository_query/service_query
// stage-timer convention (go/internal/query/repository/query_timing.go,
// go/internal/query/service/query_timing.go). Issue #7007: the
// impact/findings route combined a Postgres findings read, a Postgres
// readiness-snapshot read, and up to three graph probes with no stage-level
// signal, so a 13-25s request had nothing to attribute the time to.
type supplyChainQueryStageTimer struct {
	logger    *slog.Logger
	operation string
	repoID    string
	stage     string
	startedAt time.Time
}

// startSupplyChainQueryStage logs a bounded stage start and returns a timer
// for the matching completion event. A nil logger makes every call a no-op.
func startSupplyChainQueryStage(
	ctx context.Context,
	logger *slog.Logger,
	operation string,
	repoID string,
	stage string,
) supplyChainQueryStageTimer {
	timer := supplyChainQueryStageTimer{
		logger:    logger,
		operation: operation,
		repoID:    repoID,
		stage:     stage,
		startedAt: time.Now(),
	}
	if logger != nil {
		logger.InfoContext(
			ctx, "supply chain query stage started",
			telemetry.EventAttr("supply_chain_query.stage_started"),
			log.Operation(operation),
			slog.String("stage", stage),
			slog.String("repo_id", repoID),
		)
	}
	return timer
}

// Done emits a bounded completion event with duration and caller-owned
// attributes (row counts, truncation, error class, and similar bounded
// values — never raw payloads or unbounded identifiers).
func (t supplyChainQueryStageTimer) Done(ctx context.Context, attrs ...slog.Attr) {
	if t.logger == nil {
		return
	}
	base := []slog.Attr{
		telemetry.EventAttr("supply_chain_query.stage_completed"),
		log.Operation(t.operation),
		slog.String("stage", t.stage),
		slog.String("repo_id", t.repoID),
		slog.Float64("duration_seconds", time.Since(t.startedAt).Seconds()),
	}
	base = append(base, attrs...)
	t.logger.LogAttrs(ctx, slog.LevelInfo, "supply chain query stage completed", base...)
}

// Failed emits ONE ERROR-level supply_chain_query.stage_failed event for a
// handler-owned 5xx on this stage (#7546). It carries the stage, repository,
// the error text bounded to 256 bytes, and the closed-set error_site and
// error_cause classes from classifyReaderFailure. querycontract.WriteError
// never logs, so without this event a 500 left no attributable log line. A nil
// logger makes the call a no-op, like Done.
func (t supplyChainQueryStageTimer) Failed(ctx context.Context, err error) {
	if t.logger == nil || err == nil {
		return
	}
	site, cause := classifyReaderFailure(err)
	t.logger.LogAttrs(
		ctx, slog.LevelError, "supply chain query stage failed",
		telemetry.EventAttr("supply_chain_query.stage_failed"),
		log.Operation(t.operation),
		slog.String("stage", t.stage),
		slog.String("repo_id", t.repoID),
		slog.Float64("duration_seconds", time.Since(t.startedAt).Seconds()),
		slog.String("error", boundedErrorText(err, stageFailedErrorMaxBytes)),
		slog.String("error_site", site),
		slog.String("error_cause", cause),
	)
}

// failStage records a handler-owned 5xx on the handler span and logs the
// failed stage. The span carries the error as an exception event and a fixed
// Error status so the next occurrence is findable by trace (#7546).
func failStage(ctx context.Context, span trace.Span, timer supplyChainQueryStageTimer, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, stageFailedSpanStatus)
	timer.Failed(ctx, err)
}

// boundedErrorText returns err's text cut to at most maxBytes without
// splitting a UTF-8 sequence.
func boundedErrorText(err error, maxBytes int) string {
	text := err.Error()
	if len(text) <= maxBytes {
		return text
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// classifyReaderFailure reduces an error chain to two closed-set labels that
// never carry error text, so a fixed-text failure such as "PostgreSQL reader
// connection unavailable" still says which cause class produced it (#7546).
// It uses errors.Is and errors.As only, with no driver import.
//
// site is one of reader_stale, reader_unavailable, other. The writer-side and
// topology sentinels (ErrWriterUnavailable, ErrMissingCheckpoint,
// ErrWrongTopology) live in internal/runtime/postgres, which the query layer
// must not import, so they report as other.
//
// cause is one of deadline_exceeded, canceled, conn_done, eof, conn_refused,
// conn_reset, net_timeout, sqlstate_<two-character class>, unknown.
func classifyReaderFailure(err error) (site, cause string) {
	if err == nil {
		return "other", "unknown"
	}
	switch {
	case errors.Is(err, db.ErrReaderStale):
		site = "reader_stale"
	case errors.Is(err, db.ErrReaderUnavailable):
		site = "reader_unavailable"
	default:
		site = "other"
	}
	return site, classifyFailureCause(err)
}

func classifyFailureCause(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, sql.ErrConnDone):
		return "conn_done"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "eof"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "conn_refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "conn_reset"
	}
	var stateErr interface{ SQLState() string }
	if errors.As(err, &stateErr) {
		if class := sqlStateClass(stateErr.SQLState()); class != "" {
			return "sqlstate_" + class
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "net_timeout"
	}
	return "unknown"
}

// sqlStateClass returns the two-character SQLSTATE class, or "" when code is
// not a well-formed SQLSTATE, so a malformed value can never reach a log.
func sqlStateClass(code string) string {
	if len(code) < 2 {
		return ""
	}
	for _, c := range code[:2] {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return ""
		}
	}
	return code[:2]
}
