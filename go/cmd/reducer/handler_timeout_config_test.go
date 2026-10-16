// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestLoadReducerHandlerTimeoutDefaultsToZero(t *testing.T) {
	t.Parallel()

	if got, err := loadReducerHandlerTimeout(func(string) string { return "" }); err != nil || got != 0 {
		t.Fatalf("loadReducerHandlerTimeout() = %v, %v; want 0, nil (no bound configured)", got, err)
	}
	if got, err := loadReducerHandlerTimeout(nil); err != nil || got != 0 {
		t.Fatalf("loadReducerHandlerTimeout(nil) = %v, %v; want 0, nil", got, err)
	}
}

func TestLoadReducerHandlerTimeoutReadsDuration(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		if name == reducerHandlerTimeoutEnv {
			return "10m"
		}
		return ""
	}
	if got, err := loadReducerHandlerTimeout(getenv); err != nil || got != 10*time.Minute {
		t.Fatalf("loadReducerHandlerTimeout() = %v, %v; want %v, nil", got, err, 10*time.Minute)
	}
}

func TestLoadReducerHandlerTimeoutRejectsBadValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"not-a-duration", "0s", "-5m", "10mm"} {
		getenv := func(string) string { return raw }
		if got, err := loadReducerHandlerTimeout(getenv); err == nil {
			t.Fatalf("loadReducerHandlerTimeout(%q) = %v, want error", raw, got)
		}
	}
}

func TestLoadReducerHandlerTimeoutOverridesParsesDomains(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		if name == reducerHandlerTimeoutOverridesEnv {
			return "workload_materialization=10m, service_catalog_correlation=5m"
		}
		return ""
	}
	overrides, err := loadReducerHandlerTimeoutOverrides(getenv)
	if err != nil {
		t.Fatalf("loadReducerHandlerTimeoutOverrides() error = %v, want nil", err)
	}
	if got, want := overrides[reducer.DomainWorkloadMaterialization], 10*time.Minute; got != want {
		t.Fatalf("override workload_materialization = %v, want %v", got, want)
	}
	if got, want := overrides[reducer.DomainServiceCatalogCorrelation], 5*time.Minute; got != want {
		t.Fatalf("override service_catalog_correlation = %v, want %v", got, want)
	}
}

func TestLoadReducerHandlerTimeoutOverridesEmpty(t *testing.T) {
	t.Parallel()

	overrides, err := loadReducerHandlerTimeoutOverrides(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadReducerHandlerTimeoutOverrides() error = %v, want nil", err)
	}
	if len(overrides) != 0 {
		t.Fatalf("overrides = %v, want empty", overrides)
	}
}

func TestLoadReducerHandlerTimeoutOverridesRejectsBadEntries(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"no-equals-here",
		"not_a_domain=5m",
		"workload_materialization=forever",
		"workload_materialization=0s",
		"workload_materialization=5m,",
		"workload_materialization=5m,workload_materialization=10m",
	} {
		getenv := func(string) string { return raw }
		if overrides, err := loadReducerHandlerTimeoutOverrides(getenv); err == nil {
			t.Fatalf("loadReducerHandlerTimeoutOverrides(%q) = %v, want error", raw, overrides)
		}
	}
}
