// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceProcessesClaimAndCommitsSourceFacts(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	item.TenantID = "tenant-a"
	item.WorkspaceID = "workspace-a"
	item.SubjectClass = "scanner_worker"
	item.PolicyRevisionHash = "policy-a"
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	committer := &recordingClaimCommitter{}
	analyzer := &recordingAnalyzer{
		result: AnalyzerResult{
			Output: FactOutput{
				TargetCount: 1,
				ResultCount: 1,
				Facts:       []facts.Envelope{testScannerFact(t, item, claim, facts.ScannerWorkerAnalysisFactKind)},
			},
			Usage: ResourceUsage{CPUSeconds: 2.5, PeakMemoryBytes: 256 << 20},
		},
	}
	service := testScannerService(store, committer, analyzer)

	if err := service.processClaimed(context.Background(), item, claim); err != nil {
		t.Fatalf("processClaimed() error = %v, want nil", err)
	}
	if len(analyzer.inputs) != 1 {
		t.Fatalf("analyzer inputs = %d, want 1", len(analyzer.inputs))
	}
	input := analyzer.inputs[0]
	if input.WorkItemID != item.WorkItemID || input.ClaimID != claim.ClaimID {
		t.Fatalf("claim boundary = (%q,%q), want (%q,%q)", input.WorkItemID, input.ClaimID, item.WorkItemID, claim.ClaimID)
	}
	if strings.Contains(input.Target.LocatorHash, "repo-private-name") {
		t.Fatalf("LocatorHash leaked raw target: %q", input.Target.LocatorHash)
	}
	if got, want := input.Limits.MaxFacts, 50000; got != want {
		t.Fatalf("Limits.MaxFacts = %d, want %d", got, want)
	}
	if len(committer.facts) != 1 {
		t.Fatalf("committed facts = %d, want 1", len(committer.facts))
	}
	if got := committer.mutation; got.TenantID != item.TenantID ||
		got.WorkspaceID != item.WorkspaceID ||
		got.SubjectClass != item.SubjectClass ||
		got.PolicyRevisionHash != item.PolicyRevisionHash {
		t.Fatalf("commit mutation tenant boundary = %#v, want boundary from work item", got)
	}
	if committer.facts[0].FactKind != facts.ScannerWorkerAnalysisFactKind {
		t.Fatalf("committed fact kind = %q, want scanner source fact", committer.facts[0].FactKind)
	}
	if !store.completed {
		t.Fatal("completed = false, want true")
	}
	if store.retryable || store.terminal {
		t.Fatalf("retryable=%v terminal=%v, want false,false", store.retryable, store.terminal)
	}
}

func TestServiceRecordsRetryableAnalyzerFailure(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	committer := &recordingClaimCommitter{}
	analyzer := &recordingAnalyzer{
		err: NewRetryableAnalyzerFailure(
			FailureClassSourceUnavailable,
			ResourceUsage{CPUSeconds: 1.25, PeakMemoryBytes: 128 << 20},
			errors.New("temporary source outage at raw fixture source locator"),
		),
	}
	service := testScannerService(store, committer, analyzer)

	if err := service.processClaimed(context.Background(), item, claim); err != nil {
		t.Fatalf("processClaimed() error = %v, want nil after retry record", err)
	}
	if !store.retryable {
		t.Fatal("retryable = false, want true")
	}
	if store.retryMutation.FailureClass != string(FailureClassSourceUnavailable) {
		t.Fatalf("FailureClass = %q, want %q", store.retryMutation.FailureClass, FailureClassSourceUnavailable)
	}
	if !strings.Contains(store.retryMutation.FailureMessage, `"retryable":true`) {
		t.Fatalf("FailureMessage = %q, want retryable payload", store.retryMutation.FailureMessage)
	}
	if strings.Contains(store.retryMutation.FailureMessage, item.ScopeID) ||
		strings.Contains(store.retryMutation.FailureMessage, "raw fixture source locator") {
		t.Fatalf("FailureMessage leaked raw target detail: %q", store.retryMutation.FailureMessage)
	}
	if committer.called || store.completed || store.terminal {
		t.Fatalf("called=%v completed=%v terminal=%v, want no commit/complete/dead-letter", committer.called, store.completed, store.terminal)
	}
}

