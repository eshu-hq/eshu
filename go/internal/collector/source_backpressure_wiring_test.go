// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestLoadSourceBackpressureConfigNamesTheMalformedVariable proves every
// per-source backpressure variable is parsed strictly: a value that is not a
// duration (or, for the depth floor, an integer) refuses startup with an error
// naming that variable, instead of loading a half-read throttle.
func TestLoadSourceBackpressureConfigNamesTheMalformedVariable(t *testing.T) {
	t.Parallel()

	full := map[string]string{
		SourceBackpressureEngageAgeEnv:     "1h",
		SourceBackpressureReleaseAgeEnv:    "15m",
		SourceBackpressureDepthFloorEnv:    "25",
		SourceBackpressureGrantDelayEnv:    "2s",
		SourceBackpressureMaxEngagementEnv: "30m",
		SourceBackpressureSignalCacheEnv:   "30s",
	}
	for _, name := range []string{
		SourceBackpressureEngageAgeEnv,
		SourceBackpressureReleaseAgeEnv,
		SourceBackpressureDepthFloorEnv,
		SourceBackpressureGrantDelayEnv,
		SourceBackpressureMaxEngagementEnv,
		SourceBackpressureSignalCacheEnv,
	} {
		env := map[string]string{}
		for key, value := range full {
			env[key] = value
		}
		env[name] = "not-a-number"
		config, err := LoadSourceBackpressureConfig(func(key string) string { return env[key] })
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("malformed %s: error = %v, want one naming the variable", name, err)
		}
		if config != (workflow.SourceBackpressureConfig{}) {
			t.Fatalf("malformed %s: config = %+v, want the zero value on error", name, config)
		}
	}
}

type failingSourceStore struct {
	stubClaimStore
	err error
}

func (s *failingSourceStore) CheckSourceBackpressure(
	context.Context, workflow.ClaimSelector, workflow.SourceBackpressureConfig, time.Time,
) (workflow.ClaimPacingDecision, error) {
	return workflow.ClaimPacingDecision{}, s.err
}

// TestSourceBackpressureDeferralStaysOutOfTheWayWhenItCannotJudge proves the
// direct grant path never throttles on a consult it cannot trust: an absent or
// disabled config, a control store that does not serve the consult, an unpaced
// verdict and a failing consult (logged at debug) all return no deferral, so
// the claim proceeds exactly as before the leg existed.
func TestSourceBackpressureDeferralStaysOutOfTheWayWhenItCannotJudge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	enabled := sourceBackpressureDispatcherConfig()
	disabled := workflow.SourceBackpressureConfig{}
	unpaced := &sourceStubClaimStore{}
	for name, svc := range map[string]ClaimedService{
		"no config":      {ControlStore: unpaced},
		"disabled":       {ControlStore: unpaced, SourceBackpressure: &disabled},
		"store without":  {ControlStore: &stubClaimStore{}, SourceBackpressure: &enabled},
		"unpaced":        {ControlStore: unpaced, SourceBackpressure: &enabled},
		"consult failed": {ControlStore: &failingSourceStore{err: errors.New("queue read failed")}, SourceBackpressure: &enabled},
	} {
		svc.Clock = func() time.Time { return now }
		if got := svc.sourceBackpressureDeferral(context.Background(), pacingConsultSelector()); got != nil {
			t.Fatalf("%s: deferral = %+v, want nil", name, got)
		}
	}

	buf, logger := consultLogSink()
	svc := ClaimedService{
		ControlStore:       &failingSourceStore{err: errors.New("queue read failed")},
		SourceBackpressure: &enabled,
		Logger:             logger,
		Clock:              func() time.Time { return now },
	}
	if got := svc.sourceBackpressureDeferral(context.Background(), pacingConsultSelector()); got != nil {
		t.Fatalf("consult failure deferral = %+v, want nil (fail open)", got)
	}
	if logged := buf.String(); !strings.Contains(logged, "source backpressure consult failed open") || !strings.Contains(logged, "queue read failed") {
		t.Fatalf("log = %q, want the fail-open record naming the error", logged)
	}
}

