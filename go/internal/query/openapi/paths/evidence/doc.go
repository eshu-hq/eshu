// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidence holds the OpenAPI 3.0 path fragments for Eshu's
// evidence-surface reads: the evidence bundle, citation, and
// admission-decision docs plus the documentation facts/findings docs in
// routes.go, the documentation findings count/inventory doc
// (documentation_finding_aggregate.go), the incident-context path doc
// (incident_context.go), the investigation-workflow and investigation-packet
// docs (investigation_workflows.go and investigations.go), the
// visualization-packet doc (visualization_packets.go), and the evidence
// work-item count doc (work_item.go).
//
// Each file exports one JSON string constant that openapi/spec.go
// concatenates, in order, into the published OpenAPI document. This package
// MUST NOT import the parent openapi package: spec.go imports this package,
// so the reverse direction is an import cycle. It may import openapi/schema
// for a shared inline schema fragment.
package evidence
