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

	if got := loadReducerHandlerTimeout(func(string) string { return "" }); got != 0 {
		t.Fatalf("loadReducerHandlerTimeout() = %v, want 0 (no bound configured)", got)
	}
	if got := loadReducerHandlerTimeout(nil); got != 0 {
		t.Fatalf("loadReducerHandlerTimeout(nil) = %v, want 0", got)
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
	if got, want := loadReducerHandlerTimeout(getenv), 10*time.Minute; got != want {
		t.Fatalf("loadReducerHandlerTimeout() = %v, want %v", got, want)
	}
}

func TestLoadReducerHandlerTimeoutRejectsBadValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"not-a-duration", "0s", "-5m"} {
		getenv := func(string) string { return raw }
		if got := loadReducerHandlerTimeout(getenv); got != 0 {
			t.Fatalf("loadReducerHandlerTimeout(%q) = %v, want 0", raw, got)
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
	} {
		getenv := func(string) string { return raw }
		if overrides, err := loadReducerHandlerTimeoutOverrides(getenv); err == nil {
			t.Fatalf("loadReducerHandlerTimeoutOverrides(%q) = %v, want error", raw, overrides)
		}
	}
}
