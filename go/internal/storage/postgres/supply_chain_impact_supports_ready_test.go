// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
)

// TestSupplyChainImpactSupportsReadyPredicate pins the readiness answers a
// grant-scoped reader acts on: a completed marker at the current version is
// ready, anything else is not, and a failed read (a missing table among them)
// is an error the reader must treat as not ready, never as ready.
func TestSupplyChainImpactSupportsReadyPredicate(t *testing.T) {
	t.Parallel()
	missingTable := errors.New(`relation "supply_chain_impact_finding_supports_backfill" does not exist`)
	for name, tc := range map[string]struct {
		response  fakeRows
		wantReady bool
		wantErr   error
	}{
		"completed at the current version": {response: fakeRows{rows: [][]any{{true}}}, wantReady: true},
		"marker incomplete or behind":      {response: fakeRows{rows: [][]any{{false}}}},
		"no row":                           {response: fakeRows{}},
		"missing table":                    {response: fakeRows{err: missingTable}, wantErr: missingTable},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			queryer := &fakeQueryer{responses: []fakeRows{tc.response}}
			ready, err := SupplyChainImpactSupportsReady(context.Background(), queryer)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if ready != tc.wantReady {
				t.Fatalf("ready = %v, want %v", ready, tc.wantReady)
			}
			if len(queryer.args) != 1 || len(queryer.args[0]) != 2 ||
				queryer.args[0][0] != SupplyChainImpactSupportsBackfillMarker ||
				queryer.args[0][1] != SupplyChainImpactSupportsContractVersion {
				t.Fatalf("query args = %v, want the marker name and the contract version", queryer.args)
			}
		})
	}
}
