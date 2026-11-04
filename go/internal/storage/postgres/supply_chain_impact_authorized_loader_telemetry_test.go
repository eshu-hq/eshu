// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestAuthorizedLoaderStaticExpansionCarriesTelemetry is the #9991 F2
// regression: the impact read swapped in a bare FactStore, so the manifest
// dependency read's histograms, delta-union counter, and WARN lines never
// recorded on the path #9991 was filed for. The FactStore the loader hands
// its static expansion must carry the wired instruments and logger.
func TestAuthorizedLoaderStaticExpansionCarriesTelemetry(t *testing.T) {
	t.Parallel()

	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	logger := slog.New(slog.DiscardHandler)
	loader := NewAuthorizedSupplyChainImpactEvidenceLoaderWithTelemetry(disengagedPolicyDB{}, instruments, logger)
	var seen *FactStore
	if _, err := loader.ListAuthorizedSupplyChainImpactEvidence(
		context.Background(),
		reducercontract.Intent{ScopeID: "scope", GenerationID: "gen", Domain: reducercontract.DomainSupplyChainImpact},
		func(_ context.Context, factLoader factload.FactLoader, _ []facts.Envelope) (supplychaincore.AuthorizedStaticSupplyChainImpactLoad, error) {
			seen, _ = factLoader.(*FactStore)
			return supplychaincore.AuthorizedStaticSupplyChainImpactLoad{Complete: true}, nil
		},
	); err != nil {
		t.Fatalf("ListAuthorizedSupplyChainImpactEvidence() error = %v", err)
	}
	if seen == nil {
		t.Fatal("static expansion FactLoader is not a *FactStore")
	}
	if seen.Instruments() != instruments || seen.Logger() != logger {
		t.Fatalf("static expansion FactStore instruments = %p logger = %p, want the wired ones", seen.Instruments(), seen.Logger())
	}
}
