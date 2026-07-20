// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudformation extracts CloudFormation and SAM template evidence for
// parser adapters.
//
// IsTemplate recognizes bounded CloudFormation JSON or YAML documents. Parse
// returns deterministic resource, parameter, output, condition, import, and
// export buckets that parent JSON and YAML adapters can attach to their
// payloads. Condition evaluation is intentionally limited to simple literal and
// parameter-default expressions, and unresolved values stay unevaluated instead
// of inventing deployment truth.
//
// ParseWithPositions extracts the same buckets as Parse, but stamps each
// Parameters/Conditions/Resources/Outputs entity (and each entity's Export)
// with its own real line_number and end_line from a caller-measured Positions
// value, instead of the single document-root lineNumber every entity gets
// from Parse. Positions and its SectionPositions/EntityPosition fields exist
// so a caller with real per-entity source positions -- the YAML adapter via a
// gopkg.in/yaml.v3 node walk (issue #5328) and the JSON adapter via an
// ordered-entry walk (issue #5348) -- can report them. Parse itself is defined
// as ParseWithPositions called with a zero Positions, so the two never drift.
package cloudformation
