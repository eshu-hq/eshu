// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestPagerDutyCommittersCarryInstruments proves the claimed PagerDuty runtime
// path hands the shared ingestion committer the process instruments, so the
// generation outcome counter (#10020) records minted and skipped generations
// for the pagerduty collector kind instead of silently recording nothing. The
// cassette path sets the same field in buildCassetteService but is not
// asserted here, because building it needs a cassette file.
func TestPagerDutyCommittersCarryInstruments(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	claimed, err := buildClaimedService(nil, claimStoreTestGetenv, nil, instruments, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	claimedStore, ok := claimed.Committer.(postgres.IngestionStore)
	if !ok {
		t.Fatalf("claimed Committer = %T, want postgres.IngestionStore", claimed.Committer)
	}
	if claimedStore.Instruments != instruments {
		t.Fatal("claimed committer Instruments is not the process instruments")
	}
}
