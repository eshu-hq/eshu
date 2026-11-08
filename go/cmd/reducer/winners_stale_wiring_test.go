// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
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

	// No DSN is set, so the open must fail: the failure itself proves the
	// enabled branch reaches the access open instead of silently staying off.
	access, _, err := openWinnersStaleAccessIfEnabled(context.Background(), staleWiringGetenv("stale-audits"), nil, nil, nil)
	if err == nil {
		t.Fatalf("stale-audits without a DSN: error = nil, want the open to fail")
	}
	if access != nil {
		t.Fatalf("stale-audits without a DSN: opened an access despite the error")
	}
}
