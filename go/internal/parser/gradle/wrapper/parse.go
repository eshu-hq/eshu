// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wrapper

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// Parse reads one gradle-wrapper.properties and returns the parent parser
// payload: the standard buckets, a "variables" bucket holding the distribution
// row when the grammar matched, and a StateKey envelope. The payload never
// carries the file body: options.IndexSource is ignored on purpose, because a
// distributionUrl may hold user:password@ userinfo.
func Parse(path string, isDependency bool, _ shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	result := Scan(source)
	payload := shared.BasePayload(path, LanguageName, isDependency)
	if result.Rows != nil {
		payload["variables"] = result.Rows
	}
	envelope := map[string]any{
		"state":             result.State,
		"unsupported_count": result.Counts.Unsupported,
	}
	if result.ParseError != "" {
		envelope["parse_error"] = result.ParseError
	}
	payload[StateKey] = envelope
	return payload, nil
}
