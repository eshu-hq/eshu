// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The closed set of blocks the deferred maintenance pass reports (#8082). Each
// value names one separately timed step: it is the suffix of the block's span
// name (see blockSpanNamePrefix), the block= field of its
// deferred_backfill_block_completed log line, and the block label of
// eshu_dp_deferred_backfill_block_duration_seconds. The values are defined in
// the telemetry contract, so the metric label set cannot drift from them.
const (
	// BlockCatalogLoad is the repository catalog read, row iteration, and decode.
	BlockCatalogLoad = telemetry.DeferredBackfillBlockCatalogLoad
	// BlockParamsBuild is the anchor-scoped fact query parameter build.
	BlockParamsBuild = telemetry.DeferredBackfillBlockParamsBuild
	// BlockPartitionList is the active scope-generation partition listing.
	BlockPartitionList = telemetry.DeferredBackfillBlockPartitionList
	// BlockMemoGate is the partition memo lookup and skip decision.
	BlockMemoGate = telemetry.DeferredBackfillBlockMemoGate
	// BlockFactLoad is the per-partition fact load, merge, and ArgoCD config reload.
	BlockFactLoad = telemetry.DeferredBackfillBlockFactLoad
	// BlockDiscover is evidence discovery, dedupe, and per-source-repo grouping.
	BlockDiscover = telemetry.DeferredBackfillBlockDiscover
	// BlockWriteBatches is the per-repository evidence write batches.
	BlockWriteBatches = telemetry.DeferredBackfillBlockWriteBatches
	// BlockFanIn is the per-partition readiness and memo publication.
	BlockFanIn = telemetry.DeferredBackfillBlockFanIn
	// BlockReopen is one reopen domain call after the backfill commits.
	BlockReopen = telemetry.DeferredBackfillBlockReopen
)

// blockSpanNamePrefix is joined with a block name to form its span name, for
// example relationship.backfill.catalog_load.
const blockSpanNamePrefix = "relationship.backfill."

// blockLogName is the structured log line emitted once per completed block per
// pass; blockFailedLogName is emitted instead, once, for a block that fails.
const (
	blockLogName         = telemetry.DeferredBackfillBlockLogCompleted
	blockFailedLogName   = telemetry.DeferredBackfillBlockLogFailed
	blockDegradedLogName = telemetry.DeferredBackfillBlockLogDegraded
)

// BlockAttr is one named value a block records on its span and its log line.
// Build values with IntAttr, FloatAttr, and StringAttr so the span attribute
// and the log field always carry the same number.
type BlockAttr struct {
	key  string
	kind blockAttrKind
	i    int64
	f    float64
	s    string
}

type blockAttrKind int

const (
	blockAttrInt blockAttrKind = iota
	blockAttrFloat
	blockAttrString
	blockAttrBool
)

// IntAttr builds an integer block attribute.
func IntAttr(key string, value int) BlockAttr {
	return BlockAttr{key: key, kind: blockAttrInt, i: int64(value)}
}

// FloatAttr builds a floating-point block attribute, logged to two decimals.
func FloatAttr(key string, value float64) BlockAttr {
	return BlockAttr{key: key, kind: blockAttrFloat, f: value}
}

// StringAttr builds a string block attribute. Use it only for closed-set
// identifiers such as a reopen domain; it is logged unquoted.
func StringAttr(key, value string) BlockAttr {
	return BlockAttr{key: key, kind: blockAttrString, s: value}
}

// ReopenBlockAttrs returns a fresh, ordered attribute set for one reopen
// domain: its name, reopened work-item count, then memo-skipped count. Both
// whole and targeted passes use this shape for spans and completion logs.
func ReopenBlockAttrs(domain string, reopened, skippedByMemo int) []BlockAttr {
	return []BlockAttr{
		StringAttr("domain", domain),
		IntAttr("reopened", reopened),
		IntAttr("skipped_by_memo", skippedByMemo),
	}
}

// BoolAttr builds a boolean block attribute, logged as true or false.
func BoolAttr(key string, value bool) BlockAttr {
	attr := BlockAttr{key: key, kind: blockAttrBool}
	if value {
		attr.i = 1
	}
	return attr
}

func (a BlockAttr) spanAttribute() attribute.KeyValue {
	switch a.kind {
	case blockAttrBool:
		return attribute.Bool(a.key, a.i == 1)
	case blockAttrFloat:
		return attribute.Float64(a.key, a.f)
	case blockAttrString:
		return attribute.String(a.key, a.s)
	default:
		return attribute.Int64(a.key, a.i)
	}
}

func (a BlockAttr) logField() string {
	switch a.kind {
	case blockAttrBool:
		return fmt.Sprintf("%s=%t", a.key, a.i == 1)
	case blockAttrFloat:
		return fmt.Sprintf("%s=%.2f", a.key, a.f)
	case blockAttrString:
		return fmt.Sprintf("%s=%s", a.key, a.s)
	default:
		return fmt.Sprintf("%s=%d", a.key, a.i)
	}
}

// BlockSpan times one block of the deferred maintenance pass. It owns a child
// span (when a tracer is present), records one
// eshu_dp_deferred_backfill_block_duration_seconds sample (when instruments are
// present), and emits exactly one log line: deferred_backfill_block_completed
// when the block ends successfully, deferred_backfill_block_failed (with the
// quoted error and no histogram sample) when it fails the pass, or
// deferred_backfill_block_degraded (via EndDegraded, histogram sample kept)
// when it errored but the pass continued on a fallback. A nil *BlockSpan is
// valid and records nothing, so a code path that runs outside the maintenance
// pass pays no cost and needs no branch.
type BlockSpan struct {
	ctx         context.Context
	block       string
	start       time.Time
	span        trace.Span
	instruments *telemetry.Instruments
}

