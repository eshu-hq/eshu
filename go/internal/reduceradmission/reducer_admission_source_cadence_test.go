// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// pacedChecker reports the named source slowed with RetryAfter for the first
// slowedRounds consults and clear afterwards, counting every consult.
type pacedChecker struct {
	mu           sync.Mutex
	retryAfter   time.Duration
	slowedRounds int
	calls        int
}

func (c *pacedChecker) CheckSourceBackpressureForSource(
	_ context.Context,
	source string,
	_ workflow.SourceBackpressureConfig,
	_ time.Time,
) (workflow.SourceBackpressureDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls <= c.slowedRounds {
		return workflow.SourceBackpressureDecision{
			SourceSystem: source,
			Slowed:       true,
			Reason:       workflow.SourceBackpressureReasonSourceBehind,
			RetryAfter:   c.retryAfter,
		}, nil
	}
	return workflow.SourceBackpressureDecision{SourceSystem: source}, nil
}

func cadenceWriter(inner runtime.ReducerIntentWriter, checker reducerAdmissionSourceChecker, depths []map[string]map[string]int64, maxWait time.Duration, sleeps *[]time.Duration) reducerAdmissionWriter {
	return reducerAdmissionWriter{
		inner:         inner,
		depthReader:   &fakeReducerAdmissionDepthReader{depths: depths},
		config:        reducerAdmissionConfig{HighWaterMark: 10, PollInterval: time.Second, MaxWait: maxWait},
		deferral:      newAdmissionDeferralState(),
		sourceChecker: checker,
		sourceConfig:  admissionSourceTestConfig(),
		sleep: func(_ context.Context, d time.Duration) error {
			*sleeps = append(*sleeps, d)
			return nil
		},
	}
}

func gitIntent() []runtime.ReducerIntent {
	return []runtime.ReducerIntent{{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "git", EntityKey: "git-1"}}
}

// TestReducerAdmissionAllSourcePacedWaitsTheGrantDelay proves #10882 F4: when
// every deferred group is source_behind the writer waits max(PollInterval,
// RetryAfter) between rounds, so a paced worker does not rerun the global
// depth reads, the bounded probe, and the latch read every second. N rounds
// still cost exactly N consults.
func TestReducerAdmissionAllSourcePacedWaitsTheGrantDelay(t *testing.T) {
	t.Parallel()

	var sleeps []time.Duration
	checker := &pacedChecker{retryAfter: 30 * time.Second, slowedRounds: 3}
	writer := &recordingReducerIntentWriter{}
	admission := cadenceWriter(writer, checker, []map[string]map[string]int64{{"reducer": {"pending": 1}}}, 5*time.Minute, &sleeps)

	if _, err := admission.Enqueue(context.Background(), gitIntent()); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if len(sleeps) != 3 {
		t.Fatalf("sleeps = %v, want 3 (one per slowed round)", sleeps)
	}
	for i, d := range sleeps {
		if d != 30*time.Second {
			t.Fatalf("sleep %d = %s, want 30s (max of PollInterval 1s and RetryAfter 30s)", i, d)
		}
	}
	if checker.calls != 4 {
		t.Fatalf("consults = %d, want 4 (3 slowed + 1 clear): N rounds cost N consults", checker.calls)
	}
}

// TestReducerAdmissionPollIntervalWinsWhenLongerThanGrantDelay keeps the
// floor: a RetryAfter below PollInterval never shortens the poll.
func TestReducerAdmissionPollIntervalWinsWhenLongerThanGrantDelay(t *testing.T) {
	t.Parallel()

	var sleeps []time.Duration
	checker := &pacedChecker{retryAfter: 100 * time.Millisecond, slowedRounds: 1}
	admission := cadenceWriter(&recordingReducerIntentWriter{}, checker, []map[string]map[string]int64{{"reducer": {"pending": 1}}}, 5*time.Minute, &sleeps)

	if _, err := admission.Enqueue(context.Background(), gitIntent()); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if len(sleeps) != 1 || sleeps[0] != time.Second {
		t.Fatalf("sleeps = %v, want [1s] (PollInterval floor)", sleeps)
	}
}

// TestReducerAdmissionGlobalReasonKeepsPollInterval proves a round where any
// deferred group is deferred by a global reason polls at PollInterval: the
// grant delay is a source-latch backoff, not a database-protection one.
func TestReducerAdmissionGlobalReasonKeepsPollInterval(t *testing.T) {
	t.Parallel()

	var sleeps []time.Duration
	checker := &pacedChecker{retryAfter: 30 * time.Second, slowedRounds: 1}
	// Round 1: the first group's global read sees high water (reason
	// high_water), the second group's read sees clear depth and then the
	// source latch slows it. Mixed reasons must poll at PollInterval.
	depths := []map[string]map[string]int64{
		{"reducer": {"pending": 50}},
		{"reducer": {"pending": 1}},
		{"reducer": {"pending": 1}},
		{"reducer": {"pending": 1}},
	}
	admission := cadenceWriter(&recordingReducerIntentWriter{}, checker, depths, 5*time.Minute, &sleeps)
	intents := []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "aws", EntityKey: "aws-1"},
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "git", EntityKey: "git-1"},
	}

	if _, err := admission.Enqueue(context.Background(), intents); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if len(sleeps) == 0 || sleeps[0] != time.Second {
		t.Fatalf("sleeps = %v, want first sleep 1s (a global reason keeps PollInterval)", sleeps)
	}
}

// TestReducerAdmissionGrantDelayCappedByRemainingMaxWait proves a long grant
// delay never sleeps past the claim-scoped MaxWait: the wait is capped by
// what remains, so the timeout fires on time instead of a poll later.
func TestReducerAdmissionGrantDelayCappedByRemainingMaxWait(t *testing.T) {
	t.Parallel()

	var sleeps []time.Duration
	checker := &pacedChecker{retryAfter: 30 * time.Second, slowedRounds: 1}
	maxWait := 2 * time.Second
	admission := cadenceWriter(&recordingReducerIntentWriter{}, checker, []map[string]map[string]int64{{"reducer": {"pending": 1}}}, maxWait, &sleeps)

	if _, err := admission.Enqueue(context.Background(), gitIntent()); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if len(sleeps) != 1 {
		t.Fatalf("sleeps = %v, want 1", sleeps)
	}
	if sleeps[0] > maxWait || sleeps[0] >= 30*time.Second {
		t.Fatalf("sleep = %s, want <= remaining MaxWait %s (grant delay 30s capped)", sleeps[0], maxWait)
	}
}
