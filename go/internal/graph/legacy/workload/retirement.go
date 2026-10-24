// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workload

import "errors"

// The retirement contract types live here, beside the detection, so the
// runtime admin route that decodes and answers POST
// /admin/retire-legacy-workloads and the reducer runner that serves it share
// one definition without the runtime package importing the reducer.

var (
	// ErrInvalidRequest wraps a caller-fixable retirement request problem;
	// its text is safe to return to the caller (HTTP 422).
	ErrInvalidRequest = errors.New("invalid legacy workload retirement request")
	// ErrBackendUnvalidated reports that the configured graph backend is not
	// the one the detection and the delete were tested on (Neo4j). Both dry
	// runs and execute are refused, so an untested answer is never served
	// (HTTP 501).
	ErrBackendUnvalidated = errors.New("legacy workload retirement is validated on Neo4j only; dry runs and execute are refused on this graph backend")
	// ErrWriterUnavailable reports that the backend is validated but the
	// process has no graph writer wired, a deployment fault. Execute is
	// refused; dry runs still read (HTTP 503).
	ErrWriterUnavailable = errors.New("legacy workload retirement has no graph writer configured; execute is refused")
)

// RetireRequest is one retirement request.
type RetireRequest struct {
	DryRun bool
	// Limit bounds the eligible nodes returned or deleted; zero means
	// MaxBatch.
	Limit int
	// IDs optionally restricts the request to these node ids.
	IDs []string
	// Reason is required when DryRun is false and goes to the log line.
	Reason string
}

// RetireCandidate is one name-only Workload node in a response.
type RetireCandidate struct {
	ID            string         `json:"id"`
	TwinID        string         `json:"twin_id"`
	RepoID        string         `json:"repo_id"`
	Name          string         `json:"name"`
	Relationships map[string]int `json:"relationships"`
	Eligible      bool           `json:"eligible"`
	BlockedReason string         `json:"blocked_reason"`
}

// RetireResponse is the result of a retirement request, and the route's
// response body.
type RetireResponse struct {
	DryRun     bool              `json:"dry_run"`
	Candidates []RetireCandidate `json:"candidates"`
	Eligible   int               `json:"eligible"`
	Blocked    int               `json:"blocked"`
	Deleted    int               `json:"deleted"`
	// Truncated is true when the request stopped before the label was
	// exhausted, so more candidates may remain.
	Truncated bool `json:"truncated"`
}
