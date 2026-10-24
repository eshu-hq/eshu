// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// StateKey is the payload key that carries the scan state and counts, so a
// reader can tell an empty catalog from one the parser could not read.
const StateKey = "gradle_catalog_state"

// Parse reads one libs.versions.toml and returns the parent parser payload:
// the standard buckets, a "variables" bucket holding the rows, and a
// StateKey envelope. A malformed, oversize or too-deep file returns a payload
// with no rows and the reason in the envelope, never an error.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	result := Scan(source)

	payload := shared.BasePayload(path, LanguageName, isDependency)
	if result.Rows != nil {
		payload["variables"] = result.Rows
	}
	payload[StateKey] = stateEnvelope(result)
	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

// stateEnvelope renders a Result's state and counts for the payload.
func stateEnvelope(result Result) map[string]any {
	envelope := map[string]any{
		"state":                 result.State,
		"version_count":         result.Counts.Versions,
		"library_count":         result.Counts.Libraries,
		"plugin_count":          result.Counts.Plugins,
		"bundle_count":          result.Counts.Bundles,
		"resolved_count":        result.Counts.Resolved,
		"partial_count":         result.Counts.Partial,
		"unresolved_count":      result.Counts.Unresolved,
		"unsupported_count":     result.Counts.Unsupported,
		"dropped_count":         result.Counts.Dropped,
		"alias_collision_count": result.Counts.Collided,
	}
	if result.ParseError != "" {
		envelope["parse_error"] = result.ParseError
	}
	return envelope
}
