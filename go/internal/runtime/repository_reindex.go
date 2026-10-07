// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "time"

// RepositoryReindexRequest is one per-repository reindex watermark (#7620):
// the git default-branch repository scope a request named and the stored
// database-clock time of its newest request. Git ingesters force a full
// re-parse of the scope while its newest activated full generation predates
// RequestedAt; nothing claims or completes the request.
type RepositoryReindexRequest struct {
	ScopeID     string
	RequestedAt time.Time
}
