// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

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
	if res.Status != ResultStatusSucceeded {
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

// TestDriftHandlerNoOwnerWriteFailureStaysNonFatal proves a durability write
// failure on the unresolved path does not turn an already operator-actionable,
// non-fatal rejection into a retriable Handle() error, mirroring
// TestDriftHandlerAmbiguousOwnerWriteFailureStaysNonFatal. It also proves the
// failure is not invisible to metrics:
// eshu_dp_drift_unresolved_owner_write_failed_total increments once.
func TestDriftHandlerNoOwnerWriteFailureStaysNonFatal(t *testing.T) {
	t.Parallel()

	inst, reader := newDriftInstruments(t)
	writer := &stubDriftWriter{err: errors.New("db unavailable")}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{}),
		EvidenceLoader: &stubDriftLoader{},
		Writer:         writer,
		Instruments:    inst,
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v, want nil even when the durable write fails", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
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
