// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ScanIntervalConfigKey is the optional per-instance configuration field that
// widens one collector instance's scheduled-scan bucket beyond the global
// ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL (#6720). It lives inside the
// instance's free-form configuration object, next to kind-specific fields such
// as scheduled_scan_enabled, so every collector kind reads it the same way and
// no chart schema or Postgres column changes. The coordinator plans with it and
// the status reader sizes the terminal-failure window from it (#8803), so both
// parse it here.
const ScanIntervalConfigKey = "scan_interval"

// ParseScanInterval decodes scan_interval from one collector instance
// configuration document. It reports whether the field was set so a caller can
// tell "unset, use the global interval" from "set to this value". A blank
// string counts as unset, matching how envDuration treats a blank environment
// variable. A valid document that is not a JSON object ([], a scalar, null) has
// no fields at all, so it is unset too rather than an error:
// DesiredCollectorInstance.Validate accepts any valid JSON here and a generic
// or disabled collector may legitimately carry one. Only a document that is an
// object but cannot be decoded is an error. A key that is present with a
// non-string value, null included, is an error rather than unset: a template
// that meant 12h and emitted null would otherwise run on the global cadence
// with no signal.
func ParseScanInterval(raw string) (time.Duration, bool, error) {
	// Decode into a map of raw values so key presence and value type are
	// separable (encoding/json writes a JSON null into a string field as the
	// zero value, indistinguishable from an absent key) and so every other
	// field stays opaque: json.Valid accepts a number such as 1e400 that a
	// float64 decode would reject, and an unrelated field must never make
	// this reader fail.
	var decoded map[string]json.RawMessage
	normalized := strings.TrimSpace(raw)
	if !strings.HasPrefix(normalized, "{") {
		return 0, false, nil
	}
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		return 0, false, fmt.Errorf("decode collector configuration %s: %w", ScanIntervalConfigKey, err)
	}
	rawValue, present := decoded[ScanIntervalConfigKey]
	if !present {
		return 0, false, nil
	}
	return ParseScanIntervalValue(rawValue)
}

// ParseScanIntervalValue decodes the JSON value stored under
// ScanIntervalConfigKey, with the same rules as ParseScanInterval for a
// present key. A nil or empty value means the key was absent and reads as
// unset. The status reader selects configuration->'scan_interval' and parses
// it here, so it never decodes the whole configuration document and never
// parses a Go duration in SQL.
func ParseScanIntervalValue(rawValue json.RawMessage) (time.Duration, bool, error) {
	if len(rawValue) == 0 {
		return 0, false, nil
	}
	// Both halves of this guard are needed: json.Unmarshal of the literal
	// null into a string succeeds silently and leaves "", so only the type
	// check rejects an explicit null. Report the JSON type, never the value:
	// the diagnostic is "wrong type", and echoing an operator-supplied
	// document into an error that lands in startup output and reconcile
	// logs is not worth the risk.
	var text string
	if err := json.Unmarshal(rawValue, &text); err != nil || jsonTypeName(rawValue) != "string" {
		return 0, false, fmt.Errorf("%s must be a duration string, got %s", ScanIntervalConfigKey, jsonTypeName(rawValue))
	}
	value := strings.TrimSpace(text)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %w", ScanIntervalConfigKey, err)
	}
	return parsed, true, nil
}

// jsonTypeName names the JSON type of one raw value from its first byte,
// for error messages that must not echo the value itself. An explicit null in
// a RawMessage map entry arrives as the four-byte literal null and is named by
// the 'n' case; the empty case is defensive only and does not occur for a
// present key.
func jsonTypeName(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "null"
	}
	switch trimmed[0] {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}
