// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestLoadRepoDependencyProjectionConfigBoundsWorkers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		raw     string
		backend runtimecfg.GraphBackend
		want    int
	}{
		{name: "empty NornicDB defaults to proven four", backend: runtimecfg.GraphBackendNornicDB, want: 4},
		{name: "empty Neo4j defaults to unscaled one", backend: runtimecfg.GraphBackendNeo4j, want: 1},
		{name: "one", raw: "1", backend: runtimecfg.GraphBackendNornicDB, want: 1},
		{name: "two", raw: "2", backend: runtimecfg.GraphBackendNornicDB, want: 2},
		{name: "four", raw: "4", backend: runtimecfg.GraphBackendNornicDB, want: 4},
		{name: "explicit four on Neo4j remains operator controlled", raw: "4", backend: runtimecfg.GraphBackendNeo4j, want: 4},
		{name: "invalid NornicDB falls back to proven four", raw: "3", backend: runtimecfg.GraphBackendNornicDB, want: 4},
		{name: "invalid Neo4j falls back to unscaled one", raw: "3", backend: runtimecfg.GraphBackendNeo4j, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := loadRepoDependencyProjectionConfig(func(name string) string {
				if name == repoDependencyProjectionWorkersEnv {
					return tc.raw
				}
				return ""
			}, tc.backend)
			if got := cfg.Workers; got != tc.want {
				t.Fatalf("repo dependency workers = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLoadRepoDependencyProjectionConfigSafetyBudgets(t *testing.T) {
	t.Parallel()

	defaults := loadRepoDependencyProjectionConfig(
		func(string) string { return "" },
		runtimecfg.GraphBackendNornicDB,
	)
	if got, want := defaults.LeaseTTL, 5*time.Minute; got != want {
		t.Fatalf("default repo dependency lease TTL = %v, want %v", got, want)
	}
	if got, want := defaults.CycleTimeout, 45*time.Second; got != want {
		t.Fatalf("default repo dependency cycle timeout = %v, want %v", got, want)
	}
	if got, want := defaults.GraphQuiescenceBudget, defaultNornicDBCanonicalWriteTimeout; got != want {
		t.Fatalf("default graph quiescence budget = %v, want %v", got, want)
	}

	overrides := loadRepoDependencyProjectionConfig(func(name string) string {
		switch name {
		case repoDependencyProjectionLeaseTTLEnv:
			return "6m"
		case repoDependencyProjectionCycleTimeoutEnv:
			return "1m"
		case canonicalWriteTimeoutEnv:
			return "2m"
		default:
			return ""
		}
	}, runtimecfg.GraphBackendNornicDB)
	if got, want := overrides.LeaseTTL, 6*time.Minute; got != want {
		t.Fatalf("overridden repo dependency lease TTL = %v, want %v", got, want)
	}
	if got, want := overrides.CycleTimeout, time.Minute; got != want {
		t.Fatalf("overridden repo dependency cycle timeout = %v, want %v", got, want)
	}
	if got, want := overrides.GraphQuiescenceBudget, 2*time.Minute; got != want {
		t.Fatalf("overridden graph quiescence budget = %v, want %v", got, want)
	}
}

func TestLoadRepoDependencyProjectionConfigUsesPerProcessLeaseOwner(t *testing.T) {
	t.Parallel()

	cfg := loadRepoDependencyProjectionConfig(func(name string) string {
		if name == repoDependencyProjectionLeaseOwnerEnv {
			return "operator-prefix"
		}
		return ""
	}, runtimecfg.GraphBackendNornicDB)
	if cfg.LeaseOwner == "operator-prefix" || !strings.HasPrefix(cfg.LeaseOwner, "operator-prefix:") {
		t.Fatalf("repo dependency lease owner = %q, want operator prefix plus per-process identity", cfg.LeaseOwner)
	}
	parts := strings.Split(cfg.LeaseOwner, ":")
	if len(parts) < 4 || strings.TrimSpace(parts[len(parts)-3]) == "" ||
		strings.TrimSpace(parts[len(parts)-2]) == "" || strings.TrimSpace(parts[len(parts)-1]) == "" {
		t.Fatalf("repo dependency lease owner = %q, want non-empty hostname, pid, and boot nonce suffix", cfg.LeaseOwner)
	}
	if parts[len(parts)-2] != strconv.Itoa(os.Getpid()) {
		t.Fatalf("repo dependency lease owner = %q, want current process pid before boot nonce", cfg.LeaseOwner)
	}
	if len(parts[len(parts)-1]) < 16 {
		t.Fatalf("repo dependency lease owner = %q, want boot-unique nonce", cfg.LeaseOwner)
	}
}

func TestLoadRepoDependencyProjectionConfigLeaseOwnerStableWithinBoot(t *testing.T) {
	t.Parallel()

	first := loadRepoDependencyProjectionConfig(
		func(string) string { return "" }, runtimecfg.GraphBackendNornicDB,
	).LeaseOwner
	second := loadRepoDependencyProjectionConfig(
		func(string) string { return "" }, runtimecfg.GraphBackendNornicDB,
	).LeaseOwner
	if first != second {
		t.Fatalf("repo dependency lease owner changed within one process boot: %q != %q", first, second)
	}
}

func TestLoadRepoDependencyProjectionConfigDefaultLeaseOwnerIsPerProcess(t *testing.T) {
	t.Parallel()

	cfg := loadRepoDependencyProjectionConfig(
		func(string) string { return "" }, runtimecfg.GraphBackendNornicDB,
	)
	if cfg.LeaseOwner == defaultRepoDependencyProjectionLeaseOwner ||
		!strings.HasPrefix(cfg.LeaseOwner, defaultRepoDependencyProjectionLeaseOwner+":") {
		t.Fatalf(
			"repo dependency lease owner = %q, want %q prefix plus per-process identity",
			cfg.LeaseOwner,
			defaultRepoDependencyProjectionLeaseOwner,
		)
	}
}
