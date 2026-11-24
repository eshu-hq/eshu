// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// Wiring proofs for the #10416 slice-1 winners-audit split scan: the reducer
// opens a stale reader access only when ESHU_POSTGRES_READ_ROUTING admits
// stale audits. Default off keeps today's writer-backed path: no access is
// opened, so the Auditor stays exactly as built before.

func staleWiringGetenv(routing string) func(string) string {
	return func(key string) string {
		if key == "ESHU_POSTGRES_READ_ROUTING" {
			return routing
		}
		return ""
	}
}

func TestOpenWinnersStaleAccessDefaultsOff(t *testing.T) {
	t.Parallel()

	for _, routing := range []string{"", "off"} {
		access, cleanup, err := openWinnersStaleAccessIfEnabled(context.Background(), staleWiringGetenv(routing), nil, nil, nil)
		if err != nil {
			t.Fatalf("routing %q: error = %v, want nil", routing, err)
		}
		if access != nil {
			t.Fatalf("routing %q: opened an access, want nil: default off keeps today's path", routing)
		}
		if cleanup == nil {
			t.Fatalf("routing %q: nil cleanup", routing)
		}
		cleanup()
	}
}

func TestOpenWinnersStaleAccessRejectsBogusRouting(t *testing.T) {
	t.Parallel()

	access, _, err := openWinnersStaleAccessIfEnabled(context.Background(), staleWiringGetenv("sometimes"), nil, nil, nil)
	if err == nil {
		t.Fatalf("bogus routing: error = nil, want a startup failure, never a silent off")
	}
	if access != nil {
		t.Fatalf("bogus routing: opened an access despite the error")
	}
}

func TestOpenWinnersStaleAccessAttemptsOpenWhenEnabled(t *testing.T) {
	t.Parallel()

	// The DSN parses but dials a closed port, so config load succeeds and a
	// failure can only come from inside the access open: Open bootstraps the
	// physical writer identity with a dial, and the refused dial surfaces as
	// "writer identity unavailable". (An unset DSN would fail earlier in
	// config load and prove nothing about Open.)
	getenv := func(key string) string {
		switch key {
		case "ESHU_POSTGRES_READ_ROUTING":
			return "stale-audits"
		case "ESHU_POSTGRES_DSN":
			return "postgresql://127.0.0.1:1/eshu?connect_timeout=2"
		}
		return ""
	}
	meter := noopmetric.NewMeterProvider().Meter("winners-stale-wiring")
	tracer := nooptrace.NewTracerProvider().Tracer("winners-stale-wiring")
	access, _, err := openWinnersStaleAccessIfEnabled(context.Background(), getenv, meter, tracer, nil)
	if err == nil {
		t.Fatalf("stale-audits with a closed-port DSN: error = nil, want the open to fail")
	}
	if access != nil {
		t.Fatalf("stale-audits with a closed-port DSN: opened an access despite the error")
	}
	if !strings.Contains(err.Error(), "writer identity unavailable") {
		t.Fatalf("stale-audits open error = %v, want the Open bootstrap dial failure proving Open was reached", err)
	}
}

func TestOpenWinnersStaleAccessAdmitsStagedMode(t *testing.T) {
	t.Parallel()

	// staged-audits opens the same stale access as stale-audits: the mode
	// selects the staged read phase, not a different connection.
	getenv := func(key string) string {
		switch key {
		case "ESHU_POSTGRES_READ_ROUTING":
			return "staged-audits"
		case "ESHU_POSTGRES_DSN":
			return "postgresql://127.0.0.1:1/eshu?connect_timeout=2"
		}
		return ""
	}
	meter := noopmetric.NewMeterProvider().Meter("winners-stale-wiring")
	tracer := nooptrace.NewTracerProvider().Tracer("winners-stale-wiring")
	access, _, err := openWinnersStaleAccessIfEnabled(context.Background(), getenv, meter, tracer, nil)
	if err == nil {
		t.Fatal("staged-audits with a closed-port DSN: error = nil, want the open to fail")
	}
	if access != nil {
		t.Fatal("staged-audits with a closed-port DSN: opened an access despite the error")
	}
	if !winnersStagedAuditRouting(staleWiringGetenv("staged-audits")) {
		t.Fatal("staged-audits must select the staged read phase")
	}
	if winnersStagedAuditRouting(staleWiringGetenv("stale-audits")) {
		t.Fatal("stale-audits must keep the split read phase")
	}
	if winnersStagedAuditRouting(staleWiringGetenv("off")) {
		t.Fatal("off must not select the staged read phase")
	}
}

