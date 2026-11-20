// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
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

	for _, raw := range []string{"not-a-duration", "-5m", "10mm"} {
		getenv := func(string) string { return raw }
		if got, err := loadReducerHandlerTimeout(getenv); err == nil {
			t.Fatalf("loadReducerHandlerTimeout(%q) = %v, want error", raw, got)
		}
	}
}

// TestLoadReducerHandlerTimeoutZeroDisables pins the explicit opt-out: "0s"
// parses to no bound. It is distinct from empty (never configured) so an
// operator can loudly disable the bound instead of leaving it unset.
func TestLoadReducerHandlerTimeoutZeroDisables(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"0s", "0m", "0h"} {
		getenv := func(string) string { return raw }
		if got, err := loadReducerHandlerTimeout(getenv); err != nil || got != 0 {
			t.Fatalf("loadReducerHandlerTimeout(%q) = %v, %v; want 0, nil (explicitly disabled)", raw, got, err)
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

// TestLoadReducerHandlerTimeoutOverridesAcceptsZeroExemption pins #10945
// review: an explicit domain=0s exempts that domain from a default bound
// (HandlerTimeoutFor returns a present 0 as-is), so the loader must store
// 0 rather than fail startup.
func TestLoadReducerHandlerTimeoutOverridesAcceptsZeroExemption(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		if name == reducerHandlerTimeoutOverridesEnv {
			return "workload_materialization=0s"
		}
		return ""
	}
	overrides, err := loadReducerHandlerTimeoutOverrides(getenv)
	if err != nil {
		t.Fatalf("loadReducerHandlerTimeoutOverrides() error = %v, want nil", err)
	}
	bound, ok := overrides[reducer.DomainWorkloadMaterialization]
	if !ok {
		t.Fatalf("overrides = %v, want workload_materialization present", overrides)
	}
	if bound != 0 {
		t.Fatalf("override workload_materialization = %v, want 0 (exempt)", bound)
	}
}

// TestHandlerTimeoutsEnforced pins the startup-log branch: only a positive
// default or a positive override counts as enforced. An all-zero
// configuration (including pure-0s exemption maps) must log disabled.
func TestHandlerTimeoutsEnforced(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		def       time.Duration
		overrides map[reducercontract.Domain]time.Duration
		want      bool
	}{
		{"unset", 0, nil, false},
		{"all-zero overrides", 0, map[reducercontract.Domain]time.Duration{reducer.DomainWorkloadMaterialization: 0}, false},
		{"positive default", 5 * time.Minute, nil, true},
		{"positive override", 0, map[reducercontract.Domain]time.Duration{reducer.DomainWorkloadMaterialization: 5 * time.Minute}, true},
		{"mixed zero and positive", 0, map[reducercontract.Domain]time.Duration{reducer.DomainWorkloadMaterialization: 0, reducer.DomainServiceCatalogCorrelation: time.Minute}, true},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := handlerTimeoutsEnforced(tt.def, tt.overrides); got != tt.want {
				t.Fatalf("handlerTimeoutsEnforced(%v, %v) = %v, want %v", tt.def, tt.overrides, got, tt.want)
			}
		})
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
		"workload_materialization=-5m",
		"workload_materialization=5m,",
		"workload_materialization=5m,workload_materialization=10m",
	} {
		getenv := func(string) string { return raw }
		if overrides, err := loadReducerHandlerTimeoutOverrides(getenv); err == nil {
			t.Fatalf("loadReducerHandlerTimeoutOverrides(%q) = %v, want error", raw, overrides)
		}
	}
}
