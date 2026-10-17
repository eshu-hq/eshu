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

// EmptyResolvedReadFingerprint is the fingerprint value of a read that
// selected no resolved row, and of a run that read no by-repos input at all
// (a deployable_unit_correlation item with no workload candidate). The store's
// SQL coalesces an empty row set to this exact value, so it is never NULL
// and compares equal to itself on both sides of the reopen gate.
const EmptyResolvedReadFingerprint = "empty-resolved-input"

// DeployableUnitCorrelationLogicVersion is the version of the
// deployable_unit_correlation handler logic a recorded fingerprint was taken
// under (#8283). The reopen gate re-opens every item whose recorded version
// differs, so any change that can produce a different graph or admission
// decision from the same facts and resolved input MUST bump it: the handler
// and the code path its output passes through, which the doc comment on
// reducer.DeployableUnitCorrelationHandler lists. A change that cannot
// (logging, telemetry, refactors proven output-identical) must not.
const DeployableUnitCorrelationLogicVersion = 1

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