// TestOpenWinnersStaleAccessRetriesReaderUnavailableBounded is the #11306
// regression test: a reader that is briefly unreachable must not fail the
// open fast. The first two attempts fail with the reader-unavailable
// sentinel, the third succeeds, and the wrapper must ride out the outage.
// A reader that stays unreachable past the bound must stop retrying and
// fail loudly with the same prefix, never silently fall back to the writer.
func TestOpenWinnersStaleAccessRetriesReaderUnavailableBounded(t *testing.T) {
	t.Parallel()

	transientAttempts := 0
	transientOpen := func(context.Context, pgaccess.Config, pgaccess.Observer) (*pgaccess.Access, error) {
		transientAttempts++
		if transientAttempts <= 2 {
			return nil, fmt.Errorf("replica restarting: %w", db.ErrReaderUnavailable)
		}
		return nil, nil
	}
	if _, err := openWinnersStaleAccessWithRetry(context.Background(), pgaccess.Config{}, nil, transientOpen, nil); err != nil {
		t.Fatalf("transient reader outage: error = %v, want nil after the reader recovers", err)
	}
	if transientAttempts != 3 {
		t.Fatalf("transient reader outage: attempts = %d, want 3 (two retries, then success)", transientAttempts)
	}

	pastBound, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	boundAttempts := 0
	alwaysDown := func(context.Context, pgaccess.Config, pgaccess.Observer) (*pgaccess.Access, error) {
		boundAttempts++
		return nil, fmt.Errorf("replica down: %w", db.ErrReaderUnavailable)
	}
	_, err := openWinnersStaleAccessWithRetry(pastBound, pgaccess.Config{}, nil, alwaysDown, nil)
	if err == nil {
		t.Fatal("past-bound reader outage: error = nil, want a loud failure, never a silent fallback")
	}
	if !strings.Contains(err.Error(), "supply chain impact winners stale access") {
		t.Fatalf("past-bound reader outage: error = %v, want the stale-access prefix so the exit matches today's failure", err)
	}
	if boundAttempts < 2 {
		t.Fatalf("past-bound reader outage: attempts = %d, want at least 2, proving the wrapper retried before giving up", boundAttempts)
	}
}

// TestOpenWinnersStaleAccessLogsRetrySignal pins the #11306 operator signal:
// every wait logs its attempt and reason, so the startup wait and its cause
// stay visible while the status server reports not-ready.
func TestOpenWinnersStaleAccessLogsRetrySignal(t *testing.T) {
	t.Parallel()

	var buf lockedBuffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	attempts := 0
	flaky := func(context.Context, pgaccess.Config, pgaccess.Observer) (*pgaccess.Access, error) {
		attempts++
		if attempts == 1 {
			return nil, fmt.Errorf("replica restarting: %w", db.ErrReaderUnavailable)
		}
		return nil, nil
	}
	if _, err := openWinnersStaleAccessWithRetry(context.Background(), pgaccess.Config{}, nil, flaky, logger); err != nil {
		t.Fatalf("transient reader outage: error = %v, want nil after the reader recovers", err)
	}
	out := buf.String()
	if !strings.Contains(out, "winners stale access waiting for reader") {
		t.Fatalf("retry wait: log = %q, want the waiting message", out)
	}
	for _, want := range []string{`"attempt":1`, `"retry_in":250000000`, `"reason":`} {
		if !strings.Contains(out, want) {
			t.Fatalf("retry wait: log = %q, want field %s", out, want)
		}
	}
}

// TestWinnersStaleAccessRoutingError pins the build-time fail-fast: a bogus
// routing mode is a startup error naming the value, never a silent
// writer-backed run. Off and the two admitted modes pass validation.
func TestWinnersStaleAccessRoutingError(t *testing.T) {
	t.Parallel()

	for routing, wantErr := range map[string]bool{
		"":              false,
		"off":           false,
		"stale-audits":  false,
		"staged-audits": false,
		"sometimes":     true,
		"STALE-AUDITS":  true,
	} {
		err := winnersStaleAccessRoutingError(staleWiringGetenv(routing))
		if (err != nil) != wantErr {
			t.Fatalf("routing %q: error = %v, want error %v (bogus modes must fail at build, never serve writer-backed)", routing, err, wantErr)
		}
	}
	if err := winnersStaleAccessRoutingError(staleWiringGetenv("sometimes")); err == nil ||
		!strings.Contains(err.Error(), `"sometimes"`) {
		t.Fatalf("bogus routing: error = %v, want the offending value named", err)
	}
}

