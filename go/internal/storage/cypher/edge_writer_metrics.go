// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (w *EdgeWriter) recordGroupedWrite(
	ctx context.Context,
	domain string,
	duration float64,
	stmts []Statement,
) {
	if w.Instruments == nil || len(stmts) == 0 {
		return
	}

	attrs := metric.WithAttributes(telemetry.AttrDomain(domain))
	w.Instruments.SharedEdgeWriteGroups.Add(ctx, 1, attrs)
	w.Instruments.SharedEdgeWriteGroupDuration.Record(ctx, duration, attrs)
	w.Instruments.SharedEdgeWriteGroupStatementCount.Record(ctx, int64(len(stmts)), attrs)
}

func (w *EdgeWriter) recordCodeCallBatch(ctx context.Context, duration float64) {
	if w.Instruments == nil {
		return
	}

	attrs := metric.WithAttributes(telemetry.AttrDomain(reducer.DomainCodeCalls))
	w.Instruments.CodeCallEdgeBatches.Add(ctx, 1, attrs)
	w.Instruments.CodeCallEdgeDuration.Record(ctx, duration, attrs)
}
