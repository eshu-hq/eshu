// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	gitcollector "github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// discoverySkipErr is a retryable discovery failure that opts into the cycle
// skip path, the shape the git selector returns.
type discoverySkipErr struct{ consecutive int }

func (discoverySkipErr) Error() string   { return "list GitHub org repositories: status 504" }
func (discoverySkipErr) Retryable() bool { return true }
func (e discoverySkipErr) CycleSkip() collector.CycleSkip {
	return collector.CycleSkip{
		CollectorKind: "git", FailureClass: "server_error",
		Attempts: 4, Consecutive: e.consecutive, RetryAfter: 5 * time.Second,
	}
}

// failThenBlockSource fails Next retryably `failures` times, then blocks until
// the run is cancelled.
type failThenBlockSource struct {
	failures int
	calls    atomic.Int64
}

func (s *failThenBlockSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	n := int(s.calls.Add(1))
	if n <= s.failures {
		return collector.CollectedGeneration{}, false, discoverySkipErr{consecutive: n}
	}
	<-ctx.Done()
	return collector.CollectedGeneration{}, false, ctx.Err()
}

type neverCommitter struct{}

func (neverCommitter) CommitScopeGeneration(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
	return errors.New("no generation expected")
}

// ctxProbeRunner is a projector stand-in that exposes the context the
// composite runner gave it, so a test can assert it is never cancelled.
type ctxProbeRunner struct {
	ctx     atomic.Pointer[context.Context]
	started chan struct{}
	once    sync.Once
}

func newCtxProbeRunner() *ctxProbeRunner { return &ctxProbeRunner{started: make(chan struct{})} }

func (r *ctxProbeRunner) Run(ctx context.Context) error {
	r.ctx.Store(&ctx)
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	return nil
}

// alive reports whether the projector context is still live.
func (r *ctxProbeRunner) alive() bool {
	ctx := r.ctx.Load()
	return ctx != nil && (*ctx).Err() == nil
}

// TestCompositeRunnerKeepsTheProjectorThroughSkippedDiscoveryCycles pins D1 at
// the supervisor: a collector whose Next fails retryably N times and then
// recovers never cancels the projector sibling, and a clean shutdown returns
// nil. compositeRunner and its retryable=false log stay untouched.
func TestCompositeRunnerKeepsTheProjectorThroughSkippedDiscoveryCycles(t *testing.T) {
	t.Parallel()

	const failures = 5
	projector := newCtxProbeRunner()
	source := &failThenBlockSource{failures: failures}
	var waits atomic.Int64
	var projectorCancelled atomic.Bool
	service := collector.Service{
		Source:       source,
		Committer:    neverCommitter{},
		PollInterval: time.Second,
		Wait: func(ctx context.Context, _ time.Duration) error {
			waits.Add(1)
			if !projector.alive() {
				projectorCancelled.Store(true)
			}
			return ctx.Err()
		},
	}
	runner := newCompositeRunnerWithGrace(2*time.Second, nil, service, projector)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	deadline := time.After(10 * time.Second)
	for source.calls.Load() <= int64(failures) {
		select {
		case err := <-done:
			t.Fatalf("composite run returned early with %v after %d Next calls", err, source.calls.Load())
		case <-deadline:
			t.Fatal("collector did not get past the failing cycles")
		case <-time.After(time.Millisecond):
		}
	}
	if projectorCancelled.Load() || !projector.alive() {
		t.Fatal("projector context was cancelled by a skipped discovery cycle")
	}
	if got := waits.Load(); got != failures {
		t.Fatalf("skip waits = %d, want %d", got, failures)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("composite Run() error = %v, want nil on shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("composite run did not stop on shutdown")
	}
}

func discoveryRetryPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// fakeGitHub serves the installation token mint and the org listing. The
// listing fails with 504 for the first failCycles cycles: the selector's
// in-call retry makes attemptsPerCycle requests per failing cycle, so the
// first failCycles*attemptsPerCycle listing requests fail and the next one
// returns an empty, complete listing.
type fakeGitHub struct {
	server  *httptest.Server
	mu      sync.Mutex
	listing int
	mints   int
	okLists int
}

const discoveryRetryAttemptsPerCycle = 4

func newFakeGitHub(t *testing.T, failCycles int) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/app/installations/777/access_tokens"):
			g.mints++
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "minted-token"})
		case r.Method == http.MethodGet && r.URL.Path == "/orgs/acme/repos":
			g.listing++
			if g.listing <= failCycles*discoveryRetryAttemptsPerCycle {
				http.Error(w, `{"message":"gateway timeout"}`, http.StatusGatewayTimeout)
				return
			}
			g.okLists++
			_, _ = io.WriteString(w, "[]")
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusTeapot)
		}
	}))
	t.Cleanup(g.server.Close)
	return g
}

func (g *fakeGitHub) counts() (listing, ok int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.listing, g.okLists
}

func discoveryMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) (sum int64, last int64, seen bool) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					sum += dp.Value
					seen = true
				}
			case metricdata.Gauge[int64]:
				for _, dp := range data.DataPoints {
					last = dp.Value
					seen = true
				}
			}
		}
	}
	return sum, last, seen
}

type discoveryRetryRun struct {
	err           error
	returnedEarly bool
	projectorCut  bool
	skipped       int64
	gauge         int64
	gaugeSeen     bool
	waits         []time.Duration
	listing       int
	okListings    int
}