func TestServiceRecordsRetryableTimeoutFailure(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	committer := &recordingClaimCommitter{}
	analyzer := &contextBlockingAnalyzer{
		usage: ResourceUsage{CPUSeconds: 3.5, PeakMemoryBytes: 512 << 20},
	}
	service := testScannerService(store, committer, &recordingAnalyzer{})
	service.Analyzer = analyzer
	service.ResourceLimits = testResourceLimits()
	service.ResourceLimits.Timeout = time.Nanosecond

	if err := service.processClaimed(context.Background(), item, claim); err != nil {
		t.Fatalf("processClaimed() error = %v, want nil after timeout retry record", err)
	}
	if !store.retryable {
		t.Fatal("retryable = false, want true")
	}
	if store.retryMutation.FailureClass != string(FailureClassTimeout) {
		t.Fatalf("FailureClass = %q, want %q", store.retryMutation.FailureClass, FailureClassTimeout)
	}
	if !strings.Contains(store.retryMutation.FailureMessage, `"failure_class":"timeout"`) {
		t.Fatalf("FailureMessage = %q, want timeout payload", store.retryMutation.FailureMessage)
	}
	if committer.called || store.completed || store.terminal {
		t.Fatalf("called=%v completed=%v terminal=%v, want no commit/complete/dead-letter", committer.called, store.completed, store.terminal)
	}
}

func TestServiceRecordsDeadLetterPayloadForTerminalFailure(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	analyzer := &recordingAnalyzer{
		err: NewTerminalAnalyzerFailure(
			FailureClassMemoryLimitExceeded,
			ResourceUsage{CPUSeconds: 9, PeakMemoryBytes: 4 << 30},
			errors.New("scanner exceeded memory while reading private image"),
		),
	}
	service := testScannerService(store, &recordingClaimCommitter{}, analyzer)

	if err := service.processClaimed(context.Background(), item, claim); err != nil {
		t.Fatalf("processClaimed() error = %v, want nil after terminal record", err)
	}
	if !store.terminal {
		t.Fatal("terminal = false, want true")
	}
	if store.terminalMutation.FailureClass != string(FailureClassMemoryLimitExceeded) {
		t.Fatalf("FailureClass = %q, want %q", store.terminalMutation.FailureClass, FailureClassMemoryLimitExceeded)
	}
	for _, forbidden := range []string{item.ScopeID, "private image"} {
		if strings.Contains(store.terminalMutation.FailureMessage, forbidden) {
			t.Fatalf("FailureMessage leaked %q in %q", forbidden, store.terminalMutation.FailureMessage)
		}
	}
	if !strings.Contains(store.terminalMutation.FailureMessage, `"dead_letter"`) {
		t.Fatalf("FailureMessage = %q, want dead-letter payload", store.terminalMutation.FailureMessage)
	}
}

func TestServiceDeadLettersSilentCleanOutput(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	committer := &recordingClaimCommitter{}
	analyzer := &recordingAnalyzer{
		result: AnalyzerResult{
			Output: FactOutput{TargetCount: 1, ResultCount: 0},
			Usage:  ResourceUsage{CPUSeconds: 0.2, PeakMemoryBytes: 64 << 20},
		},
	}
	service := testScannerService(store, committer, analyzer)

	if err := service.processClaimed(context.Background(), item, claim); err != nil {
		t.Fatalf("processClaimed() error = %v, want nil after dead-letter record", err)
	}
	if !store.terminal {
		t.Fatal("terminal = false, want true for silent clean output")
	}
	if committer.called || store.completed {
		t.Fatalf("commit called=%v completed=%v, want false,false", committer.called, store.completed)
	}
	if got := store.terminalMutation.FailureClass; got != string(FailureClassAnalyzerFailed) {
		t.Fatalf("FailureClass = %q, want %q", got, FailureClassAnalyzerFailed)
	}
}

