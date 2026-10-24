// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import "time"

// UnreadRepository is one allowlisted repository of an organization target
// whose own read did not complete, with the closed collection-incomplete
// reason the collector assigned to it. The reason is a fixed token, never
// provider text.
type UnreadRepository struct {
	Repository string
	Reason     string
}

// TargetReadState is the collector-owned record of one organization target's
// read for one committed generation (#8943). It lets the read path tell an
// empty answer for a repository the collector read fully from an empty answer
// for a repository it did not read. The collector builds it and the storage
// layer persists it in the same transaction as the generation; this package
// holds only the value, never a database handle.
type TargetReadState struct {
	GenerationID string
	ScopeID      string
	// OrgReadTruncated reports whether the organization stream stopped at the
	// max_pages cap.
	OrgReadTruncated bool
	// RepositoriesTotal is the number of allowlisted repositories of the
	// organization the target covers.
	RepositoriesTotal int
	// Unread lists the repositories whose own read did not complete. A
	// repository the fan-out read completely is absent.
	Unread     []UnreadRepository
	ObservedAt time.Time
}
