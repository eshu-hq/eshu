// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

// SpanQueryIncidentContext wraps incident-context evidence reads from active
// incident source facts.
const SpanQueryIncidentContext = "query.incident_context"

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryAdvisoryEvidence {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryIncidentContext)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryIncidentContext)
}
