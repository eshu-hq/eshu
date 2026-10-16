// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestErrorEnvelopeRetryableReportsTheVerdictMarker pins ErrorEnvelope.Retryable:
// it reports the per-verdict marker that WriteErrorEnvelope reads for
// Retry-After, so a route that writes a verdict in its own body shape sets the
// same header. A transient verdict reports true; a permanent 503, a 504, a
// hand-built envelope, and a nil envelope report false.
func TestErrorEnvelopeRetryableReportsTheVerdictMarker(t *testing.T) {
	t.Parallel()

	verdict := func(err error) *ErrorEnvelope {
		t.Helper()
		_, env, ok := GraphReadErrorEnvelope(err, "c")
		if !ok {
			t.Fatalf("GraphReadErrorEnvelope(%v) claimed nothing", err)
		}
		return env
	}
	for _, tc := range []struct {
		name string
		env  *ErrorEnvelope
		want bool
	}{
		{name: "stale reader", env: verdict(fmt.Errorf("pg: %w", db.ErrReaderStale)), want: true},
		{name: "graph unavailable", env: verdict(ErrGraphUnavailable), want: true},
		{name: "identity store unavailable", env: IdentityStoreUnavailableEnvelope(), want: true},
		{name: "snapshot export refused", env: verdict(db.ErrSnapshotExportUnqualified), want: false},
		{name: "graph deadline", env: verdict(ErrGraphReadDeadline), want: false},
		{name: "hand-built envelope", env: &ErrorEnvelope{Code: ErrorCodeBackendUnavailable}, want: false},
		{name: "nil envelope", env: nil, want: false},
	} {
		if got := tc.env.Retryable(); got != tc.want {
			t.Errorf("%s: Retryable() = %t, want %t", tc.name, got, tc.want)
		}
	}
}
