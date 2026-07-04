// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildBootstrapProjectorWrapsReducerIntentWriterWithAdmission proves
// bootstrap-index's reducer intent writer is wrapped with the shared
// internal/reduceradmission gate (issue #4515 parity): before this change,
// bootstrap-index assigned the raw postgres.ReducerQueue directly as
// IntentWriter with no backpressure at all, unlike the ingester. The default
// getenv (all unset) enables both gates, so the wrapped writer differs from
// the raw postgres.ReducerQueue's own type.
func TestBuildBootstrapProjectorWrapsReducerIntentWriterWithAdmission(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapProjector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		&noopCanonicalWriter{},
		func(string) string { return "" },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapProjector() error = %v, want nil", err)
	}

	runtime, ok := deps.runner.(projector.Runtime)
	if !ok {
		t.Fatalf("buildBootstrapProjector() runner type = %T, want projector.Runtime", deps.runner)
	}
	if runtime.IntentWriter == nil {
		t.Fatal("buildBootstrapProjector() IntentWriter = nil, want admission-wrapped writer")
	}
	if _, ok := runtime.IntentWriter.(postgres.ReducerQueue); ok {
		t.Fatal("buildBootstrapProjector() IntentWriter type = postgres.ReducerQueue, want reduceradmission-wrapped writer")
	}
}

// TestBuildBootstrapProjectorReducerAdmissionDisabledReturnsRawQueue proves
// that explicitly disabling both admission gates via getenv returns the raw
// postgres.ReducerQueue unwrapped, matching the no-op contract documented in
// internal/reduceradmission.
func TestBuildBootstrapProjectorReducerAdmissionDisabledReturnsRawQueue(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		switch key {
		case "ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK", "ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK":
			return "0"
		default:
			return ""
		}
	}

	deps, err := buildBootstrapProjector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		&noopCanonicalWriter{},
		getenv,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapProjector() error = %v, want nil", err)
	}

	runtime, ok := deps.runner.(projector.Runtime)
	if !ok {
		t.Fatalf("buildBootstrapProjector() runner type = %T, want projector.Runtime", deps.runner)
	}
	if _, ok := runtime.IntentWriter.(postgres.ReducerQueue); !ok {
		t.Fatalf("buildBootstrapProjector() IntentWriter type = %T, want postgres.ReducerQueue when both gates disabled", runtime.IntentWriter)
	}
}
