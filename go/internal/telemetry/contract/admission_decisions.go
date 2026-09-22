// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

const (
	// SpanQueryAdmissionDecisions wraps bounded reducer-owned correlation
	// admission decision reads. The span carries only stable route and
	// capability attributes; domains, scope ids, anchors, and source handles
	// stay out of telemetry labels.
	SpanQueryAdmissionDecisions = "query.admission_decisions"
)
