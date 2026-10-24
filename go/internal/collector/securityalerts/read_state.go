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
	// FanoutStartRepository is the allowlisted repository this generation's
	// fan-out started at, or "" when it started at the top of the allowlist
	// (#8946).
	FanoutStartRepository string
	// FanoutNextRepository is the allowlisted repository the next claim's
	// fan-out starts at, or "" for the top. It commits with the generation, so
	// a claim that rolls back, or an unchanged generation that is skipped,
	// leaves the cursor where it was.
	FanoutNextRepository string
	// FanoutLapRepository names the last allowlisted repository when its failed
	// read closed a lap (#8946): the start returned to the top and stays there
	// until a claim reads every repository or that repository leaves the
	// allowlist. Empty means no lap.
	FanoutLapRepository string
}

// FanoutCursor is the committed fan-out position of one org target, read from
// the newest pending-or-active generation's read state (#8946). Both fields are
// lowercase allowlist entries; empty means the top and no lap.
type FanoutCursor struct {
	// NextRepository is the repository the next claim starts at.
	NextRepository string
	// LapRepository is the last-position repository whose failed read closed a
	// lap, while the lap is in progress.
	LapRepository string
}
