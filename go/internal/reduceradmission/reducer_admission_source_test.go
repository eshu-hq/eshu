// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// scriptedSourceChecker returns scripted slowed verdicts per source: each
// consult pops the next slowed flag for the source, and an exhausted script
// reads as clear. It proves the admission writer consults per intent-batch
// source without a live store.
type scriptedSourceChecker struct {
	mu      sync.Mutex
	scripts map[string][]bool
	calls   map[string]int
}

func (c *scriptedSourceChecker) CheckSourceBackpressureForSource(
	_ context.Context,
	source string,
	_ workflow.SourceBackpressureConfig,
	_ time.Time,
) (workflow.SourceBackpressureDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[source]++
	script := c.scripts[source]
	slowed := false
	if len(script) > 0 {
		slowed = script[0]
		c.scripts[source] = script[1:]
	}
	decision := workflow.SourceBackpressureDecision{SourceSystem: source}
	if slowed {
		decision.Slowed = true
		decision.Reason = workflow.SourceBackpressureReasonSourceBehind
		decision.RetryAfter = time.Second
	}
	return decision, nil
}

// admissionSourceTestConfig returns an enabled source-backpressure config at
// test scale.
func admissionSourceTestConfig() workflow.SourceBackpressureConfig {
	return workflow.SourceBackpressureConfig{
		EngageAge:     time.Hour,
		ReleaseAge:    15 * time.Minute,
		DepthFloor:    25,
		GrantDelay:    time.Second,
		MaxEngagement: 30 * time.Minute,
	}
}

// TestReducerAdmissionSourceBehindDefersOnlySlowedSource proves the
// per-source admission leg (#10882 S3): intents for the slowed git source
// defer under reason source_behind while another source's intents in the
// same batch admit immediately (AC5), and after release every git intent
// lands (AC4: none lost, intake resumes). The batch mixes sources, so the
// writer must partition by source instead of gating the whole batch.
func TestReducerAdmissionSourceBehindDefersOnlySlowedSource(t *testing.T) {
	t.Parallel()

	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{{"reducer": {"pending": 1}}},
	}
	writer := &recordingReducerIntentWriter{}
	checker := &scriptedSourceChecker{scripts: map[string][]bool{
		"git": {true, true, false},
	}}
	var reasons []string
	sleeps := 0
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark: 10_000,
			PollInterval:  time.Second,
		},
		deferral:      newAdmissionDeferralState(),
		sourceChecker: checker,
		sourceConfig:  admissionSourceTestConfig(),
		sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
		failureClassSink: func(_ context.Context, reason, _ string, _ int64, _ int) {
			reasons = append(reasons, reason)
		},
	}
	intents := []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "git", EntityKey: "git-1"},
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "aws", EntityKey: "aws-1"},
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "git", EntityKey: "git-2"},
	}

	result, err := admission.Enqueue(context.Background(), intents)
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if result.Count != len(intents) {
		t.Fatalf("Enqueue() count = %d, want %d (every intent lands)", result.Count, len(intents))
	}
	// Two inner enqueues: the healthy aws group first, then git after
	// release. RecordingReducerIntentWriter batches are per call.
	if writer.calls != 2 {
		t.Fatalf("inner enqueue calls = %d, want 2 (healthy group, then slowed group after release)", writer.calls)
	}
	if sleeps != 2 {
		t.Fatalf("sleep count = %d, want 2 (two slowed polls before release)", sleeps)
	}
	for _, reason := range reasons {
		if reason != workflow.SourceBackpressureReasonSourceBehind {
			t.Fatalf("deferral reason = %q, want source_behind", reason)
		}
	}
	if len(reasons) != 2 {
		t.Fatalf("deferrals recorded = %d, want 2", len(reasons))
	}
	if checker.calls["aws"] != 1 {
		t.Fatalf("aws consults = %d, want 1 (healthy source admitted on first consult)", checker.calls["aws"])
	}
}

// TestLoadSourceBackpressureConfig proves the shared env contract: unset
// means disabled (no Proposed thresholds ship as defaults), a full set
// enables the leg, and a partial set is a startup error.
func TestLoadSourceBackpressureConfig(t *testing.T) {
	t.Parallel()

	disabled, err := loadSourceBackpressureConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadSourceBackpressureConfig(unset) error = %v, want nil", err)
	}
	if disabled.Enabled() {
		t.Fatal("unset env Enabled() = true, want false (no default thresholds)")
	}

	env := map[string]string{
		sourceBackpressureEngageAgeEnv:     "1h",
		sourceBackpressureReleaseAgeEnv:    "15m",
		sourceBackpressureDepthFloorEnv:    "25",
		sourceBackpressureGrantDelayEnv:    "2s",
		sourceBackpressureMaxEngagementEnv: "30m",
	}
	enabled, err := loadSourceBackpressureConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadSourceBackpressureConfig(full) error = %v, want nil", err)
	}
	if !enabled.Enabled() || enabled.EngageAge != time.Hour || enabled.DepthFloor != 25 {
		t.Fatalf("enabled config = %+v, want 1h engage / floor 25", enabled)
	}

	delete(env, sourceBackpressureReleaseAgeEnv)
	if _, err := loadSourceBackpressureConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("loadSourceBackpressureConfig(partial) error = nil, want startup error")
	}
}

