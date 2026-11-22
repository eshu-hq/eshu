// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// loadSourceBackpressureThresholds reads the per-source slowdown
// thresholds (#10882) for the coordinator snapshot's source_pacing
// rows. A partial set is a startup error, never silent
// unstuck-by-default rows; unset disables stuck evaluation.
func loadSourceBackpressureThresholds(getenv func(string) string) (workflow.SourceBackpressureConfig, error) {
	return collector.LoadSourceBackpressureConfig(getenv)
}

// backpressureConfig reports the per-source slowdown thresholds the
// coordinator snapshot's source_pacing rows evaluate the stuck flag
// from. A nil reader leaves stuck unevaluated, matching the zero-value
// contract.
func (r *StatusSummaryReader) backpressureConfig() workflow.SourceBackpressureConfig {
	if r == nil {
		return workflow.SourceBackpressureConfig{}
	}
	return r.sourceBackpressure
}