// TestClaimNextReturnsTheSourceDeferralWithoutClaiming proves the direct grant
// path hands a slowed source's verdict to the runner as a deferral error and
// issues no claim, so no attempt is consumed and the work item stays pending.
func TestClaimNextReturnsTheSourceDeferralWithoutClaiming(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	store := &sourceStubClaimStore{decision: workflow.ClaimPacingDecision{
		Paced: true, Signal: workflow.ClaimPacingSignalReducerSource, Domain: "git", RetryAfter: 2 * time.Second,
	}}
	cfg := sourceBackpressureDispatcherConfig()
	svc := ClaimedService{
		ControlStore:        store,
		SourceBackpressure:  &cfg,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "git-primary",
		OwnerID:             "owner-1",
		Clock:               func() time.Time { return now },
	}
	_, _, target, found, err := svc.claimNext(context.Background(), "claim-1")
	var deferral *workflow.ClaimPacingDeferral
	if !errors.As(err, &deferral) {
		t.Fatalf("claimNext() error = %v, want a *ClaimPacingDeferral", err)
	}
	if deferral.Domain != "git" || deferral.RetryAfter != 2*time.Second {
		t.Fatalf("deferral = %+v, want git with the 2s grant delay", deferral)
	}
	if found || target.CollectorInstanceID != "git-primary" {
		t.Fatalf("found = %v, target = %+v, want no claim and the service's own target", found, target)
	}
	if store.claimCalls != 0 {
		t.Fatalf("claim calls = %d, want 0: a slowed source issues no claim", store.claimCalls)
	}
}

// TestMultiSourceHostWiresSourceBackpressure proves the host validates the
// per-source config and refuses a control store that cannot serve the consult
// when the leg is enabled, and otherwise puts the consult in front of every
// grant: a slowed family's grant comes back as a deferral.
func TestMultiSourceHostWiresSourceBackpressure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	build := func(store ClaimControlStore, cfg workflow.SourceBackpressureConfig) (*MultiSourceCollectorHost, error) {
		return NewMultiSourceCollectorHost(MultiSourceCollectorHostConfig{
			Template: ClaimedService{ControlStore: store},
			Sources: []ClaimSourceRegistration{
				wildcardSourceRegistration(scope.CollectorGit, &recordingClaimedSource{}),
			},
			Instances: []workflow.CollectorInstance{
				enabledCollectorInstance(scope.CollectorGit, "git-primary"),
			},
			SourceBackpressure: &cfg,
		})
	}

	if _, err := build(&sourceStubClaimStore{}, workflow.SourceBackpressureConfig{EngageAge: time.Hour}); err == nil || !strings.Contains(err.Error(), "source backpressure config") {
		t.Fatalf("partial config error = %v, want a source backpressure config error", err)
	}
	if _, err := build(&stubClaimStore{}, sourceBackpressureDispatcherConfig()); err == nil || !strings.Contains(err.Error(), "CheckSourceBackpressure") {
		t.Fatalf("store without the consult error = %v, want it to name CheckSourceBackpressure", err)
	}
	if host, err := build(&stubClaimStore{}, workflow.SourceBackpressureConfig{}); err != nil || host == nil {
		t.Fatalf("disabled config = (%v, %v), want a host with no source consult", host, err)
	}

	store := &sourceStubClaimStore{decision: workflow.ClaimPacingDecision{
		Paced: true, Signal: workflow.ClaimPacingSignalReducerSource, Domain: "git", RetryAfter: 2 * time.Second,
	}}
	host, err := build(store, sourceBackpressureDispatcherConfig())
	if err != nil {
		t.Fatalf("enabled config error = %v, want nil", err)
	}
	_, _, _, found, err := host.dispatcher.ClaimNext(context.Background(), "owner-1", "claim-1", now, time.Minute)
	var deferral *workflow.ClaimPacingDeferral
	if found || !errors.As(err, &deferral) {
		t.Fatalf("ClaimNext() = (found %v, %v), want a deferral for the slowed family", found, err)
	}
}
