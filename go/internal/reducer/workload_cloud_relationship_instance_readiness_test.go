// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/workloadinstance"
)

// fakeWorkloadInstanceExistence reports the anchors in existing as
// materialized and records every lookup.
type fakeWorkloadInstanceExistence struct {
	existing map[workloadinstance.Anchor]struct{}
	err      error
	calls    [][]workloadinstance.Anchor
}

func (f *fakeWorkloadInstanceExistence) ExistingAnchors(
	_ context.Context,
	anchors []workloadinstance.Anchor,
) (map[workloadinstance.Anchor]struct{}, error) {
	f.calls = append(f.calls, slices.Clone(anchors))
	if f.err != nil {
		return nil, f.err
	}
	return f.existing, nil
}

func anchorRoleEnvelope(factID, environment string) facts.Envelope {
	return workloadCloudAWSResourceEnvelope(factID, map[string]any{
		"arn":                 "arn:aws:iam::123456789012:role/app-role",
		"resource_id":         "arn:aws:iam::123456789012:role/app-role",
		"resource_type":       "aws_iam_role",
		"name":                "app-role",
		"account_id":          "123456789012",
		"region":              "us-east-1",
		"service_kind":        "iam",
		"correlation_anchors": []any{"arn:aws:iam::123456789012:role/app-role"},
		"workload_id":         "workload:app",
		"environment":         environment,
		"attributes":          map[string]any{},
	})
}

func instanceReadinessHandler(
	lookup workloadinstance.ExistenceLookup,
	writer *recordingWorkloadCloudRelationshipWriter,
) WorkloadCloudRelationshipMaterializationHandler {
	return WorkloadCloudRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			anchorRoleEnvelope("fact-prod", "prod"),
			anchorRoleEnvelope("fact-stage", "stage"),
		}},
		EdgeWriter:                writer,
		ReadinessLookup:           readyLookup(true, true),
		WorkloadInstanceExistence: lookup,
	}
}

func instanceReadinessIntent(cycleStartedAt time.Time) Intent {
	intent := workloadCloudRelationshipIntent()
	intent.CycleStartedAt = cycleStartedAt
	return intent
}

var (
	prodAnchor  = workloadinstance.Anchor{WorkloadID: "workload:app", Environment: "prod"}
	stageAnchor = workloadinstance.Anchor{WorkloadID: "workload:app", Environment: "stage"}
)

// TestWorkloadCloudRelationshipDefersUntilWorkloadInstanceExists is the #6785
// USES race fix: a resource anchored to a WorkloadInstance that has not
// materialized yet must defer with a retryable readiness class, not succeed as
// a MATCH no-op nothing would ever re-run. Once the instances exist, the same
// intent writes both edges.
func TestWorkloadCloudRelationshipDefersUntilWorkloadInstanceExists(t *testing.T) {
	t.Parallel()

	t.Run("instance missing defers without touching the graph", func(t *testing.T) {
		t.Parallel()
		writer := &recordingWorkloadCloudRelationshipWriter{}
		lookup := &fakeWorkloadInstanceExistence{existing: map[workloadinstance.Anchor]struct{}{prodAnchor: {}}}
		_, err := instanceReadinessHandler(lookup, writer).Handle(context.Background(), instanceReadinessIntent(time.Now()))
		if err == nil {
			t.Fatal("Handle() error = nil, want a readiness defer while the stage instance is missing")
		}
		var classified interface {
			Retryable() bool
			FailureClass() string
		}
		if !errors.As(err, &classified) || !classified.Retryable() {
			t.Fatalf("error %v is not retryable", err)
		}
		if got := classified.FailureClass(); got != workloadinstance.NotReadyFailureClass {
			t.Fatalf("failure class = %q, want %q", got, workloadinstance.NotReadyFailureClass)
		}
		if writer.writeCalls != 0 || writer.retractCalls != 0 {
			t.Fatalf("writer touched on defer: writes=%d retracts=%d", writer.writeCalls, writer.retractCalls)
		}
		if len(lookup.calls) != 1 || !slices.Equal(lookup.calls[0], []workloadinstance.Anchor{prodAnchor, stageAnchor}) {
			t.Fatalf("lookup calls = %v, want one call with the sorted distinct anchors", lookup.calls)
		}
	})

	t.Run("instances present writes the edges", func(t *testing.T) {
		t.Parallel()
		writer := &recordingWorkloadCloudRelationshipWriter{}
		lookup := &fakeWorkloadInstanceExistence{existing: map[workloadinstance.Anchor]struct{}{prodAnchor: {}, stageAnchor: {}}}
		result, err := instanceReadinessHandler(lookup, writer).Handle(context.Background(), instanceReadinessIntent(time.Now()))
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(writer.writtenRows) != 2 || result.CanonicalWrites != 2 {
			t.Fatalf("rows = %d writes = %d, want 2 and 2", len(writer.writtenRows), result.CanonicalWrites)
		}
	})
}