func TestDefaultResourceLimitsByAnalyzerClass(t *testing.T) {
	t.Parallel()

	source, err := DefaultResourceLimits(AnalyzerSourceAnalysis)
	if err != nil {
		t.Fatalf("DefaultResourceLimits(source) error = %v, want nil", err)
	}
	if source.CPUMillis != 4000 || source.MemoryBytes != 4<<30 || source.Timeout != 10*time.Minute {
		t.Fatalf("source limits = %#v, want 4000m/4Gi/10m", source)
	}
	if source.MaxFiles != 250000 || source.MaxFacts != 50000 {
		t.Fatalf("source cardinality limits = %#v, want 250000 files and 50000 facts", source)
	}

	image, err := DefaultResourceLimits(AnalyzerImageUnpacking)
	if err != nil {
		t.Fatalf("DefaultResourceLimits(image) error = %v, want nil", err)
	}
	if image.CPUMillis != 6000 || image.MemoryBytes != 12<<30 || image.Timeout != 15*time.Minute {
		t.Fatalf("image limits = %#v, want 6000m/12Gi/15m", image)
	}
	if image.MaxInputBytes != 4<<30 {
		t.Fatalf("image MaxInputBytes = %d, want 4Gi", image.MaxInputBytes)
	}

	if _, err := DefaultResourceLimits(AnalyzerVulnerabilityMatching); err == nil {
		t.Fatal("DefaultResourceLimits(reducer analyzer) error = nil, want rejection")
	}
}

func TestFactKindCountsAggregatesEmittedFacts(t *testing.T) {
	t.Parallel()

	counts := factKindCounts([]facts.Envelope{
		{FactKind: facts.ScannerWorkerAnalysisFactKind},
		{FactKind: facts.ScannerWorkerWarningFactKind},
		{FactKind: facts.ScannerWorkerAnalysisFactKind},
	})

	if got, want := counts[facts.ScannerWorkerAnalysisFactKind], int64(2); got != want {
		t.Fatalf("analysis fact count = %d, want %d", got, want)
	}
	if got, want := counts[facts.ScannerWorkerWarningFactKind], int64(1); got != want {
		t.Fatalf("warning fact count = %d, want %d", got, want)
	}
}

func TestRecordSuccessAggregatesFactMetricAddsByKind(t *testing.T) {
	t.Parallel()

	factsEmitted := &recordingFactsCounter{}
	service := Service{
		Instruments: &telemetry.Instruments{
			ScannerWorkerClaims:       noop.Int64Counter{},
			ScannerWorkerTargetCount:  noop.Int64Histogram{},
			ScannerWorkerResultCount:  noop.Int64Histogram{},
			ScannerWorkerCPUSeconds:   noop.Float64Histogram{},
			ScannerWorkerMemoryBytes:  noop.Int64Histogram{},
			ScannerWorkerFactsEmitted: factsEmitted,
		},
	}
	service.recordSuccess(context.Background(), ClaimInput{
		Analyzer: AnalyzerSBOMGeneration,
		Target:   TargetScope{Kind: TargetRepository},
	}, AnalyzerResult{
		Output: FactOutput{
			TargetCount: 1,
			ResultCount: 3,
			Facts: []facts.Envelope{
				{FactKind: facts.ScannerWorkerAnalysisFactKind},
				{FactKind: facts.ScannerWorkerWarningFactKind},
				{FactKind: facts.ScannerWorkerAnalysisFactKind},
			},
		},
	})

	counts := make(map[string]int64)
	for _, call := range factsEmitted.calls {
		counts[call.factKind] += call.increment
	}
	if got, want := len(factsEmitted.calls), 2; got != want {
		t.Fatalf("facts emitted Add calls = %d, want one per fact kind %d", got, want)
	}
	if got, want := counts[facts.ScannerWorkerAnalysisFactKind], int64(2); got != want {
		t.Fatalf("analysis increment = %d, want %d", got, want)
	}
	if got, want := counts[facts.ScannerWorkerWarningFactKind], int64(1); got != want {
		t.Fatalf("warning increment = %d, want %d", got, want)
	}
}

func TestFactChannelUsesBoundedBuffer(t *testing.T) {
	t.Parallel()

	values := make([]facts.Envelope, 100)
	stream := factChannel(values)
	if got, want := cap(stream), scannerFactChannelBuffer; got != want {
		t.Fatalf("factChannel cap = %d, want bounded buffer %d", got, want)
	}
	for range stream {
	}
}

type factMetricCall struct {
	factKind  string
	increment int64
}

type recordingFactsCounter struct {
	noop.Int64Counter
	calls []factMetricCall
}

