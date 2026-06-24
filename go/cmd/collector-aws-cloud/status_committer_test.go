// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestAWSStatusCommitterRecordsSuccessfulClaimedCommit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 15, 0, 0, 0, time.UTC)
	statusStore := &recordingAWSScanCommitStatusStore{}
	inner := &recordingAWSInnerCommitter{}
	committer := newAWSStatusCommitter(inner, statusStore, "aws-prod", func() time.Time { return now }, nil)

	err := committer.CommitClaimedScopeGeneration(
		context.Background(),
		awsClaimMutation(),
		awsScope(),
		awsGeneration(),
		closedFactStream(),
	)
	if err != nil {
		t.Fatalf("CommitClaimedScopeGeneration() error = %v, want nil", err)
	}
	if inner.claimedCalls != 1 {
		t.Fatalf("inner claimed calls = %d, want 1", inner.claimedCalls)
	}
	if len(statusStore.commits) != 1 {
		t.Fatalf("status commits = %d, want 1", len(statusStore.commits))
	}
	commit := statusStore.commits[0]
	if commit.CommitStatus != awscloud.ScanCommitCommitted {
		t.Fatalf("commit status = %q, want %q", commit.CommitStatus, awscloud.ScanCommitCommitted)
	}
	if commit.Boundary.CollectorInstanceID != "aws-prod" || commit.Boundary.FencingToken != 7 {
		t.Fatalf("boundary = %+v, want collector aws-prod token 7", commit.Boundary)
	}
	if !commit.CompletedAt.Equal(now) {
		t.Fatalf("completed at = %s, want %s", commit.CompletedAt, now)
	}
}

func TestAWSStatusCommitterRecordsFailedCommitAndJoinsStatusError(t *testing.T) {
	t.Parallel()

	commitErr := errors.New("commit failed for arn:aws:sts::123456789012:assumed-role/Admin/session")
	statusErr := errors.New("status write failed")
	statusStore := &recordingAWSScanCommitStatusStore{err: statusErr}
	inner := &recordingAWSInnerCommitter{claimedErr: commitErr}
	committer := newAWSStatusCommitter(inner, statusStore, "aws-prod", nil, nil)

	err := committer.CommitClaimedScopeGeneration(
		context.Background(),
		awsClaimMutation(),
		awsScope(),
		awsGeneration(),
		closedFactStream(),
	)
	if !errors.Is(err, commitErr) || !errors.Is(err, statusErr) {
		t.Fatalf("error = %v, want joined commit and status errors", err)
	}
	if len(statusStore.commits) != 1 {
		t.Fatalf("status commits = %d, want 1", len(statusStore.commits))
	}
	commit := statusStore.commits[0]
	if commit.CommitStatus != awscloud.ScanCommitFailed || commit.FailureClass != "commit_failure" {
		t.Fatalf("failure commit = %+v, want commit failure status", commit)
	}
	if strings.Contains(commit.FailureMessage, "123456789012") ||
		strings.Contains(commit.FailureMessage, "arn:aws") {
		t.Fatalf("failure message = %q, want redacted account and ARN", commit.FailureMessage)
	}
}

func TestAWSStatusCommitterDoesNotRetrySuccessfulCommitForMissingScopeMetadata(t *testing.T) {
	t.Parallel()

	statusStore := &recordingAWSScanCommitStatusStore{}
	inner := &recordingAWSInnerCommitter{}
	scopeValue := awsScope()
	delete(scopeValue.Metadata, "region")
	committer := newAWSStatusCommitter(inner, statusStore, "aws-prod", nil, nil)

	err := committer.CommitClaimedScopeGeneration(
		context.Background(),
		awsClaimMutation(),
		scopeValue,
		awsGeneration(),
		closedFactStream(),
	)
	if err != nil {
		t.Fatalf("CommitClaimedScopeGeneration() error = %v, want nil after durable commit", err)
	}
	if len(statusStore.commits) != 0 {
		t.Fatalf("status commits = %d, want 0 when scope metadata is incomplete", len(statusStore.commits))
	}
}

func TestAWSStatusCommitterDelegatesAllCommitMethods(t *testing.T) {
	t.Parallel()

	inner := &recordingAWSInnerCommitter{}
	committer := newAWSStatusCommitter(inner, nil, "aws-prod", nil, nil)

	if err := committer.CommitScopeGeneration(context.Background(), awsScope(), awsGeneration(), closedFactStream()); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if err := committer.CommitClaimedScopeGeneration(
		context.Background(),
		awsClaimMutation(),
		awsScope(),
		awsGeneration(),
		closedFactStream(),
	); err != nil {
		t.Fatalf("CommitClaimedScopeGeneration() error = %v, want nil", err)
	}
	if err := committer.CommitClaimedScopeGenerationWithStreamError(
		context.Background(),
		awsClaimMutation(),
		awsScope(),
		awsGeneration(),
		closedFactStream(),
		func() error { return nil },
	); err != nil {
		t.Fatalf("CommitClaimedScopeGenerationWithStreamError() error = %v, want nil", err)
	}

	if inner.scopeCalls != 1 || inner.claimedCalls != 1 || inner.streamClaimedCalls != 1 {
		t.Fatalf(
			"inner calls = scope:%d claimed:%d stream:%d, want 1/1/1",
			inner.scopeCalls,
			inner.claimedCalls,
			inner.streamClaimedCalls,
		)
	}
}

