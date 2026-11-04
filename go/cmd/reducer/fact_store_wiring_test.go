// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestNewReducerFactStoreCarriesInstrumentsAndLogger is the #9991 F1
// regression: the reducer FactStore was built without a logger, so the
// manifest delta-union WARN for a refused base or a scope mismatch never
// reached an operator. Both the plain and the identity-cache stores must carry
// the process instruments and logger.
func TestNewReducerFactStoreCarriesInstrumentsAndLogger(t *testing.T) {
	t.Parallel()

	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	logger := slog.New(slog.DiscardHandler)
	cache, err := postgres.NewIdentityEpochCache(instruments, 0)
	if err != nil {
		t.Fatalf("NewIdentityEpochCache: %v", err)
	}
	for name, cache := range map[string]*postgres.IdentityEpochCache{
		"plain":          nil,
		"identity cache": cache,
	} {
		store := newReducerFactStore(postgres.SQLDB{}, cache, instruments, logger)
		if store.Instruments() != instruments {
			t.Fatalf("%s store instruments = %p, want the process instruments", name, store.Instruments())
		}
		if store.Logger() != logger {
			t.Fatalf("%s store logger = %p, want the process logger", name, store.Logger())
		}
	}
}
