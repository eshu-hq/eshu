// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestIngesterReducerIntentWriterSkipsAdmissionInLocalLightweight proves the
// ingester-only local-lightweight bypass (ESHU_QUERY_PROFILE=local_lightweight
// or ESHU_DISABLE_NEO4J=true) short-circuits before the shared
// internal/reduceradmission gate ever wraps the writer. bootstrap-index has no
// equivalent bypass; this behavior must stay local to the ingester.
func TestIngesterReducerIntentWriterSkipsAdmissionInLocalLightweight(t *testing.T) {
	t.Parallel()

	writer, err := ingesterReducerIntentWriter(
		postgres.SQLDB{},
		mapGetenv(map[string]string{
			"ESHU_QUERY_PROFILE":                     "local_lightweight",
			"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "100",
		}),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ingesterReducerIntentWriter() error = %v, want nil", err)
	}
	if _, ok := writer.(lightweightReducerIntentWriter); !ok {
		t.Fatalf("writer type = %T, want lightweightReducerIntentWriter", writer)
	}
}

// TestIngesterReducerIntentWriterWrapsAdmissionOutsideLocalLightweight proves
// that outside the local-lightweight profile, the ingester still applies the
// shared reduceradmission gate around the real reducer queue writer.
func TestIngesterReducerIntentWriterWrapsAdmissionOutsideLocalLightweight(t *testing.T) {
	t.Parallel()

	writer, err := ingesterReducerIntentWriter(
		postgres.SQLDB{},
		mapGetenv(map[string]string{
			"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "100",
		}),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ingesterReducerIntentWriter() error = %v, want nil", err)
	}
	if _, ok := writer.(lightweightReducerIntentWriter); ok {
		t.Fatal("writer type = lightweightReducerIntentWriter, want admission-wrapped real writer")
	}
}
