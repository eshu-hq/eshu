// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestIngesterAdmissionIntentWriterSkipsLocalLightweight proves the
// deployment-profile contract: the local lightweight profile has no real
// reducer queue behind it, so it keeps the profile writer unwrapped
// instead of gating against a database that is not there.
func TestIngesterAdmissionIntentWriterSkipsLocalLightweight(t *testing.T) {
	t.Parallel()

	writer, err := ingesterAdmissionIntentWriter(
		postgres.SQLDB{},
		mapGetenv(map[string]string{
			"ESHU_QUERY_PROFILE":                     "local_lightweight",
			"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "100",
		}),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ingesterAdmissionIntentWriter() error = %v, want nil", err)
	}
	if _, ok := writer.(lightweightReducerIntentWriter); !ok {
		t.Fatalf("writer type = %T, want lightweightReducerIntentWriter", writer)
	}
}

// TestIngesterAdmissionIntentWriterDisabledReturnsInner proves a fully
// disabled gate returns the profile inner writer unwrapped: with no
// thresholds set, admission adds no behavior.
func TestIngesterAdmissionIntentWriterDisabledReturnsInner(t *testing.T) {
	t.Parallel()

	writer, err := ingesterAdmissionIntentWriter(
		postgres.SQLDB{},
		mapGetenv(map[string]string{}),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ingesterAdmissionIntentWriter() error = %v, want nil", err)
	}
	if writer == nil {
		t.Fatal("writer = nil, want the profile inner writer")
	}
	if _, ok := writer.(lightweightReducerIntentWriter); ok {
		t.Fatal("writer is the lightweight writer without the profile set")
	}
}
