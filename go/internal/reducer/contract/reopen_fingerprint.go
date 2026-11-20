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

// AWSDriftReopenSnapshot is the ARN snapshot an aws_cloud_runtime_drift run
// read (#10878, first domain). ARNIDs and ARNPayloadHashes are parallel
// arrays in ascending ARN order; Fingerprint is
// ComputeARNInputFingerprint over the snapshot map. The next run diffs the
// recorded snapshot against its fresh load to narrow to changed ARNs.
type AWSDriftReopenSnapshot struct {
	ScopeID          string
	GenerationID     string
	ARNIDs           []string
	ARNPayloadHashes []string
	Fingerprint      string
}

// AWSDriftReopenFingerprintRecorder stores the AWSDriftReopenSnapshot of an
// aws_cloud_runtime_drift run that is about to succeed, so the
// deferred-maintenance reopen can leave the item succeeded while its ARN
// input is unchanged (#10878). The write is fenced on the exact claim
// (workItemID plus claimedAt, the claim's last_attempt_at) and must return
// an error wrapping ErrExecutionClaimRejected when that claim no longer
// holds the item. An unclaimed run records nothing.
//
// fullRun distinguishes a complete re-derivation (audit_due, fallback, or
// first run: each satisfies the daily audit purpose) from a narrowed delta:
// a full run advances the row's audit clock (last_audit_at) to this run,
// while a delta preserves the existing clock under the same claimed_at
// fence, so hourly deltas never defer the daily audit (#11018 P1).
type AWSDriftReopenFingerprintRecorder interface {
	RecordAWSDriftReopenFingerprint(
		ctx context.Context,
		workItemID string,
		claimedAt time.Time,
		snapshot AWSDriftReopenSnapshot,
		fullRun bool,
	) error
}

// ContainerIdentityReopenEpoch is the identity epoch a
// container_image_identity run read (#10878, first domain): the count, max
// observed_at, and ordered-set SHA-256 the probe query already returns for
// the active identity set. MaxObservedAt is nil on an empty set; the probe
// scans NULL to the zero time so the empty-set epoch stays stable and
// cacheable (#8607).
type ContainerIdentityReopenEpoch struct {
	EpochCount         int64
	EpochMaxObservedAt *time.Time
	EpochFingerprint   string
}

// ContainerIdentityReopenFingerprintRecorder stores the
// ContainerIdentityReopenEpoch of a container_image_identity run that is
// about to succeed, so the deferred-maintenance reopen can leave the item
// succeeded while the fleet-wide identity epoch is unchanged (#10878). The
// write is fenced on the exact claim (workItemID plus claimedAt, the
// claim's last_attempt_at) and must return an error wrapping
// ErrExecutionClaimRejected when that claim no longer holds the item. An
// unclaimed run records nothing.
type ContainerIdentityReopenFingerprintRecorder interface {
	RecordContainerIdentityReopenEpoch(
		ctx context.Context,
		workItemID string,
		claimedAt time.Time,
		epoch ContainerIdentityReopenEpoch,
	) error
}

// WorkloadMaterializationLogicVersion is declared in change_signal.go
// (#10878, same value and bump contract); it is not redeclared here.
// ServiceCatalogCorrelationLogicVersion is the version of the
// service_catalog_correlation handler logic a recorded fingerprint was taken
// under (#10877). Same bump rule as WorkloadMaterializationLogicVersion.
const ServiceCatalogCorrelationLogicVersion = 1

// WorkloadMaterializationReopenFingerprintRecorder stores the
// ResolvedReadFingerprint of a workload_materialization run that is about to
// succeed, so the deferred-maintenance reopen can restart the item when a
// late-arriving foreign scope changes its resolved input (#10877). Same
// claim-fenced write contract as
// DeployableUnitReopenFingerprintRecorder.
type WorkloadMaterializationReopenFingerprintRecorder interface {
	RecordWorkloadMaterializationReopenFingerprint(
		ctx context.Context,
		workItemID string,
		claimedAt time.Time,
		fingerprint ResolvedReadFingerprint,
	) error
}

// ServiceCatalogCorrelationReopenFingerprintRecorder stores the
// ResolvedReadFingerprint of a service_catalog_correlation run that is about
// to succeed (#10877). Same claim-fenced write contract as
// DeployableUnitReopenFingerprintRecorder.
type ServiceCatalogCorrelationReopenFingerprintRecorder interface {
	RecordServiceCatalogCorrelationReopenFingerprint(
		ctx context.Context,
		workItemID string,
		claimedAt time.Time,
		fingerprint ResolvedReadFingerprint,
	) error
}