// TestAWSStatusCommitterClassifiesStaleFenceCommitAsTerminal proves the
// commit-side path matches the awsruntime start/observe paths added in
// issue #612: when CommitAWSScan is rejected by stale fence, the wrapper
// classifies the failure as terminal and bumps
// eshu_dp_aws_scan_status_stale_fence_total{operation=commit}. Without this,
// a stale-fenced commit would still feed FailClaimRetryable and the same
// runaway loop could resurface through the commit path.
func TestAWSStatusCommitterClassifiesStaleFenceCommitAsTerminal(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() err = %v", err)
	}
	statusStore := &recordingAWSScanCommitStatusStore{err: fmt.Errorf("commit: %w", awscloud.ErrScanStatusStaleFence)}
	inner := &recordingAWSInnerCommitter{}
	committer := newAWSStatusCommitter(inner, statusStore, "aws-prod", nil, instruments)

	err = committer.CommitClaimedScopeGeneration(
		context.Background(),
		awsClaimMutation(),
		awsScope(),
		awsGeneration(),
		closedFactStream(),
	)
	if err == nil {
		t.Fatalf("CommitClaimedScopeGeneration() err = nil, want classified stale fence")
	}
	if !errors.Is(err, awscloud.ErrScanStatusStaleFence) {
		t.Fatalf("CommitClaimedScopeGeneration() err = %v, want errors.Is awscloud.ErrScanStatusStaleFence", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != awsruntime.FailureClassStaleFence {
		got := ""
		if classified != nil {
			got = classified.FailureClass()
		}
		t.Fatalf("FailureClass() = %q, want %q (err=%v)", got, awsruntime.FailureClassStaleFence, err)
	}
	var terminal interface{ TerminalFailure() bool }
	if !errors.As(err, &terminal) || !terminal.TerminalFailure() {
		t.Fatalf("TerminalFailure() = false, want true (err=%v)", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() err = %v", err)
	}
	if got := commitStaleFenceCounterValue(t, rm); got != 1 {
		t.Fatalf("stale fence commit counter = %d, want 1", got)
	}
}

func commitStaleFenceCounterValue(t *testing.T, rm metricdata.ResourceMetrics) int64 {
	t.Helper()
	wantAttrs := map[string]string{
		telemetry.MetricDimensionService:   awscloud.ServiceECR,
		telemetry.MetricDimensionAccount:   "123456789012",
		telemetry.MetricDimensionRegion:    "us-east-1",
		telemetry.MetricDimensionOperation: awsruntime.ScanStatusPhaseCommit,
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != "eshu_dp_aws_scan_status_stale_fence_total" {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Sum[int64]", record.Data)
			}
			for _, dp := range sum.DataPoints {
				if commitStaleFenceAttrsMatch(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("metric eshu_dp_aws_scan_status_stale_fence_total{operation=commit} not found")
	return 0
}

func commitStaleFenceAttrsMatch(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}
	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}
	return true
}

func awsScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID: "aws:123456789012:us-east-1:ecr",
		Metadata: map[string]string{
			"account_id":   "123456789012",
			"region":       "us-east-1",
			"service_kind": awscloud.ServiceECR,
		},
	}
}

func awsGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{GenerationID: "generation-1"}
}

func awsClaimMutation() workflow.ClaimMutation {
	return workflow.ClaimMutation{FencingToken: 7}
}

func closedFactStream() <-chan facts.Envelope {
	ch := make(chan facts.Envelope)
	close(ch)
	return ch
}

type recordingAWSInnerCommitter struct {
	scopeCalls           int
	claimedCalls         int
	streamClaimedCalls   int
	scopeErr             error
	claimedErr           error
	streamClaimedErr     error
	streamClaimedErrSeen error
}

func (c *recordingAWSInnerCommitter) CommitScopeGeneration(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error {
	c.scopeCalls++
	return c.scopeErr
}

func (c *recordingAWSInnerCommitter) CommitClaimedScopeGeneration(
	context.Context,
	workflow.ClaimMutation,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error {
	c.claimedCalls++
	return c.claimedErr
}

func (c *recordingAWSInnerCommitter) CommitClaimedScopeGenerationWithStreamError(
	_ context.Context,
	_ workflow.ClaimMutation,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	c.streamClaimedCalls++
	if factStreamErr != nil {
		c.streamClaimedErrSeen = factStreamErr()
	}
	return c.streamClaimedErr
}

type recordingAWSScanCommitStatusStore struct {
	commits []awscloud.ScanStatusCommit
	err     error
}

func (s *recordingAWSScanCommitStatusStore) CommitAWSScan(
	_ context.Context,
	commit awscloud.ScanStatusCommit,
) error {
	s.commits = append(s.commits, commit)
	return s.err
}