// StartBlock begins timing block, one of the Block* constants. When tracer is
// non-nil it starts a span named relationship.backfill.<block> as a child of
// the span already carried by ctx. The log line is emitted regardless of the
// tracer so an operator without tracing still sees the per-block durations.
// A nil instruments skips the histogram; the partition-scoped pass passes nil
// so the whole pass's series keep measuring only the whole pass. ctx is not
// replaced: callers keep writing attributes to the enclosing
// relationship.backfill_deferred span through trace.SpanFromContext.
func StartBlock(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	block string,
) *BlockSpan {
	b := &BlockSpan{ctx: ctx, block: block, start: time.Now(), instruments: instruments}
	if tracer != nil {
		_, b.span = tracer.Start(ctx, blockSpanNamePrefix+block, trace.WithTimestamp(b.start))
	}
	return b
}

// End finishes the block now. A non-nil err is recorded on the span and logged
// once as deferred_backfill_block_failed with the block's duration and the
// quoted error. The histogram sample and the completed line are skipped, so the
// completed series counts only blocks that finished.
func (b *BlockSpan) End(err error, attrs ...BlockAttr) {
	b.EndAt(time.Now(), err, attrs...)
}

// EndAt finishes the block with an explicit end time. It lets a block whose
// attributes are only known after a later step still report the duration of
// its own work, so sibling block durations stay disjoint.
func (b *BlockSpan) EndAt(end time.Time, err error, attrs ...BlockAttr) {
	if b == nil {
		return
	}
	b.endSpan(end, err, attrs)
	duration := end.Sub(b.start).Seconds()
	if err != nil {
		log.Printf("%s block=%s duration_s=%.2f error=%q", blockFailedLogName, b.block, duration, err.Error())
		return
	}
	b.recordSample(duration)
	log.Print(blockLine(blockLogName, b.block, duration, attrs))
}

// EndDegraded finishes a block that errored while the pass continued on a
// fallback path (today only the memo gate falling back to a full load). The
// error is recorded on the span with Error status and a fallback=true
// attribute, the block still records its histogram sample because it ran and
// fell back, and exactly one deferred_backfill_block_degraded line is logged
// with the block's fields plus the quoted error. It never emits the failed or
// completed line: a failed line means the pass failed.
func (b *BlockSpan) EndDegraded(err error, attrs ...BlockAttr) {
	if b == nil {
		return
	}
	attrs = append(attrs, BoolAttr("fallback", true))
	end := time.Now()
	b.endSpan(end, err, attrs)
	duration := end.Sub(b.start).Seconds()
	b.recordSample(duration)
	line := blockLine(blockDegradedLogName, b.block, duration, attrs)
	if err != nil {
		line += fmt.Sprintf(" error=%q", err.Error())
	}
	log.Print(line)
}

// endSpan sets the attributes, records a non-nil err, and ends the span.
func (b *BlockSpan) endSpan(end time.Time, err error, attrs []BlockAttr) {
	if b.span == nil {
		return
	}
	for _, attr := range attrs {
		b.span.SetAttributes(attr.spanAttribute())
	}
	if err != nil {
		b.span.RecordError(err)
		b.span.SetStatus(codes.Error, err.Error())
	}
	b.span.End(trace.WithTimestamp(end))
}

// recordSample records one eshu_dp_deferred_backfill_block_duration_seconds
// sample labeled with the block, when instruments are present.
func (b *BlockSpan) recordSample(duration float64) {
	if b.instruments == nil {
		return
	}
	b.instruments.DeferredBackfillBlockDuration.Record(b.ctx, duration,
		metric.WithAttributes(attribute.String(telemetry.MetricDimensionBlock, b.block)))
}

// blockLine formats one block log line: name, block, duration, then fields.
func blockLine(name, block string, duration float64, attrs []BlockAttr) string {
	var line strings.Builder
	fmt.Fprintf(&line, "%s block=%s duration_s=%.2f", name, block, duration)
	for _, attr := range attrs {
		line.WriteByte(' ')
		line.WriteString(attr.logField())
	}
	return line.String()
}

// blockTelemetryKey carries the pass's tracer to code that has no tracer
// parameter, so the fact-load and fan-in helpers can report their blocks
// without changing their exported signatures.
type blockTelemetryKey struct{}

type blockTelemetry struct {
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// withBlockTelemetry marks ctx as running inside the maintenance pass. Only
// RunAllRelationshipEvidence calls it, so helpers reached from other callers
// (for example the targeted partition pass) stay silent.
func withBlockTelemetry(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) context.Context {
	return context.WithValue(ctx, blockTelemetryKey{}, blockTelemetry{tracer: tracer, instruments: instruments})
}

// startBlockFromContext starts a block when ctx carries pass telemetry and
// returns nil (a no-op BlockSpan) otherwise.
func startBlockFromContext(ctx context.Context, block string) *BlockSpan {
	pass, ok := ctx.Value(blockTelemetryKey{}).(blockTelemetry)
	if !ok {
		return nil
	}
	return StartBlock(ctx, pass.tracer, pass.instruments, block)
}
