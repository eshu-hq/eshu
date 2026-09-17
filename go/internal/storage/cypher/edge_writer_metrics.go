// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordGroupedWrite records one shared-edge execution unit — an atomic
// statement group ("group") or the whole post-commit evidence-artifact
// phase ("artifact-sequential") — under the grouped-write instruments with
// an execution_mode label, so the instruments keep covering the whole
// repo_dependency write instead of silently dropping the artifact phase
// (#6184 owner finding on PR #6730). Dashboards that sum by domain alone
// are unaffected; filter execution_mode="group" for the pre-#6730 series.
func (w *EdgeWriter) recordGroupedWrite(
	ctx context.Context,
	domain string,
	executionMode string,
	duration float64,
	stmts []Statement,
) {
	if w.Instruments == nil || len(stmts) == 0 {
		return
	}

	attrs := metric.WithAttributes(telemetry.AttrDomain(domain), telemetry.AttrExecutionMode(executionMode))
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