// TestOpenWinnersStaleAccessAdmittedModes pins which routing modes wait for
// the reader. Only the two admitted scans gate readiness; off stays
// writer-backed and bogus stays a startup failure when opened.
func TestOpenWinnersStaleAccessAdmittedModes(t *testing.T) {
	t.Parallel()

	for routing, want := range map[string]bool{
		"":              false,
		"off":           false,
		"stale-audits":  true,
		"staged-audits": true,
		"sometimes":     false,
	} {
		if got := winnersStaleAccessAdmitted(staleWiringGetenv(routing)); got != want {
			t.Fatalf("routing %q: admitted = %v, want %v", routing, got, want)
		}
	}
}

// stubWinnersRunner records Run calls for the deferred-gate tests.
type stubWinnersRunner struct {
	ran int
	err error
}

func (s *stubWinnersRunner) Run(context.Context) error {
	s.ran++
	return s.err
}

// TestOpenWinnersStaleAccessGateProbe pins the /readyz contract: the probe
// passes when routing never admitted the scans, fails while an admitted gate
// is still waiting, and passes once the access is attached.
func TestOpenWinnersStaleAccessGateProbe(t *testing.T) {
	t.Parallel()

	off := newWinnersStaleAccessDeferred(false, nil, nil)
	if err := off.readinessProbe().Check(context.Background()); err != nil {
		t.Fatalf("routing off: probe = %v, want ready (nothing to wait for)", err)
	}

	waiting := newWinnersStaleAccessDeferred(true, nil, nil)
	if err := waiting.readinessProbe().Check(context.Background()); err == nil {
		t.Fatal("admitted gate before attach: probe = nil, want not-ready while waiting for the reader")
	}
	waiting.ready.Store(true)
	if err := waiting.readinessProbe().Check(context.Background()); err != nil {
		t.Fatalf("admitted gate after attach: probe = %v, want ready", err)
	}
}

// TestOpenWinnersStaleAccessRunnerDefersOpen pins the startup order: the
// Runner opens (with retry) before the inner service runs, skips the attach
// when there is no access, and returns the loud open error without running
// the inner service when the reader stays unreachable.
func TestOpenWinnersStaleAccessRunnerDefersOpen(t *testing.T) {
	t.Parallel()

	opened := 0
	noop := func() {}
	off := newWinnersStaleAccessDeferred(false,
		func(context.Context) (*pgaccess.Access, func(), error) {
			opened++
			return nil, noop, nil
		},
		func(*pgaccess.Access) error {
			t.Error("attach must not run when routing is off")
			return nil
		},
	)
	inner := &stubWinnersRunner{}
	if err := off.wrapRunner(inner, nil).Run(context.Background()); err != nil {
		t.Fatalf("routing off: Run = %v, want nil", err)
	}
	if opened != 0 || inner.ran != 1 {
		t.Fatalf("routing off: opened = %d, inner runs = %d, want 0 and 1 (straight passthrough, no I/O)", opened, inner.ran)
	}

	// A nil access skips the attach: only routing off opens nothing, and a
	// non-nil access needs a live backend, so the attach body itself is
	// covered by review and the existing live-gated suites.
	admitted := newWinnersStaleAccessDeferred(true,
		func(context.Context) (*pgaccess.Access, func(), error) {
			return nil, noop, nil
		},
		func(*pgaccess.Access) error {
			t.Error("attach must not run without an access")
			return nil
		},
	)
	ready := &stubWinnersRunner{}
	if err := admitted.wrapRunner(ready, nil).Run(context.Background()); err != nil {
		t.Fatalf("admitted gate, nil access: Run = %v, want nil", err)
	}
	if ready.ran != 1 {
		t.Fatalf("admitted gate, nil access: inner runs = %d, want 1", ready.ran)
	}
	if err := admitted.readinessProbe().Check(context.Background()); err != nil {
		t.Fatalf("admitted gate after Run: probe = %v, want ready", err)
	}

	loud := fmt.Errorf("supply chain impact winners stale access: %w", fmt.Errorf("replica down: %w", db.ErrReaderUnavailable))
	failing := newWinnersStaleAccessDeferred(true,
		func(context.Context) (*pgaccess.Access, func(), error) {
			return nil, nil, loud
		},
		func(*pgaccess.Access) error {
			t.Error("attach must not run when the open failed")
			return nil
		},
	)
	blocked := &stubWinnersRunner{}
	if err := failing.wrapRunner(blocked, nil).Run(context.Background()); err == nil {
		t.Fatal("admitted gate, open failed: Run = nil, want the loud error, never a silent start")
	} else if err.Error() != loud.Error() {
		t.Fatalf("admitted gate, open failed: Run = %v, want the same wrapped error %v", err, loud)
	}
	if blocked.ran != 0 {
		t.Fatalf("admitted gate, open failed: inner runs = %d, want 0 (no service without the reader)", blocked.ran)
	}
	if err := failing.readinessProbe().Check(context.Background()); err == nil {
		t.Fatal("admitted gate, open failed: probe = nil, want not-ready")
	}
}

