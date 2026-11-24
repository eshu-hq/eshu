// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/gomodules"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestSupportsReadyBindingCarriesThePinnedPredicate pins the binding the Go-modules
// fence embeds in its holder statement (#11197): SQL is the readiness constant
// itself, Args are its two parameters in order (marker, then contract version), and
// the fence's composed holder statement contains that constant, in parentheses,
// exactly once.
func TestSupportsReadyBindingCarriesThePinnedPredicate(t *testing.T) {
	t.Parallel()

	binding := NewSupplyChainImpactSupportsReadyBinding(nil, telemetry.SupplyChainImpactSupportsReaderGoModulesFence)
	if binding.SQL != supplyChainImpactSupportsReadySQL {
		t.Fatalf("binding SQL differs from supplyChainImpactSupportsReadySQL: %q", binding.SQL)
	}
	wantArgs := []any{SupplyChainImpactSupportsBackfillMarker, SupplyChainImpactSupportsContractVersion}
	if len(binding.Args) != 2 || !slices.Equal(binding.Args, wantArgs) {
		t.Fatalf("binding Args = %v, want the marker then the contract version %v", binding.Args, wantArgs)
	}
	statement := gomodules.HolderStatement(binding.SQL)
	if got := strings.Count(statement, "("+supplyChainImpactSupportsReadySQL+")"); got != 1 {
		t.Fatalf("holder statement embeds the readiness constant %d times, want 1", got)
	}
}

// TestSupportsReadyBindingObserveClassifiesLikeTheSharedReaders pins Observe: ready
// records nothing and returns nil, not ready returns the typed non-counting refusal
// and counts not_ready under the fence's label, and a probe error counts failed and
// never becomes the refusal.
func TestSupportsReadyBindingObserveClassifiesLikeTheSharedReaders(t *testing.T) {
	t.Parallel()

	inst, reader := supportsReaderMetrics(t)
	observe := NewSupplyChainImpactSupportsReadyBinding(inst, telemetry.SupplyChainImpactSupportsReaderGoModulesFence).Observe
	if err := observe(context.Background(), true, nil); err != nil {
		t.Fatalf("ready Observe() = %v, want nil", err)
	}
	var notReady *supplychaincore.SupplyChainImpactSupportsNotReadyError
	if err := observe(context.Background(), false, nil); !errors.As(err, &notReady) || notReady.Reader != telemetry.SupplyChainImpactSupportsReaderGoModulesFence {
		t.Fatalf("not-ready Observe() = %v, want the typed refusal for go_modules_fence", err)
	}
	probe := errors.New("probe boom")
	if err := observe(context.Background(), false, probe); !errors.Is(err, probe) || errors.As(err, new(*supplychaincore.SupplyChainImpactSupportsNotReadyError)) {
		t.Fatalf("probe-error Observe() = %v, want the probe error and never the refusal", err)
	}
	if got := supportsReadSeries(t, reader, telemetry.SupplyChainImpactSupportsReaderGoModulesFence, telemetry.SupplyChainImpactSupportsReadNotReady); got != 1 {
		t.Fatalf("not_ready = %d, want 1", got)
	}
	if got := supportsReadSeries(t, reader, telemetry.SupplyChainImpactSupportsReaderGoModulesFence, telemetry.SupplyChainImpactSupportsReadFailed); got != 1 {
		t.Fatalf("failed = %d, want 1", got)
	}
}
