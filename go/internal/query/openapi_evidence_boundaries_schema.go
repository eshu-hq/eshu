// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIEvidenceBoundariesSchema is the shared OpenAPI schema fragment for
// the evidence_boundaries response field emitted by attachEvidenceBoundaries
// (evidence_boundaries.go). Field names match the PostgresOnlyBoundary struct
// tags exactly (domain, read_surface, reason) so the documented contract
// stays in lockstep with the Go type actually serialized onto the wire.
const openAPIEvidenceBoundariesSchema = `{
  "type": "array",
  "description": "Static disclosure of Postgres-only reducer domains this read surface omits from its graph-sourced sections. Absent when no boundary applies to the surface.",
  "items": {
    "type": "object",
    "properties": {
      "domain": {"type": "string", "description": "Postgres-only reducer domain name, e.g. ci_cd_run_correlation or container_image_identity."},
      "read_surface": {"type": "string", "description": "The read surface this boundary applies to, matching the current route."},
      "reason": {"type": "string", "description": "Machine-readable boundary reason.", "enum": ["postgres_only_read_model"]}
    }
  }
}`
