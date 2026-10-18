// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadid

import (
	"strings"
)

// WorkloadHandlePrefix marks the public workload handle. A handle is
// workload:<name>; it is not a node key, and every surface that accepts one
// resolves it server-side through the handle ladder (compatibility doc
// section 1.4): exact id first, then the stripped bare name within the
// caller's grant, failing closed on ambiguity.
const WorkloadHandlePrefix = "workload:"

// SelectorKind classifies a workload selector string for the handle ladder:
// a full scoped node id, a public handle, or anything else (a bare name or
// input no ladder rung claims).
type SelectorKind string

const (
	// SelectorKindBare covers bare names and any other input: the ladder
	// treats it as a name, never as an id or handle.
	SelectorKindBare SelectorKind = "bare"
	// SelectorKindHandle is workload:<name> with a non-empty,
	// colon-free bare name.
	SelectorKindHandle SelectorKind = "handle"
	// SelectorKindFullID is workload:<repo_id>:<name>: the scoped node key.
	SelectorKindFullID SelectorKind = "full_id"
)

// ClassifyWorkloadSelector reports which ladder rung a selector string
// belongs to. A workload: value whose remainder still holds a colon carries
// the repository segment, so it is the full scoped id; without one it is the
// public handle. A bare prefix, a blank string, or any other scheme is bare:
// callers must not read those as handles.
func ClassifyWorkloadSelector(selector string) SelectorKind {
	trimmed := strings.TrimSpace(selector)
	rest, ok := strings.CutPrefix(trimmed, WorkloadHandlePrefix)
	if !ok || strings.TrimSpace(rest) == "" {
		return SelectorKindBare
	}
	if strings.Contains(rest, ":") {
		return SelectorKindFullID
	}
	return SelectorKindHandle
}

// StripWorkloadHandlePrefix returns the bare name a handle rung matches on.
// Only a classified handle loses its prefix; every other input — including a
// full scoped id — comes back as the trimmed string it went in as, so a
// caller that strips without classifying cannot mint a phantom name from an
// id. A bare prefix with nothing after it strips to empty.
func StripWorkloadHandlePrefix(selector string) string {
	trimmed := strings.TrimSpace(selector)
	if rest, ok := strings.CutPrefix(trimmed, WorkloadHandlePrefix); ok && strings.TrimSpace(rest) == "" {
		return ""
	}
	if ClassifyWorkloadSelector(trimmed) != SelectorKindHandle {
		return trimmed
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, WorkloadHandlePrefix))
}

// FormatWorkloadHandle builds the public handle for a workload name: one
// prefix, trimmed, never blank. The handle is what identity-only fallbacks
// emit beside the node id so existing clients keep a stable display string.
func FormatWorkloadHandle(workloadName string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(workloadName), WorkloadHandlePrefix))
	if trimmed == "" {
		return ""
	}
	return WorkloadHandlePrefix + trimmed
}