// TestSourceBackpressureEnvMatchesCollectorContract pins this package's
// env names to the canonical collector contract: two loaders read one
// contract, so a rename on either side fails here instead of silently
// wiring different variables (#10882 decision 3).
func TestSourceBackpressureEnvMatchesCollectorContract(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{sourceBackpressureEngageAgeEnv, collector.SourceBackpressureEngageAgeEnv},
		{sourceBackpressureReleaseAgeEnv, collector.SourceBackpressureReleaseAgeEnv},
		{sourceBackpressureDepthFloorEnv, collector.SourceBackpressureDepthFloorEnv},
		{sourceBackpressureGrantDelayEnv, collector.SourceBackpressureGrantDelayEnv},
		{sourceBackpressureMaxEngagementEnv, collector.SourceBackpressureMaxEngagementEnv},
		{sourceBackpressureSignalCacheEnv, collector.SourceBackpressureSignalCacheEnv},
	}
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			t.Fatalf("admission env %q != collector env %q: one contract, two names", pair[0], pair[1])
		}
	}
}

// alwaysSlowedSourceChecker reports every source slowed on every consult: a
// source whose engagement outlasts any finite script.
type alwaysSlowedSourceChecker struct{}

func (alwaysSlowedSourceChecker) CheckSourceBackpressureForSource(
	_ context.Context,
	source string,
	_ workflow.SourceBackpressureConfig,
	_ time.Time,
) (workflow.SourceBackpressureDecision, error) {
	return workflow.SourceBackpressureDecision{
		SourceSystem: source,
		Slowed:       true,
		Reason:       workflow.SourceBackpressureReasonSourceBehind,
		RetryAfter:   time.Second,
	}, nil
}

// TestReducerAdmissionSourceBehindTimeoutIsSourcePacedNotAdmissionTimeout
// proves #10882 fork 1: a wait that outlasts MaxWait while the source is
// latched slowed returns the source-paced error, never the admission
// timeout. The admission timeout spends the finite admission budget and
// dead-letters at exhaustion as admission_timeout, so a long engagement
// would mint dead letters out of pacing (AC4). The paced error is retryable,
// carries no dead-letter class, and releases the claim without spending
// either attempt budget.
func TestReducerAdmissionSourceBehindTimeoutIsSourcePacedNotAdmissionTimeout(t *testing.T) {
	t.Parallel()

	writer := &recordingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner: writer,
		depthReader: &fakeReducerAdmissionDepthReader{
			depths: []map[string]map[string]int64{{"reducer": {"pending": 1}}},
		},
		config: reducerAdmissionConfig{
			HighWaterMark: 10_000,
			PollInterval:  time.Millisecond,
			MaxWait:       30 * time.Millisecond,
		},
		deferral:      newAdmissionDeferralState(),
		sourceChecker: alwaysSlowedSourceChecker{},
		sourceConfig:  admissionSourceTestConfig(),
		sleep:         func(context.Context, time.Duration) error { return nil },
	}

	_, err := admission.Enqueue(context.Background(), []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "git", EntityKey: "git-1"},
	})
	if err == nil {
		t.Fatal("Enqueue() error = nil, want a source-paced release past MaxWait")
	}
	if errors.Is(err, ErrReducerAdmissionTimeout) {
		t.Fatalf("Enqueue() error = %v: a source_behind wait must not surface as the admission timeout", err)
	}
	var admissionErr failure.AdmissionTimeoutError
	if errors.As(err, &admissionErr) {
		t.Fatalf("Enqueue() error %v implements AdmissionTimeoutError: it would spend the admission budget", err)
	}
	var pacedErr failure.SourcePacedError
	if !errors.As(fmt.Errorf("enqueue reducer intents: %w", err), &pacedErr) || !pacedErr.IsSourcePaced() {
		t.Fatalf("Enqueue() error = %v, want failure.SourcePacedError through the projector wrap", err)
	}
	if !failure.IsRetryable(err) {
		t.Fatal("source-paced error IsRetryable = false, want true so Fail releases the claim")
	}
	var classed interface{ FailureClass() string }
	if errors.As(err, &classed) {
		t.Fatalf("source-paced error carries failure class %q: it must never label a dead letter", classed.FailureClass())
	}
	if writer.calls != 0 {
		t.Fatalf("inner enqueue calls = %d, want 0 (nothing admitted while slowed)", writer.calls)
	}
}
