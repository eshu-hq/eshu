// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s ClaimedService) recordWorkflowClaimWait(ctx context.Context, item workflow.WorkItem) {
	if s.Instruments == nil {
		return
	}
	reference := item.VisibleAt
	if reference.IsZero() {
		reference = item.CreatedAt
	}
	if reference.IsZero() {
		return
	}
	wait := s.now().Sub(reference.UTC()).Seconds()
	if wait < 0 {
		wait = 0
	}
	attrs := metric.WithAttributes(
		telemetry.AttrSourceSystem(item.SourceSystem),
		telemetry.AttrCollectorKind(string(item.CollectorKind)),
	)
	if s.Instruments.WorkflowClaimWaitDuration != nil {
		s.Instruments.WorkflowClaimWaitDuration.Record(ctx, wait, attrs)
	}
	if s.CollectorKind == scope.CollectorTerraformState && s.Instruments.TerraformStateClaimWaitDuration != nil {
		s.Instruments.TerraformStateClaimWaitDuration.Record(ctx, wait, attrs)
	}
}
