// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestLoadReducerQueueConfigUsesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := loadReducerQueueConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadReducerQueueConfig() error = %v, want nil", err)
	}

	if got, want := cfg.RetryDelay, 30*time.Second; got != want {
		t.Fatalf("retryDelay = %v, want %v", got, want)
	}
	if got, want := cfg.MaxAttempts, 3; got != want {
		t.Fatalf("maxAttempts = %d, want %d", got, want)
	}
}

func TestLoadReducerQueueConfigReadsEnvOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := loadReducerQueueConfig(func(name string) string {
		switch name {
		case reducerRetryDelayEnv:
			return "2m"
		case reducerMaxAttemptsEnv:
			return "5"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadReducerQueueConfig() error = %v, want nil", err)
	}

	if got, want := cfg.RetryDelay, 2*time.Minute; got != want {
		t.Fatalf("retryDelay = %v, want %v", got, want)
	}
	if got, want := cfg.MaxAttempts, 5; got != want {
		t.Fatalf("maxAttempts = %d, want %d", got, want)
	}
}

func TestLoadReducerWorkerCount_EnvOverride(t *testing.T) {
	t.Parallel()
	got := loadReducerWorkerCount(func(k string) string {
		if k == reducerWorkersEnv {
			return "6"
		}
		return ""
	}, runtimecfg.GraphBackendNornicDB)
	if got != 6 {
		t.Fatalf("got %d, want 6", got)
	}
}

func TestLoadReducerWorkerCount_Neo4jDefaultCap(t *testing.T) {
	t.Parallel()
	got := loadReducerWorkerCount(func(string) string { return "" }, runtimecfg.GraphBackendNeo4j)
	if got < 1 || got > 4 {
		t.Fatalf("got %d, want 1-4", got)
	}
}

func TestLoadReducerWorkerCount_NornicDBDefaultsToCPU(t *testing.T) {
	t.Parallel()
	got := loadReducerWorkerCount(func(string) string { return "" }, runtimecfg.GraphBackendNornicDB)
	if want := expectedNornicDBReducerWorkers(); got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestLoadReducerWorkerCount_InvalidEnv(t *testing.T) {
	t.Parallel()
	got := loadReducerWorkerCount(func(k string) string {
		if k == reducerWorkersEnv {
			return "not-a-number"
		}
		return ""
	}, runtimecfg.GraphBackendNornicDB)
	if want := expectedNornicDBReducerWorkers(); got != want {
		t.Fatalf("got %d, want %d for NornicDB fallback", got, want)
	}
}

func TestRepoDependencyWorkersDoNotInheritMainReducerWorkers(t *testing.T) {
	t.Parallel()

	cfg := loadRepoDependencyProjectionConfig(func(name string) string {
		if name == reducerWorkersEnv {
			return "4"
		}
		return ""
	}, runtimecfg.GraphBackendNornicDB)
	if got := cfg.Workers; got != 4 {
		t.Fatalf("repo dependency workers = %d, want proven default 4 when only %s is set", got, reducerWorkersEnv)
	}
}

func TestLoadReducerBatchClaimSize_EnvOverride(t *testing.T) {
	t.Parallel()

	got := loadReducerBatchClaimSize(func(k string) string {
		if k == reducerBatchClaimEnv {
			return "6"
		}
		return ""
	}, 2, runtimecfg.GraphBackendNornicDB)
	if got != 6 {
		t.Fatalf("got %d, want 6", got)
	}
}

func TestLoadReducerBatchClaimSize_Neo4jDefault(t *testing.T) {
	t.Parallel()

	got := loadReducerBatchClaimSize(func(string) string { return "" }, 3, runtimecfg.GraphBackendNeo4j)
	if got != 12 {
		t.Fatalf("got %d, want 12", got)
	}
}

func TestLoadReducerBatchClaimSize_NornicDBDefaultsToWorkerCount(t *testing.T) {
	t.Parallel()

	got := loadReducerBatchClaimSize(func(string) string { return "" }, 8, runtimecfg.GraphBackendNornicDB)
	if got != 8 {
		t.Fatalf("got %d, want 8", got)
	}
}

func TestLoadReducerBatchClaimSize_InvalidEnvFallsBackToBackendDefault(t *testing.T) {
	t.Parallel()

	got := loadReducerBatchClaimSize(func(k string) string {
		if k == reducerBatchClaimEnv {
			return "nope"
		}
		return ""
	}, 2, runtimecfg.GraphBackendNornicDB)
	if got != 2 {
		t.Fatalf("got %d, want 2 for NornicDB fallback", got)
	}
}

func TestLoadReducerClaimDomain_DefaultsToAllDomains(t *testing.T) {
	t.Parallel()

	got, err := loadReducerClaimDomain(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadReducerClaimDomain() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("loadReducerClaimDomain() = %q, want empty domain filter", got)
	}
}

func TestLoadReducerClaimDomain_ParsesKnownDomain(t *testing.T) {
	t.Parallel()

	got, err := loadReducerClaimDomain(func(k string) string {
		if k == reducerClaimDomainEnv {
			return string(reducer.DomainSQLRelationshipMaterialization)
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadReducerClaimDomain() error = %v, want nil", err)
	}
	if got != reducer.DomainSQLRelationshipMaterialization {
		t.Fatalf("loadReducerClaimDomain() = %q, want %q", got, reducer.DomainSQLRelationshipMaterialization)
	}
}

func TestLoadReducerClaimDomain_RejectsUnknownDomain(t *testing.T) {
	t.Parallel()

	_, err := loadReducerClaimDomain(func(k string) string {
		if k == reducerClaimDomainEnv {
			return "not_a_domain"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadReducerClaimDomain() error = nil, want validation error")
	}
}

func TestLoadReducerClaimDomains_ParsesPluralKnownDomains(t *testing.T) {
	t.Parallel()

	got, err := loadReducerClaimDomains(func(k string) string {
		if k == reducerClaimDomainsEnv {
			return strings.Join([]string{
				string(reducer.DomainSQLRelationshipMaterialization),
				string(reducer.DomainInheritanceMaterialization),
			}, ",")
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadReducerClaimDomains() error = %v, want nil", err)
	}
	want := []reducer.Domain{
		reducer.DomainSQLRelationshipMaterialization,
		reducer.DomainInheritanceMaterialization,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("loadReducerClaimDomains() = %#v, want %#v", got, want)
	}
}

func TestLoadReducerClaimDomains_KeepsLegacySingleDomain(t *testing.T) {
	t.Parallel()

	got, err := loadReducerClaimDomains(func(k string) string {
		if k == reducerClaimDomainEnv {
			return string(reducer.DomainSQLRelationshipMaterialization)
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadReducerClaimDomains() error = %v, want nil", err)
	}
	want := []reducer.Domain{reducer.DomainSQLRelationshipMaterialization}
	if !slices.Equal(got, want) {
		t.Fatalf("loadReducerClaimDomains() = %#v, want %#v", got, want)
	}
}

func TestLoadReducerClaimDomains_RejectsAmbiguousLegacyAndPlural(t *testing.T) {
	t.Parallel()

	_, err := loadReducerClaimDomains(func(k string) string {
		switch k {
		case reducerClaimDomainEnv:
			return string(reducer.DomainSQLRelationshipMaterialization)
		case reducerClaimDomainsEnv:
			return string(reducer.DomainInheritanceMaterialization)
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("loadReducerClaimDomains() error = nil, want ambiguity error")
	}
}

func TestLoadReducerClaimDomains_RejectsUnknownPluralDomain(t *testing.T) {
	t.Parallel()

	_, err := loadReducerClaimDomains(func(k string) string {
		if k == reducerClaimDomainsEnv {
			return "not_a_domain"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadReducerClaimDomains() error = nil, want validation error")
	}
}

func TestLoadReducerClaimDomain_RejectsPluralDomainsWithClearLegacyMessage(t *testing.T) {
	t.Parallel()

	_, err := loadReducerClaimDomain(func(k string) string {
		if k == reducerClaimDomainsEnv {
			return strings.Join([]string{
				string(reducer.DomainSQLRelationshipMaterialization),
				string(reducer.DomainInheritanceMaterialization),
			}, ",")
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadReducerClaimDomain() error = nil, want multiple-domain validation error")
	}
	for _, want := range []string{
		reducerClaimDomainEnv,
		"supports exactly one reducer domain",
		reducerClaimDomainsEnv,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("loadReducerClaimDomain() error = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestLoadReducerClaimDomains_ReportsLegacySourceForEmptyDomain(t *testing.T) {
	t.Parallel()

	_, err := loadReducerClaimDomains(func(k string) string {
		if k == reducerClaimDomainEnv {
			return string(reducer.DomainSQLRelationshipMaterialization) + ","
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadReducerClaimDomains() error = nil, want empty-domain validation error")
	}
	if !strings.Contains(err.Error(), reducerClaimDomainEnv) {
		t.Fatalf("loadReducerClaimDomains() error = %q, want legacy env name", err.Error())
	}
	if strings.Contains(err.Error(), reducerClaimDomainsEnv) {
		t.Fatalf("loadReducerClaimDomains() error = %q, did not want plural env name", err.Error())
	}
}

func expectedNornicDBReducerWorkers() int {
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		n = 1
	}
	return n
}

func TestLoadReducerProjectorDrainGate_NornicDBLocalAuthoritative(t *testing.T) {
	t.Parallel()

	got, err := loadReducerProjectorDrainGate(func(k string) string {
		switch k {
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		default:
			return ""
		}
	}, runtimecfg.GraphBackendNornicDB)
	if err != nil {
		t.Fatalf("loadReducerProjectorDrainGate() error = %v, want nil", err)
	}
	if !got {
		t.Fatal("loadReducerProjectorDrainGate() = false, want true")
	}
}

func TestLoadReducerProjectorDrainGate_DisabledForNeo4j(t *testing.T) {
	t.Parallel()

	got, err := loadReducerProjectorDrainGate(func(k string) string {
		switch k {
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		default:
			return ""
		}
	}, runtimecfg.GraphBackendNeo4j)
	if err != nil {
		t.Fatalf("loadReducerProjectorDrainGate() error = %v, want nil", err)
	}
	if got {
		t.Fatal("loadReducerProjectorDrainGate() = true, want false")
	}
}

func TestLoadReducerProjectorDrainGate_DisabledWithoutLocalAuthoritativeProfile(t *testing.T) {
	t.Parallel()

	got, err := loadReducerProjectorDrainGate(func(string) string { return "" }, runtimecfg.GraphBackendNornicDB)
	if err != nil {
		t.Fatalf("loadReducerProjectorDrainGate() error = %v, want nil", err)
	}
	if got {
		t.Fatal("loadReducerProjectorDrainGate() = true, want false")
	}
}

func TestLoadReducerProjectorDrainGate_InvalidProfile(t *testing.T) {
	t.Parallel()

	_, err := loadReducerProjectorDrainGate(func(k string) string {
		if k == queryProfileEnv {
			return "definitely-not-a-profile"
		}
		return ""
	}, runtimecfg.GraphBackendNornicDB)
	if err == nil {
		t.Fatal("loadReducerProjectorDrainGate() error = nil, want non-nil")
	}
}

func TestParsePriorConfigDepth(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{name: "empty returns zero (loader uses default)", raw: "", want: 0},
		{name: "whitespace-only returns zero", raw: "   ", want: 0},
		{name: "valid positive integer", raw: "5", want: 5},
		{name: "default value passes through explicitly", raw: "10", want: 10},
		{name: "whitespace trimmed around integer", raw: "  20  ", want: 20},
		{name: "non-numeric returns zero", raw: "abc", want: 0},
		{name: "negative returns zero", raw: "-3", want: 0},
		{name: "zero returns zero (explicit use-default sentinel)", raw: "0", want: 0},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parsePriorConfigDepth(tc.raw, nil)
			if got != tc.want {
				t.Fatalf("parsePriorConfigDepth(%q, nil) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParsePriorConfigDepthWarnsOnInvalidValue(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	got := parsePriorConfigDepth("abc", logger)
	if got != 0 {
		t.Fatalf("parsePriorConfigDepth(%q, logger) = %d, want 0", "abc", got)
	}
	output := buf.String()
	if !strings.Contains(output, `"raw":"abc"`) {
		t.Fatalf("log output missing raw value: %s", output)
	}
	if !strings.Contains(output, `"failure_class":"env_parse"`) {
		t.Fatalf("log output missing failure_class: %s", output)
	}
}

func TestParsePriorConfigDepthNilLoggerDoesNotPanic(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("parsePriorConfigDepth(invalid, nil) panicked: %v", r)
		}
	}()

	if got := parsePriorConfigDepth("abc", nil); got != 0 {
		t.Fatalf("got = %d, want 0", got)
	}
}

func TestParsePriorConfigDepthZeroDoesNotWarn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	got := parsePriorConfigDepth("0", logger)
	if got != 0 {
		t.Fatalf("parsePriorConfigDepth(\"0\", logger) = %d, want 0", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no WARN log for \"0\" (documented use-default sentinel); got: %s", buf.String())
	}
}
