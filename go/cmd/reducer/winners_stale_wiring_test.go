// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

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
