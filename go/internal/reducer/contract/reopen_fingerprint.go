// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"context"
	"time"
)

// ResolvedReadFingerprint identifies the resolved input one corpus-fenced
// by-repos read returned (#8283). RepoIDs is the normalized repository set
// the read asked for. Value is computed by the store in the same SQL
// statement as the rows and the fence verdict, so it describes exactly the
// rows the reducer read; callers treat it as opaque and never hash or
// compare it in Go. An empty RepoIDs means the run read no by-repos input.
type ResolvedReadFingerprint struct {
	RepoIDs []string
	Value   string
}

// DeployableUnitReopenFingerprintRecorder stores the ResolvedReadFingerprint
// of a deployable_unit_correlation run that is about to succeed, so the
// deferred-maintenance reopen can leave the item succeeded while its
// resolved input is unchanged (#8283). The write is fenced on the exact claim
// (workItemID plus claimedAt, the claim's last_attempt_at) and must return an
// error wrapping ErrExecutionClaimRejected when that claim no longer holds
// the item.
type DeployableUnitReopenFingerprintRecorder interface {
	RecordDeployableUnitReopenFingerprint(
		ctx context.Context,
		workItemID string,
		claimedAt time.Time,
		fingerprint ResolvedReadFingerprint,
	) error
}
