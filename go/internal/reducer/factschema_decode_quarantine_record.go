// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file holds the durable-quarantine persistence concern split out of
// factschema_decode.go to keep that file under the repo's 500-line cap
// (issue #4630): recordQuarantinedFacts, the visible dead-letter emitter
// (metric + structured log) for facts a batch extractor quarantined during
// decode, and persistQuarantinedFacts, the best-effort batched write of those
// records to the durable reducer_input_invalid_facts read surface. Both
// operate on the quarantinedFact/QuarantinedFactRecord types and the
// classification machinery (factDecodeError, partitionDecodeFailures, and the
// attribute-shape adapters) that remain in factschema_decode.go — this is a
// move-only split with no behavior change.

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// recordQuarantinedFacts emits the visible, operator-diagnosable dead-letter for
// each fact a batch extractor quarantined during decode: it increments the
// eshu_dp_reducer_input_invalid_facts_total counter (labeled by domain and
// fact_kind) and logs one structured error per fact naming the fact id and the
// missing required field, then returns the count for the handler to record in
// Result.SubSignals["input_invalid_facts"]. This is the difference from the old
// silent skip — a quarantined fact is a first-class, dashboard-visible,
// log-searchable event, not an anonymous counter bump.
//
// It is safe to call with a nil instruments pointer (the counter is skipped, the
// logs still emit) and with an empty slice (a no-op returning 0).
//
// It also persists the quarantined facts to the durable
// reducer_input_invalid_facts read surface (issue #4630) through the writer
// stashed on ctx by Service.executeWithTelemetry (see WithQuarantineWriter in
// quarantine_writer.go), via persistQuarantinedFacts. That persistence is
// strictly best-effort: a write failure is logged and counted but NEVER
// returned, so a durable-write outage can never turn a per-fact quarantine
// (which is by design non-fatal) into a fatal intent failure.
func recordQuarantinedFacts(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain Domain,
	scopeID, generationID string,
	quarantined []quarantinedFact,
) int {
	if len(quarantined) == 0 {
		return 0
	}
	now := time.Now().UTC()
	records := make([]QuarantinedFactRecord, 0, len(quarantined))
	for _, q := range quarantined {
		if instruments != nil && instruments.ReducerInputInvalidFacts != nil {
			instruments.ReducerInputInvalidFacts.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrDomain(string(domain)),
				telemetry.AttrFactKind(q.factKind),
			))
		}
		slog.ErrorContext(
			ctx, "reducer input_invalid fact quarantined",
			log.Domain(string(domain)),
			log.ScopeID(scopeID),
			log.GenerationID(generationID),
			slog.String("fact_id", q.factID),
			slog.String("fact_kind", q.factKind),
			slog.String("missing_field", q.field),
			slog.String("failure_class", q.classification),
		)
		records = append(records, QuarantinedFactRecord{
			FactID:       q.factID,
			FactKind:     q.factKind,
			MissingField: q.field,
			FailureClass: q.classification,
			Domain:       string(domain),
			ScopeID:      scopeID,
			GenerationID: generationID,
			DecidedAt:    now,
		})
	}
	persistQuarantinedFacts(ctx, quarantineWriterFromContext(ctx), instruments, records)
	return len(quarantined)
}

// persistQuarantinedFacts writes records to writer in one batched round trip
// (per intent, not per fact) and records batch-size/committed/error telemetry.
// A nil writer (the default: Service.QuarantineWriter unset, or every
// handler-level unit test) or an empty records slice makes this a no-op. A
// write error is logged and counted through
// eshu_dp_reducer_input_invalid_fact_write_errors_total (reason=write_error)
// and NEVER returned: this durable row is an operator convenience read
// surface, not a correctness dependency, so an outage in it must never
// dead-letter or fail the owning intent (which already correctly quarantined
// the fact via the counter/log above regardless of this write's outcome).
func persistQuarantinedFacts(
	ctx context.Context,
	writer QuarantinedFactWriter,
	instruments *telemetry.Instruments,
	records []QuarantinedFactRecord,
) {
	if writer == nil || len(records) == 0 {
		return
	}
	if instruments != nil && instruments.ReducerInputInvalidFactWriteBatchSize != nil {
		instruments.ReducerInputInvalidFactWriteBatchSize.Record(ctx, float64(len(records)))
	}
	if err := writer.WriteQuarantinedFacts(ctx, records); err != nil {
		if instruments != nil && instruments.ReducerInputInvalidFactWriteErrors != nil {
			instruments.ReducerInputInvalidFactWriteErrors.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrReason("write_error"),
			))
		}
		slog.ErrorContext(
			ctx, "reducer input_invalid fact quarantine durable write failed; continuing without durable row (best-effort, non-fatal)",
			slog.Int("record_count", len(records)),
			slog.String("error", err.Error()),
		)
		return
	}
	if instruments != nil && instruments.ReducerInputInvalidFactsCommitted != nil {
		instruments.ReducerInputInvalidFactsCommitted.Add(ctx, int64(len(records)))
	}
}
