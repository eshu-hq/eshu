// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordReducerFactIDOwnerConflict counts a reducer fact write that another
// scope's row refused (#11213) on the counter the collector save path shares,
// eshu_dp_fact_id_scope_conflicts_total, with reason scope and the writer label
// the typed error carries. It adds no log line: the failed intent already logs
// its failure class (fact_id_owner_conflict) and dead-letters, because the error
// is not retryable. Any other error, and a Service without instruments, record
// nothing.
func (s Service) recordReducerFactIDOwnerConflict(ctx context.Context, err error) {
	var conflict *factwrite.OwnerConflictError
	if s.Instruments == nil || s.Instruments.FactIDScopeConflicts == nil || !errors.As(err, &conflict) {
		return
	}
	s.Instruments.FactIDScopeConflicts.Add(context.WithoutCancel(ctx), int64(conflict.Count), metric.WithAttributes(
		telemetry.AttrReason(telemetry.FactIDOwnerConflictReasonScope),
		attribute.String(telemetry.MetricDimensionWriter, conflict.Writer),
	))
}
