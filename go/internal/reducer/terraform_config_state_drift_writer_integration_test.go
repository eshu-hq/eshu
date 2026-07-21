// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// This file holds the durable-write integration tests for issue #5442, split
// out of terraform_config_state_drift_test.go to keep that file under the
// repository's 500-line file cap.

// stubDriftWriter captures every WriteTerraformConfigStateDriftFindings call
// so tests can assert exactly what the handler durably publishes, independent
// of the parallel counter/log signal.
type stubDriftWriter struct {
	writes []TerraformConfigStateDriftWrite
	err    error
}

func (s *stubDriftWriter) WriteTerraformConfigStateDriftFindings(
	_ context.Context, write TerraformConfigStateDriftWrite,
) (TerraformConfigStateDriftWriteResult, error) {
	s.writes = append(s.writes, write)
	if s.err != nil {
		return TerraformConfigStateDriftWriteResult{}, s.err
	}
	return TerraformConfigStateDriftWriteResult{
		CanonicalIDs:    []string{"canonical:stub"},
		CanonicalWrites: len(write.Candidates) + len(write.AmbiguousOwners),
	}, nil
}

// TestDriftHandlerWritesDurableFindingForAllFiveDriftKinds proves the
// durability half of issue #5442: the handler now persists one "exact"
// finding per admitted candidate through Writer, in addition to (not instead
// of) the counters TestDriftHandlerSingleOwnerEmitsCountersForAllFiveDrift
// Kinds already pins.
func TestDriftHandlerWritesDurableFindingForAllFiveDriftKinds(t *testing.T) {
	t.Parallel()

	inst, reader := newDriftInstruments(t)
	now := time.Now()
	backendRows := []tfstatebackend.TerraformBackendRow{
		{
			RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa",
			CommitObservedAt: now, BackendKind: "s3", LocatorHash: "hash-1",
		},
	}
	driftRows := []tfconfigstate.AddressedRow{
		{
			Address: "aws_s3_bucket.added_state", ResourceType: "aws_s3_bucket",
			State: &tfconfigstate.ResourceRow{Address: "aws_s3_bucket.added_state", ResourceType: "aws_s3_bucket"},
		},
		{
			Address: "aws_iam_role.added_config", ResourceType: "aws_iam_role",
			Config: &tfconfigstate.ResourceRow{Address: "aws_iam_role.added_config", ResourceType: "aws_iam_role"},
		},
		{
			Address: "aws_s3_bucket.attr_drift", ResourceType: "aws_s3_bucket",
			Config: &tfconfigstate.ResourceRow{
				Address: "aws_s3_bucket.attr_drift", ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{"versioning.enabled": "true"},
			},
			State: &tfconfigstate.ResourceRow{
				Address: "aws_s3_bucket.attr_drift", ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{"versioning.enabled": "false"},
			},
		},
		{
			Address: "aws_lambda_function.removed_state", ResourceType: "aws_lambda_function",
			Config: &tfconfigstate.ResourceRow{Address: "aws_lambda_function.removed_state", ResourceType: "aws_lambda_function"},
			Prior:  &tfconfigstate.ResourceRow{Address: "aws_lambda_function.removed_state", ResourceType: "aws_lambda_function"},
		},
		{
			Address: "aws_iam_policy.removed_config", ResourceType: "aws_iam_policy",
			State: &tfconfigstate.ResourceRow{
				Address: "aws_iam_policy.removed_config", ResourceType: "aws_iam_policy",
				PreviouslyDeclaredInConfig: true,
			},
		},
	}

	writer := &stubDriftWriter{}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{rows: backendRows}),
		EvidenceLoader: &stubDriftLoader{rows: driftRows},
		Instruments:    inst,
		Writer:         writer,
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}
	if res.CanonicalWrites != 5 {
		t.Fatalf("res.CanonicalWrites = %d, want 5", res.CanonicalWrites)
	}

	// Counters remain a parallel signal: the durable write does not replace
	// eshu_dp_correlation_drift_detected_total.
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_correlation_drift_detected_total"); got != 5 {
		t.Fatalf("drift_detected = %d, want 5 (counters must stay parallel to the write)", got)
	}

	if len(writer.writes) != 1 {
		t.Fatalf("len(writer.writes) = %d, want 1 (one WriteTerraformConfigStateDriftFindings call carrying all admitted candidates)", len(writer.writes))
	}
	write := writer.writes[0]
	if len(write.Candidates) != 5 {
		t.Fatalf("len(write.Candidates) = %d, want 5", len(write.Candidates))
	}
	if len(write.AmbiguousOwners) != 0 {
		t.Fatalf("len(write.AmbiguousOwners) = %d, want 0 for an exact write", len(write.AmbiguousOwners))
	}
	if write.BackendKind != "s3" || write.LocatorHash != "hash-1" {
		t.Fatalf("write.BackendKind/LocatorHash = %q/%q, want s3/hash-1", write.BackendKind, write.LocatorHash)
	}
	gotKinds := map[string]bool{}
	for _, candidate := range write.Candidates {
		gotKinds[readDriftKindAtom(candidate)] = true
	}
	wantKinds := []string{"added_in_state", "added_in_config", "attribute_drift", "removed_from_state", "removed_from_config"}
	for _, k := range wantKinds {
		if !gotKinds[k] {
			t.Fatalf("write.Candidates missing drift kind %q, got %v", k, gotKinds)
		}
	}
}

