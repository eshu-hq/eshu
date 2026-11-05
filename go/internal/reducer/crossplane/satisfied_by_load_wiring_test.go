// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplane_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossplane"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The production loader must keep the one-snapshot read: if its signature
// drifted, loadEdgeFacts would silently take the generation-keyed fallback,
// whose delta-only rows the scope retract turns into deleted edges (#10005).
var _ crossplane.SatisfiedBySnapshotLoaderForTest = (*postgres.FactStore)(nil)

// TestProductionFactStoreOffersTheSnapshotRead pins the same contract through
// the type assertion loadEdgeFacts makes, on the store cmd/reducer wires
// (newReducerFactStore: NewFactStore, WithInstruments, WithLogger).
func TestProductionFactStoreOffersTheSnapshotRead(t *testing.T) {
	t.Parallel()

	var loader factload.FactLoader = postgres.NewFactStore(nil).WithInstruments(nil).WithLogger(nil)
	if _, ok := loader.(crossplane.SatisfiedBySnapshotLoaderForTest); !ok {
		t.Fatalf("%T does not implement the snapshot read; the handler would fall back to the destructive generation-keyed read", loader)
	}
}