// runDiscoveryRetry drives the real GitSource + NativeRepositorySelector
// (githubOrg mode, App auth) + collector.Service + compositeRunner against
// one fake GitHub. For a recovering sequence it shuts the run down once the
// listing has succeeded and the collector idles; for the cap sequence it
// waits for the process-level error.
func runDiscoveryRetry(t *testing.T, failCycles int) discoveryRetryRun {
	t.Helper()
	github := newFakeGitHub(t, failCycles)
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("discovery-retry-e2e"),
	)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	noSleep := func(context.Context, time.Duration) error { return nil }
	selector := &gitcollector.NativeRepositorySelector{
		Config: gitcollector.RepoSyncConfig{
			ReposDir:              t.TempDir(),
			SourceMode:            "githubOrg",
			GithubOrg:             "acme",
			RepoLimit:             100,
			GitAuthMethod:         "githubApp",
			GitHubAppID:           "42",
			GitHubAppInstallation: "777",
			GitHubAppPrivateKey:   discoveryRetryPrivateKeyPEM(t),
			GitHubAPI: rest.Settings{
				BaseURL: github.server.URL,
				Retry:   rest.Config{MaxAttempts: discoveryRetryAttemptsPerCycle, BaseBackoff: time.Second, Sleep: noSleep},
			},
		},
		Instruments: instruments,
		Logger:      logger,
	}
	source := &gitcollector.GitSource{Component: "git", Selector: selector, Instruments: instruments, Logger: logger}
	projector := newCtxProbeRunner()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var result discoveryRetryRun
	var waitMu sync.Mutex
	var cancelledByTest atomic.Bool
	service := collector.Service{
		Source:       source,
		Committer:    neverCommitter{},
		PollInterval: time.Second,
		Instruments:  instruments,
		Logger:       logger,
		Wait: func(ctx context.Context, d time.Duration) error {
			waitMu.Lock()
			result.waits = append(result.waits, d)
			waitMu.Unlock()
			if !projector.alive() {
				result.projectorCut = true
			}
			if _, ok := github.counts(); ok > 0 {
				// The listing succeeded and the collector is idling: this is
				// the recovered steady state, so stop the run.
				cancelledByTest.Store(true)
				cancel()
			}
			return ctx.Err()
		},
	}
	runner := newCompositeRunnerWithGrace(5*time.Second, logger, service, projector)
	go func() { done <- runner.Run(ctx) }()

	select {
	case result.err = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("composite run did not finish")
	}
	result.returnedEarly = !cancelledByTest.Load() && result.err == nil
	result.skipped, _, _ = discoveryMetric(t, reader, "eshu_dp_collector_discovery_skipped_cycles_total")
	_, result.gauge, result.gaugeSeen = discoveryMetric(t, reader, "eshu_dp_collector_discovery_consecutive_failures")
	result.listing, result.okListings = github.counts()
	return result
}

// TestDiscoveryOutageDoesNotEndTheIngesterProcess is the #8084 end-to-end
// proof. QA saw 5 consecutive failing discoveries in 2 min 17 s then recovery,
// and 3 failures in 34 s then a clean start; each ended the whole ingester
// process before. Here the same sequences run through the real source,
// selector, collector loop, and supervisor against a fake GitHub: the run
// never returns before shutdown, the projector is never cancelled, the
// consecutive gauge returns to 0, and the skipped-cycle counter equals the
// failure count. The 20-failure sequence must still end in a process-level
// error, so a permanent fault stays a crash loop.
func TestDiscoveryOutageDoesNotEndTheIngesterProcess(t *testing.T) {
	t.Parallel()

	for _, failures := range []int{5, 3} {
		t.Run(fmt.Sprintf("%d failing cycles then recovery", failures), func(t *testing.T) {
			t.Parallel()
			got := runDiscoveryRetry(t, failures)
			if got.err != nil {
				t.Fatalf("run error = %v, want nil on shutdown", got.err)
			}
			if got.returnedEarly {
				t.Fatal("run returned before shutdown")
			}
			if got.projectorCut {
				t.Fatal("projector context was cancelled during the outage")
			}
			if got.skipped != int64(failures) {
				t.Fatalf("skipped cycles = %d, want %d", got.skipped, failures)
			}
			if !got.gaugeSeen || got.gauge != 0 {
				t.Fatalf("consecutive failures gauge = %d (seen %v), want 0 after the first good listing", got.gauge, got.gaugeSeen)
			}
			if got.okListings < 1 {
				t.Fatal("the listing never recovered")
			}
			if want := failures*discoveryRetryAttemptsPerCycle + got.okListings; got.listing != want {
				t.Fatalf("listing requests = %d, want %d (in-call retry of every failing cycle)", got.listing, want)
			}
			for i, wait := range got.waits[:failures] {
				if wait < 3750*time.Millisecond || wait > 75*time.Second {
					t.Fatalf("skip wait %d = %v, want the jittered 5 s doubling backoff", i+1, wait)
				}
			}
		})
	}

	t.Run("20 failing cycles end in a process-level error", func(t *testing.T) {
		t.Parallel()
		got := runDiscoveryRetry(t, 20)
		if got.err == nil {
			t.Fatal("run error = nil, want the fatal cap error")
		}
		if collector.IsRetryable(got.err) {
			t.Fatalf("cap error %v is retryable, want fatal", got.err)
		}
		for _, fragment := range []string{"collect scope generation", "20 consecutive", "server_error"} {
			if !strings.Contains(got.err.Error(), fragment) {
				t.Fatalf("cap error %q does not name %q", got.err, fragment)
			}
		}
		if got.skipped != 19 {
			t.Fatalf("skipped cycles = %d, want 19 before the fatal 20th", got.skipped)
		}
		if got.okListings != 0 {
			t.Fatalf("successful listings = %d, want 0", got.okListings)
		}
	})
}