func (c *recordingFactsCounter) Add(_ context.Context, incr int64, options ...metric.AddOption) {
	attrs := metric.NewAddConfig(options).Attributes()
	factKind, _ := attrs.Value(attribute.Key(telemetry.MetricDimensionFactKind))
	c.calls = append(c.calls, factMetricCall{
		factKind:  factKind.AsString(),
		increment: incr,
	})
}

func testScannerService(store *recordingClaimStore, committer *recordingClaimCommitter, analyzer *recordingAnalyzer) Service {
	return Service{
		ControlStore:        store,
		Committer:           committer,
		Analyzer:            analyzer,
		AnalyzerKind:        AnalyzerSBOMGeneration,
		CollectorInstanceID: "collector-scanner",
		OwnerID:             "scanner-worker-1",
		ClaimIDFunc:         func() string { return "claim-next" },
		PollInterval:        time.Second,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   20 * time.Second,
		Clock: func() time.Time {
			return time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
		},
	}
}

func testScannerFact(t *testing.T, item workflow.WorkItem, claim workflow.Claim, factKind string) facts.Envelope {
	t.Helper()

	version, ok := sourceFactSchemaVersion(factKind)
	if !ok {
		t.Fatalf("sourceFactSchemaVersion(%q) ok = false, want true", factKind)
	}
	return facts.Envelope{
		FactID:           "scanner-fact-1",
		ScopeID:          item.ScopeID,
		GenerationID:     item.GenerationID,
		FactKind:         factKind,
		StableFactKey:    "scanner-fact-key",
		SchemaVersion:    version,
		CollectorKind:    string(scope.CollectorScannerWorker),
		FencingToken:     claim.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       item.LastClaimedAt,
		Payload:          map[string]any{"analyzer": string(AnalyzerSBOMGeneration)},
		SourceRef: facts.Ref{
			SourceSystem: string(scope.CollectorScannerWorker),
			ScopeID:      item.ScopeID,
			GenerationID: item.GenerationID,
			FactKey:      "scanner-fact-key",
		},
	}
}

type recordingAnalyzer struct {
	result AnalyzerResult
	err    error
	inputs []ClaimInput
}

func (a *recordingAnalyzer) Analyze(_ context.Context, input ClaimInput) (AnalyzerResult, error) {
	a.inputs = append(a.inputs, input)
	return a.result, a.err
}

type contextBlockingAnalyzer struct {
	usage  ResourceUsage
	inputs []ClaimInput
}

func (a *contextBlockingAnalyzer) Analyze(ctx context.Context, input ClaimInput) (AnalyzerResult, error) {
	a.inputs = append(a.inputs, input)
	<-ctx.Done()
	return AnalyzerResult{Usage: a.usage}, ctx.Err()
}

type recordingClaimCommitter struct {
	called     bool
	mutation   workflow.ClaimMutation
	scope      scope.IngestionScope
	generation scope.ScopeGeneration
	facts      []facts.Envelope
	err        error
}

func (c *recordingClaimCommitter) CommitClaimedScopeGeneration(
	_ context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	c.called = true
	c.mutation = mutation
	c.scope = scopeValue
	c.generation = generation
	for fact := range factStream {
		c.facts = append(c.facts, fact)
	}
	return c.err
}

type recordingClaimStore struct {
	heartbeat        bool
	completed        bool
	retryable        bool
	terminal         bool
	completeMutation workflow.ClaimMutation
	retryMutation    workflow.ClaimMutation
	terminalMutation workflow.ClaimMutation
}

func (s *recordingClaimStore) ClaimNextEligible(context.Context, workflow.ClaimSelector, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
	return workflow.WorkItem{}, workflow.Claim{}, false, nil
}

func (s *recordingClaimStore) HeartbeatClaim(_ context.Context, _ workflow.ClaimMutation) error {
	s.heartbeat = true
	return nil
}

func (s *recordingClaimStore) CompleteClaim(_ context.Context, mutation workflow.ClaimMutation) error {
	s.completed = true
	s.completeMutation = mutation
	return nil
}

func (s *recordingClaimStore) FailClaimRetryable(_ context.Context, mutation workflow.ClaimMutation) error {
	s.retryable = true
	s.retryMutation = mutation
	return nil
}

func (s *recordingClaimStore) FailClaimTerminal(_ context.Context, mutation workflow.ClaimMutation) error {
	s.terminal = true
	s.terminalMutation = mutation
	return nil
}
