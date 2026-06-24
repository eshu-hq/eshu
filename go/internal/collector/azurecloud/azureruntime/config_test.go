// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
)

func TestConfigValidatedDefaults(t *testing.T) {
	cfg, err := Config{
		CollectorInstanceID: "  azure-collector-1  ",
		Targets:             []TargetConfig{testTarget()},
	}.validated()
	if err != nil {
		t.Fatalf("validated: %v", err)
	}
	if cfg.CollectorInstanceID != "azure-collector-1" {
		t.Fatalf("collector id = %q, want trimmed", cfg.CollectorInstanceID)
	}
	if cfg.PollInterval != DefaultPollInterval {
		t.Fatalf("poll interval = %v, want default %v", cfg.PollInterval, DefaultPollInterval)
	}
}

func TestConfigValidatedRejectsEmptyTargets(t *testing.T) {
	if _, err := (Config{CollectorInstanceID: "azure-collector-1"}).validated(); err == nil {
		t.Fatal("expected error for no targets")
	}
}

func TestConfigValidatedRejectsNegativePollInterval(t *testing.T) {
	_, err := Config{
		CollectorInstanceID: "azure-collector-1",
		PollInterval:        -time.Second,
		Targets:             []TargetConfig{testTarget()},
	}.validated()
	if err == nil {
		t.Fatal("expected error for negative poll interval")
	}
}

func TestConfigValidatedRejectsDuplicateTargets(t *testing.T) {
	_, err := Config{
		CollectorInstanceID: "azure-collector-1",
		Targets:             []TargetConfig{testTarget(), testTarget()},
	}.validated()
	if err == nil {
		t.Fatal("expected error for duplicate scope targets")
	}
}

func TestTargetValidatedAssignsDefaultFencingToken(t *testing.T) {
	target := testTarget()
	target.FencingToken = 0
	validated, err := target.validated()
	if err != nil {
		t.Fatalf("validated: %v", err)
	}
	if validated.FencingToken != defaultFencingToken {
		t.Fatalf("fencing token = %d, want default %d", validated.FencingToken, defaultFencingToken)
	}
}

func TestTargetValidatedDefaultsAndValidatesSourceLane(t *testing.T) {
	target := testTarget()
	target.SourceLane = ""
	validated, err := target.validated()
	if err != nil {
		t.Fatalf("validated default source lane: %v", err)
	}
	if validated.SourceLane != azurecloud.SourceLaneResourceGraph {
		t.Fatalf("source lane = %q, want default resource_graph", validated.SourceLane)
	}

	target = testTarget()
	target.SourceLane = azurecloud.SourceLaneResourceChanges
	validated, err = target.validated()
	if err != nil {
		t.Fatalf("validated resource changes lane: %v", err)
	}
	if validated.SourceLane != azurecloud.SourceLaneResourceChanges {
		t.Fatalf("source lane = %q, want resource_changes", validated.SourceLane)
	}

	target = testTarget()
	target.SourceLane = "live_unbounded"
	if _, err := target.validated(); err == nil {
		t.Fatal("expected error for unsupported source lane")
	}
}

func TestTargetValidatedRejectsBadScopeKind(t *testing.T) {
	target := testTarget()
	target.ScopeKind = "resource_group"
	if _, err := target.validated(); err == nil {
		t.Fatal("expected error for unsupported scope kind")
	}
}

func TestTargetValidatedRequiresTenantAndProviderScope(t *testing.T) {
	cases := []func(*TargetConfig){
		func(tc *TargetConfig) { tc.TenantID = "" },
		func(tc *TargetConfig) { tc.ProviderScopeID = "" },
	}
	for i, mutate := range cases {
		target := testTarget()
		mutate(&target)
		if _, err := target.validated(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestTargetValidatedNormalizesBuckets(t *testing.T) {
	target := testTarget()
	target.ResourceTypeFamily = "Microsoft.Compute"
	target.LocationBucket = "EastUS"
	validated, err := target.validated()
	if err != nil {
		t.Fatalf("validated: %v", err)
	}
	if validated.ResourceTypeFamily != "microsoft.compute" {
		t.Fatalf("resource family = %q, want lowercased", validated.ResourceTypeFamily)
	}
	if validated.LocationBucket != "eastus" {
		t.Fatalf("location bucket = %q, want lowercased", validated.LocationBucket)
	}
}

func TestValidScopeKindMatchesContract(t *testing.T) {
	for _, kind := range []string{
		azurecloud.ScopeKindSubscription,
		azurecloud.ScopeKindManagementGroup,
		azurecloud.ScopeKindTenant,
	} {
		if !validScopeKind(kind) {
			t.Fatalf("scope kind %q should be valid", kind)
		}
	}
}
