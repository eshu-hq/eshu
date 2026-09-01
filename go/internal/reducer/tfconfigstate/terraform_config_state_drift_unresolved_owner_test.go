// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfconfigstate

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// TestDriftHandlerNoOwnerWritesDurableUnresolvedFinding proves the no-owner
// rejection (tfstatebackend.ErrNoConfigRepoOwnsBackend) is recorded as one
// durable, provenance-only finding (outcome "unresolved") — not silently
// dropped to a log line only, the same durability guarantee the ambiguous
// path already has. Issue #5594 follow-up: before this, a scope whose
// backend never resolved to any config repo (the ordinary bare
// `backend "local" {}` case that started this issue, and the pre-existing
// no_config_repo_owns_backend class generally) looked identical at the read
// surface to a scope that resolved cleanly and simply had no drift — both
// returned zero findings.
func TestDriftHandlerNoOwnerWritesDurableUnresolvedFinding(t *testing.T) {
	t.Parallel()

	writer := &stubDriftWriter{}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{}), // empty rows -> ErrNoConfigRepoOwnsBackend
		EvidenceLoader: &stubDriftLoader{},
		Writer:         writer,
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v", err)
	}
	if res.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded (unresolved ownership is operator-actionable, not fatal)", res.Status)
	}

	if len(writer.writes) != 1 {
		t.Fatalf("len(writer.writes) = %d, want 1", len(writer.writes))
	}
	write := writer.writes[0]
	if !write.UnresolvedOwner {
		t.Fatal("write.UnresolvedOwner = false, want true")
	}
	if len(write.Candidates) != 0 || len(write.AmbiguousOwners) != 0 {
		t.Fatalf("write.Candidates/AmbiguousOwners = %d/%d, want both 0 for an unresolved write",
			len(write.Candidates), len(write.AmbiguousOwners))
	}
}

// TestDriftHandlerAmbiguousOwnerStillWritesAmbiguousNotUnresolved is the
// regression guard proving the new unresolved path does not swallow or
// relabel the pre-existing ambiguous path: an ambiguous rejection must still
// produce an AmbiguousOwners write, never UnresolvedOwner.
func TestDriftHandlerAmbiguousOwnerStillWritesAmbiguousNotUnresolved(t *testing.T) {
	t.Parallel()

	rows := []tfstatebackend.TerraformBackendRow{
		{RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa", BackendKind: "s3", LocatorHash: "hash-1"},
		{RepoID: "repo-b", ScopeID: "repo:repo-b@1", CommitID: "bbb", BackendKind: "s3", LocatorHash: "hash-1"},
	}
	writer := &stubDriftWriter{}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{rows: rows}),
		EvidenceLoader: &stubDriftLoader{},
		Writer:         writer,
	}
	if _, err := h.Handle(context.Background(), validIntent()); err != nil {
		t.Fatalf("Handle() err = %v", err)
	}

	if len(writer.writes) != 1 {
		t.Fatalf("len(writer.writes) = %d, want 1", len(writer.writes))
	}
	write := writer.writes[0]
	if write.UnresolvedOwner {
		t.Fatal("write.UnresolvedOwner = true, want false for an ambiguous rejection")
	}
	if len(write.AmbiguousOwners) != 2 {
		t.Fatalf("len(write.AmbiguousOwners) = %d, want 2", len(write.AmbiguousOwners))
	}
}

// TestDriftHandlerNoOwnerWriteFailureIsRetriable proves a durability write
// failure on the unresolved path turns Handle() into a retriable error, so
// the reducer queue's existing retry/backoff and dead-letter policy re-runs
// the intent instead of the finding being silently lost for this generation
// with nothing left to repair it. This deliberately diverges from
// writeAmbiguousOwner's swallow-on-failure design (unchanged, see
// TestDriftHandlerAmbiguousOwnerWriteFailureStaysNonFatal): a transient
// Postgres error here has no other recovery path at all -- unlike a
// resolvable scope, which self-heals via the next apply's new
// state_snapshot generation, a permanently-unresolved backend's state never
// produces another generation to retry against, so losing this one write is
// permanent, not "eventually corrected." Review finding on #5594 (P1):
// before this, the pre-existing swallow-and-log pattern was copied onto the
// unresolved path without weighing that difference; this handler's own
// "exact"-outcome write path already treats the identical writer failure as
// fatal (Handle() -> reducercontract.Result{}, fmt.Errorf(...)) precisely so the queue
// retries it, so this restores that same treatment for consistency instead
// of leaving the unresolved path as the only durability-losing branch.
// The failure counter still increments -- telemetry is not the recovery
// mechanism here, retry is, but the counter remains valuable for operators
// watching write-failure rate independent of whether retry eventually
// succeeds.
func TestDriftHandlerNoOwnerWriteFailureIsRetriable(t *testing.T) {
	t.Parallel()

	inst, reader := newDriftInstruments(t)
	writer := &stubDriftWriter{err: errors.New("db unavailable")}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{}),
		EvidenceLoader: &stubDriftLoader{},
		Writer:         writer,
		Instruments:    inst,
	}
	_, err := h.Handle(context.Background(), validIntent())
	if err == nil {
		t.Fatal("Handle() err = nil, want non-nil so the queue retries this intent")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_drift_unresolved_owner_write_failed_total"); got != 1 {
		t.Fatalf("eshu_dp_drift_unresolved_owner_write_failed_total = %d, want 1", got)
	}
}

// TestDriftHandlerNoOwnerWriteSuccessDoesNotIncrementFailureCounter proves the
// counter is failure-specific, not incremented on every unresolved write
// attempt.
func TestDriftHandlerNoOwnerWriteSuccessDoesNotIncrementFailureCounter(t *testing.T) {
	t.Parallel()

	inst, reader := newDriftInstruments(t)
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{}),
		EvidenceLoader: &stubDriftLoader{},
		Writer:         &stubDriftWriter{},
		Instruments:    inst,
	}
	if _, err := h.Handle(context.Background(), validIntent()); err != nil {
		t.Fatalf("Handle() err = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_drift_unresolved_owner_write_failed_total"); got != 0 {
		t.Fatalf("eshu_dp_drift_unresolved_owner_write_failed_total = %d, want 0 on a successful write", got)
	}
}
