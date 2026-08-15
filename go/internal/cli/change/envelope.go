// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

import "strings"

// Options is everything the `eshu change impact` and `eshu change plan`
// request bodies are built from. It holds plain values only: go/cmd/eshu
// reads the cobra flags and fills this in, so nothing here depends on cobra
// and every rule below is testable without a command tree.
//
// JSON selects the machine-readable rendering. It is part of Options rather
// than a separate render argument because the same flag decides both what the
// request asks for and how the answer is printed.
type Options struct {
	JSON            bool
	RepoID          string
	DeveloperIntent string
	BaseRef         string
	HeadRef         string
	RepoPath        string
	ChangedPaths    []string
	Changes         []FileChange
	Target          string
	TargetType      string
	ServiceName     string
	WorkloadID      string
	ResourceID      string
	ModuleID        string
	Topic           string
	Environment     string
	MaxDepth        int
	Limit           int
	Offset          int
}

// FileChange is one changed file as the pre-change impact API expects it.
// Status is the normalized word NormalizeStatus produces, not a raw git
// status letter. OldPath is set only for a rename or a copy, where the API
// needs both endpoints to line up evidence across the move.
type FileChange struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Status  string `json:"status"`
}

// Envelope is Eshu's canonical response shape as the change commands consume
// it. Data and Truth stay untyped because both routes return open-ended
// sections the CLI only reads a handful of keys from; decoding them into
// structs would silently drop everything the JSON rendering is expected to
// pass through verbatim.
type Envelope struct {
	Data  map[string]any `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *EnvelopeError `json:"error"`
}

// EnvelopeError is the error member of the canonical envelope. Code is the
// machine-readable classification the exit-code table in go/cmd/eshu keys on;
// Message is operator-facing text that comes either from the API or, for a
// transport failure, from the Go error itself.
type EnvelopeError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// mapValue returns parent[key] when it is a JSON object, and nil otherwise.
//
// This and the four readers below are copies of go/cmd/eshu's trace* helpers,
// not shared with them. The originals still have callers that are not moving
// (component_api.go, contract.go, map.go, trace.go, and trace_render.go), so
// the copies coexist deliberately until a shared home exists. The freshness
// family keeps a third set in go/internal/cli/freshness/values.go for the same
// reason, so an edit here belongs in three places, not two.
// TestEnvelopeReaderParity in go/cmd/eshu compares all three sets and goes red
// when one drifts.
// ErrorCodeFromTransport in failure.go is a copy of the same kind, and the one
// an edit to the originals is most likely to reach; its doc comment names the
// parity test covering all three copies of it.
func mapValue(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	if typed, ok := parent[key].(map[string]any); ok {
		return typed
	}
	return nil
}

// sliceValue returns parent[key] as a slice, accepting both the []any that
// encoding/json produces and the []map[string]any a Go caller may build
// directly.
func sliceValue(parent map[string]any, key string) []any {
	if parent == nil {
		return nil
	}
	switch typed := parent[key].(type) {
	case []any:
		return typed
	case []map[string]any:
		rows := make([]any, 0, len(typed))
		for _, row := range typed {
			rows = append(rows, row)
		}
		return rows
	default:
		return nil
	}
}

// stringValue returns parent[key] trimmed when it is a string, and "" for
// every other shape including a missing key.
func stringValue(parent map[string]any, key string) string {
	if parent == nil {
		return ""
	}
	if value, ok := parent[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// intValue returns parent[key] as an int, accepting the float64 that
// encoding/json produces for every JSON number.
func intValue(parent map[string]any, key string) int {
	if parent == nil {
		return 0
	}
	switch value := parent[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

// boolValue returns parent[key] when it is a bool, and false otherwise. A
// missing key reads as false, which is what the fail-closed checks in
// ClassifyImpact and ClassifyPlan want: an answer that never claimed to be
// truncated is not treated as truncated.
func boolValue(parent map[string]any, key string) bool {
	if parent == nil {
		return false
	}
	if value, ok := parent[key].(bool); ok {
		return value
	}
	return false
}

// freshnessState reads truth.freshness.state, the field both change commands
// fail closed on.
func freshnessState(envelope Envelope) string {
	return stringValue(mapValue(envelope.Truth, "freshness"), "state")
}