// TestOpenWinnersStaleAccessRunnerServesAfterRecovery is the #11306
// startup-level proof at unit scope: with the reader down the Runner waits
// (probe not-ready, no restart, no serve), and once the reader recovers the
// same Run establishes access and serves (inner runs, probe ready). A real
// process boot against a live replica stays NOT_CHECKED and rides the CI
// live legs.
func TestOpenWinnersStaleAccessRunnerServesAfterRecovery(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	noop := func() {}
	recovering := newWinnersStaleAccessDeferred(true,
		func(context.Context) (*pgaccess.Access, func(), error) {
			<-release
			return nil, noop, nil
		},
		func(*pgaccess.Access) error {
			t.Error("attach must not run without an access")
			return nil
		},
	)
	served := &stubWinnersRunner{}
	done := make(chan error, 1)
	go func() { done <- recovering.wrapRunner(served, nil).Run(context.Background()) }()

	if err := recovering.readinessProbe().Check(context.Background()); err == nil {
		t.Fatal("reader down: probe = nil, want not-ready while the Runner waits (no serve before the reader)")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reader recovered: Run = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reader recovered: Run did not return within 10s")
	}
	if served.ran != 1 {
		t.Fatalf("reader recovered: inner runs = %d, want 1 (same process serves, no restart)", served.ran)
	}
	if err := recovering.readinessProbe().Check(context.Background()); err != nil {
		t.Fatalf("reader recovered: probe = %v, want ready", err)
	}
}

// TestOpenWinnersStaleAccessRunnerShutdownDuringWait pins the #11346
// shutdown semantics: a parent cancel during the bounded wait is a clean
// shutdown (nil), not a startup failure. Budget expiry on a live parent
// keeps the loud error (covered by the bounded test).
func TestOpenWinnersStaleAccessRunnerShutdownDuringWait(t *testing.T) {
	t.Parallel()

	var buf lockedBuffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	alwaysDown := func(context.Context, pgaccess.Config, pgaccess.Observer) (*pgaccess.Access, error) {
		return nil, fmt.Errorf("replica down: %w", db.ErrReaderUnavailable)
	}
	shutting := newWinnersStaleAccessDeferred(true,
		func(openCtx context.Context) (*pgaccess.Access, func(), error) {
			access, err := openWinnersStaleAccessWithRetry(openCtx, pgaccess.Config{}, nil, alwaysDown, logger)
			if err != nil {
				return nil, nil, err
			}
			return access, func() {}, nil
		},
		func(*pgaccess.Access) error {
			t.Error("attach must not run when the open never succeeded")
			return nil
		},
	)
	served := &stubWinnersRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- shutting.wrapRunner(served, logger).Run(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), "winners stale access waiting for reader") {
		if time.Now().After(deadline) {
			t.Fatal("reader down: no wait logged within 10s, the Runner never entered the retry")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := shutting.readinessProbe().Check(context.Background()); err == nil {
		t.Fatal("reader down: probe = nil, want not-ready while the Runner waits")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown during wait: Run = %v, want nil (clean shutdown, not a startup failure)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown during wait: Run did not return within 10s of cancel")
	}
	if served.ran != 0 {
		t.Fatalf("shutdown during wait: inner runs = %d, want 0", served.ran)
	}
	if err := shutting.readinessProbe().Check(context.Background()); err == nil {
		t.Fatal("shutdown during wait: probe = nil, want not-ready (access never established)")
	}
	if out := buf.String(); !strings.Contains(out, "winners stale access wait canceled by shutdown") {
		t.Fatalf("shutdown during wait: log = %q, want the distinct shutdown line", out)
	}
}

// TestOpenWinnersStaleAccessDoesNotRetryOtherErrors guards the retry
// predicate: only errors.Is(db.ErrReaderUnavailable) retries. A dead writer
// or bad config must fail on the first attempt, exactly as the single-shot
// open did.
func TestOpenWinnersStaleAccessDoesNotRetryOtherErrors(t *testing.T) {
	t.Parallel()

	attempts := 0
	writerDown := func(context.Context, pgaccess.Config, pgaccess.Observer) (*pgaccess.Access, error) {
		attempts++
		return nil, fmt.Errorf("writer bootstrap: connection refused")
	}
	_, err := openWinnersStaleAccessWithRetry(context.Background(), pgaccess.Config{}, nil, writerDown, nil)
	if err == nil {
		t.Fatal("non-retryable open error: error = nil, want the failure to surface")
	}
	if attempts != 1 {
		t.Fatalf("non-retryable open error: attempts = %d, want 1 (no retry without the reader sentinel)", attempts)
	}
}
