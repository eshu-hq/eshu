// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"testing"
)

// TestFanOutNeverReopensAnInFlightManifestMarker pins #10972: a findings
// fan-out reads the in-flight scopes, and a scope whose claimed item is a
// manifest marker is not one of them. A marker loads no evidence and writes no
// finding, so reopening it would set the replay bit the queue turns into a
// pending re-run at the marker's ack. The claimed findings pass beside it is
// still reopened.
func TestFanOutNeverReopensAnInFlightManifestMarker(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{
		holders:         []string{"holder-a"},
		inFlight:        []string{"in-flight-findings", "in-flight-marker"},
		inFlightMarkers: []string{"in-flight-marker"},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"holder-a", "in-flight-findings"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v: an in-flight manifest marker is never reopened or flagged", port.reopened, want)
	}
}