// TestDriftHandlerAmbiguousOwnerWritesDurableAmbiguousFinding proves the
// ambiguous-owner rejection is recorded as one durable, provenance-only
// finding (outcome "ambiguous") carrying every competing repo's identity —
// not silently dropped to a log line only, and not collapsed into a picked
// winner.
func TestDriftHandlerAmbiguousOwnerWritesDurableAmbiguousFinding(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backendRows := []tfstatebackend.TerraformBackendRow{
		{
			RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa",
			CommitObservedAt: now, BackendKind: "s3", LocatorHash: "hash-1",
		},
		{
			RepoID: "repo-b", ScopeID: "repo:repo-b@1", CommitID: "bbb",
			CommitObservedAt: now, BackendKind: "s3", LocatorHash: "hash-1",
		},
	}
	writer := &stubDriftWriter{}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{rows: backendRows}),
		EvidenceLoader: &stubDriftLoader{},
		Writer:         writer,
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded (ambiguous owner is operator-actionable, not fatal)", res.Status)
	}

	if len(writer.writes) != 1 {
		t.Fatalf("len(writer.writes) = %d, want 1", len(writer.writes))
	}
	write := writer.writes[0]
	if len(write.Candidates) != 0 {
		t.Fatalf("len(write.Candidates) = %d, want 0 for an ambiguous write (no anchor resolved)", len(write.Candidates))
	}
	if len(write.AmbiguousOwners) != 2 {
		t.Fatalf("len(write.AmbiguousOwners) = %d, want 2 (both competing repos, no winner picked)", len(write.AmbiguousOwners))
	}
	gotRepos := map[string]bool{}
	for _, owner := range write.AmbiguousOwners {
		gotRepos[owner.RepoID] = true
	}
	if !gotRepos["repo-a"] || !gotRepos["repo-b"] {
		t.Fatalf("write.AmbiguousOwners repo ids = %v, want repo-a and repo-b both present", gotRepos)
	}
}

// TestDriftHandlerAmbiguousOwnerWriteFailureStaysNonFatal proves a durability
// write failure on the ambiguous path does not turn an already
// operator-actionable, non-fatal rejection into a retriable Handle() error —
// that would create a retry storm for a case the reducer already classifies
// as "succeeded, operator should look at this."
func TestDriftHandlerAmbiguousOwnerWriteFailureStaysNonFatal(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backendRows := []tfstatebackend.TerraformBackendRow{
		{RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa", CommitObservedAt: now, BackendKind: "s3", LocatorHash: "hash-1"},
		{RepoID: "repo-b", ScopeID: "repo:repo-b@1", CommitID: "bbb", CommitObservedAt: now, BackendKind: "s3", LocatorHash: "hash-1"},
	}
	writer := &stubDriftWriter{err: errors.New("db unavailable")}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{rows: backendRows}),
		EvidenceLoader: &stubDriftLoader{},
		Writer:         writer,
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v, want nil even when the durable write fails", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}
}

// TestDriftHandlerWriteFailureFailsIntent proves that on the normal
// (non-ambiguous) path a durability write failure DOES surface as a Handle()
// error, so the reducer retries the intent rather than silently losing
// admitted candidates that were never persisted.
func TestDriftHandlerWriteFailureFailsIntent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backendRows := []tfstatebackend.TerraformBackendRow{{
		RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa",
		CommitObservedAt: now, BackendKind: "s3", LocatorHash: "hash-1",
	}}
	driftRows := []tfconfigstate.AddressedRow{{
		Address: "aws_iam_role.svc", ResourceType: "aws_iam_role",
		Config: &tfconfigstate.ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
	}}
	writer := &stubDriftWriter{err: errors.New("db unavailable")}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{rows: backendRows}),
		EvidenceLoader: &stubDriftLoader{rows: driftRows},
		Writer:         writer,
	}
	if _, err := h.Handle(context.Background(), validIntent()); err == nil {
		t.Fatal("Handle() error = nil, want non-nil when the durable write fails")
	}
}
