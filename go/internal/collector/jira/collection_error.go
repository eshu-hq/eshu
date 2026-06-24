// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import "strings"

// PartialCollectionError preserves bounded collection counters when a Jira
// fetch fails after accepting part of a page set.
type PartialCollectionError struct {
	Stage string
	Stats CollectionStats
	Cause error
}

// Error returns a bounded failure string safe for logs and status.
func (e PartialCollectionError) Error() string {
	stage := strings.TrimSpace(e.Stage)
	if stage == "" {
		stage = "collection"
	}
	return "jira partial collection failure: " + stage
}

// Unwrap returns the underlying provider or transport error.
func (e PartialCollectionError) Unwrap() error {
	return e.Cause
}
