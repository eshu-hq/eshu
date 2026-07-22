// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

const (
	// SpanAttrGraphReadOutcome carries the closed graph-read result vocabulary:
	// success, slow, recovered, deadline, caller_deadline, unavailable,
	// canceled, or error.
	SpanAttrGraphReadOutcome = "eshu.graph_read.outcome"
	// SpanAttrGraphReadAttempts reports the bounded total attempt count (0-2).
	SpanAttrGraphReadAttempts = "eshu.graph_read.attempts"
	// SpanAttrGraphReadConfiguredDeadlineMS reports the configured client safety
	// deadline; an earlier parent deadline remains authoritative at execution.
	SpanAttrGraphReadConfiguredDeadlineMS = "eshu.graph_read.configured_deadline_ms"
)
