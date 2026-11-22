// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

// componentsSourcePacing documents one source_pacing row of the workflow
// coordinator section (#10882 AC6): whether intake from one source system
// is slowed, why, and the claimable reducer observation behind the
// verdict. It is kept in a separate constant fragment to keep
// components.go under the repository's file-size limit, like
// componentsActiveWorkSource. TestOpenAPISourcePacingMatchesTheWireType
// keeps the required keys equal to the JSON the status surface encodes.
const componentsSourcePacing = `      "SourcePacing": {
        "type": "object",
        "description": "One source's per-source reducer backpressure state: whether intake from the source is slowed, why, and the claimable reducer observation behind the verdict. Rows carry only source identity and bounded queue numbers, so the surface stays credential-safe and scoped callers are withheld the whole section like the other process-global aggregates. Stuck marks an engagement older than ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT with the queue still behind: still slowed, and loud.",
        "required": ["source_system", "slowed", "oldest_claimable_age", "oldest_claimable_age_seconds", "claimable_depth"],
        "properties": {
          "source_system": {"type": "string", "description": "The slowed or observed source system (for example git or vuln-intel)."},
          "slowed": {"type": "boolean", "description": "Whether intake from the source is currently slowed."},
          "reason": {"type": "string", "description": "Why intake slowed (source_behind). Absent unless slowed."},
          "oldest_claimable_age": {"type": "string", "description": "Age of the source's oldest claimable outstanding reducer row at read time."},
          "oldest_claimable_age_seconds": {"type": "number", "description": "The same age in seconds."},
          "claimable_depth": {"type": "integer", "description": "Count of the source's claimable outstanding reducer rows at read time."},
          "engaged_since": {"type": "string", "format": "date-time", "description": "When the current slowed engagement began. Absent unless slowed."},
          "engaged_duration": {"type": "string", "description": "How long the engagement has run at read time. Absent unless slowed with a known start."},
          "engaged_duration_seconds": {"type": "number", "description": "The same duration in seconds. Absent unless slowed with a known start."},
          "stuck": {"type": "boolean", "description": "Whether the engagement is older than the configured bound with the queue still behind. Absent unless stuck."}
        }
      },
`