// TestWorkloadCloudRelationshipCommitsWhenInstanceWaitIsExhausted proves the
// defer is bounded: an anchor whose instance never materializes stops retrying
// once the repair cycle is older than the bound. The intent then commits, the
// writer's MATCH no-ops the missing anchor, and CanonicalWrites counts only
// the anchors that exist.
func TestWorkloadCloudRelationshipCommitsWhenInstanceWaitIsExhausted(t *testing.T) {
	t.Parallel()
	writer := &recordingWorkloadCloudRelationshipWriter{}
	lookup := &fakeWorkloadInstanceExistence{existing: map[workloadinstance.Anchor]struct{}{prodAnchor: {}}}
	intent := instanceReadinessIntent(time.Now().Add(-workloadinstance.MaxWait - time.Minute))
	result, err := instanceReadinessHandler(lookup, writer).Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v, want commit past the bound", err)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1 (only the materialized prod anchor)", result.CanonicalWrites)
	}
}

// TestWorkloadCloudRelationshipZeroCycleAnchorKeepsDeferring proves an intent
// with no cycle timestamp is treated as "elapsed time unknown", which keeps
// deferring rather than reading as infinitely elapsed and committing at once.
func TestWorkloadCloudRelationshipZeroCycleAnchorKeepsDeferring(t *testing.T) {
	t.Parallel()
	lookup := &fakeWorkloadInstanceExistence{existing: map[workloadinstance.Anchor]struct{}{}}
	intent := instanceReadinessIntent(time.Time{})
	intent.EnqueuedAt = time.Time{}
	_, err := instanceReadinessHandler(lookup, &recordingWorkloadCloudRelationshipWriter{}).Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() error = nil, want a defer with an unknown cycle anchor")
	}
}

// TestWorkloadCloudRelationshipInstanceLookupErrorIsNotAReadinessMiss proves a
// graph read failure is an ordinary error, never the non-counting readiness
// class that could retry forever unseen.
func TestWorkloadCloudRelationshipInstanceLookupErrorIsNotAReadinessMiss(t *testing.T) {
	t.Parallel()
	lookup := &fakeWorkloadInstanceExistence{err: errors.New("graph unavailable")}
	_, err := instanceReadinessHandler(lookup, &recordingWorkloadCloudRelationshipWriter{}).Handle(context.Background(), instanceReadinessIntent(time.Now()))
	if err == nil {
		t.Fatal("Handle() error = nil, want the lookup error")
	}
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) && classified.FailureClass() == workloadinstance.NotReadyFailureClass {
		t.Fatalf("lookup error classified as readiness miss: %v", err)
	}
}

// TestWorkloadCloudRelationshipSkipsInstanceLookupWithoutAnchors proves a
// scope with no workload-anchored resource costs no graph read.
func TestWorkloadCloudRelationshipSkipsInstanceLookupWithoutAnchors(t *testing.T) {
	t.Parallel()
	lookup := &fakeWorkloadInstanceExistence{}
	handler := WorkloadCloudRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			workloadCloudAWSResourceEnvelope("fact-bucket", map[string]any{
				"arn": "arn:aws:s3:::receipts", "resource_id": "receipts", "resource_type": "aws_s3_bucket",
				"account_id": "123456789012", "region": "us-east-1", "attributes": map[string]any{},
			}),
		}},
		EdgeWriter:                &recordingWorkloadCloudRelationshipWriter{},
		ReadinessLookup:           readyLookup(true, true),
		WorkloadInstanceExistence: lookup,
	}
	if _, err := handler.Handle(context.Background(), instanceReadinessIntent(time.Now())); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(lookup.calls) != 0 {
		t.Fatalf("lookup calls = %d, want 0", len(lookup.calls))
	}
}
