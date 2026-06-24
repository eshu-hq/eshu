// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const fixtureSkipToken = "skip-token-page-2"

func resourceRow(id, resourceType, location string) azurecloud.ResourceRow {
	return azurecloud.ResourceRow{
		ID:             id,
		Name:           "leaf",
		Type:           resourceType,
		TenantID:       "tenant-abc",
		SubscriptionID: "11111111-1111-1111-1111-111111111111",
		ResourceGroup:  "rg-app",
		Location:       location,
	}
}

func twoPageFixture() map[string]azurecloud.ResourceGraphPage {
	return map[string]azurecloud.ResourceGraphPage{
		"": {
			TotalRecords: 2,
			Count:        1,
			SkipToken:    fixtureSkipToken,
			Rows: []azurecloud.ResourceRow{
				resourceRow(
					"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-1",
					"microsoft.compute/virtualmachines",
					"eastus",
				),
			},
		},
		fixtureSkipToken: {
			TotalRecords: 2,
			Count:        1,
			SkipToken:    "",
			Rows: []azurecloud.ResourceRow{
				resourceRow(
					"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-data/providers/Microsoft.Storage/storageAccounts/acct1",
					"microsoft.storage/storageaccounts",
					"eastus",
				),
			},
		},
	}
}

func testTarget() TargetConfig {
	return TargetConfig{
		TenantID:           "tenant-abc",
		ScopeKind:          azurecloud.ScopeKindSubscription,
		ProviderScopeID:    "11111111-1111-1111-1111-111111111111",
		ResourceTypeFamily: "microsoft.compute",
		LocationBucket:     "eastus",
		CredentialRef:      "azure-read-only-spn",
		FencingToken:       7,
	}
}

func fixedClock() func() time.Time {
	at := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func newFixtureSource(t *testing.T, provider azurecloud.PageProvider, targets ...TargetConfig) *Source {
	t.Helper()
	return &Source{
		Config: Config{
			CollectorInstanceID: "azure-collector-1",
			PollInterval:        time.Minute,
			Targets:             targets,
		},
		ProviderFactory: StaticFixtureFactory(provider),
		Clock:           fixedClock(),
	}
}

func drain(t *testing.T, collected collector.CollectedGeneration) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	for env := range collected.Facts {
		out = append(out, env)
	}
	if collected.FactStreamErr != nil {
		if err := collected.FactStreamErr(); err != nil {
			t.Fatalf("fact stream error: %v", err)
		}
	}
	return out
}

func TestSourceYieldsGenerationFromFixturePages(t *testing.T) {
	provider := NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{})
	src := newFixtureSource(t, provider, testTarget())

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next error: %v", err)
	}
	if !ok {
		t.Fatal("expected a collected generation")
	}
	if collected.Scope.CollectorKind != scope.CollectorAzure {
		t.Fatalf("collector kind = %q, want azure", collected.Scope.CollectorKind)
	}
	if collected.Scope.SourceSystem != azurecloud.CollectorKind {
		t.Fatalf("source system = %q", collected.Scope.SourceSystem)
	}
	if err := collected.Generation.ValidateForScope(collected.Scope); err != nil {
		t.Fatalf("generation invalid for scope: %v", err)
	}
	envs := drain(t, collected)
	resources := factsOfKind(envs, facts.AzureCloudResourceFactKind)
	if len(resources) != 2 {
		t.Fatalf("emitted %d resource facts, want 2", len(resources))
	}
	for _, env := range envs {
		if env.GenerationID != collected.Generation.GenerationID {
			t.Fatalf("fact generation %q != generation %q", env.GenerationID, collected.Generation.GenerationID)
		}
		if env.FencingToken != 7 {
			t.Fatalf("fencing token = %d, want 7", env.FencingToken)
		}
	}

	// Batch is drained: the next call reports exhaustion and resets.
	if _, ok, err := src.Next(context.Background()); err != nil || ok {
		t.Fatalf("expected drained batch, got ok=%v err=%v", ok, err)
	}
}

func TestSourceSkipTokenResume(t *testing.T) {
	provider := NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{})
	src := newFixtureSource(t, provider, testTarget())

	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next ok=%v err=%v", ok, err)
	}
	drain(t, collected)

	calls := provider.Calls()
	if len(calls) != 2 {
		t.Fatalf("provider visited %d pages, want 2: %v", len(calls), calls)
	}
	if calls[0] != "" || calls[1] != fixtureSkipToken {
		t.Fatalf("skip token order = %v, want [\"\" %q]", calls, fixtureSkipToken)
	}
}

func TestSourceIdempotentReEmission(t *testing.T) {
	first := newFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())
	second := newFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())

	firstCollected, _, err := first.Next(context.Background())
	if err != nil {
		t.Fatalf("first Next: %v", err)
	}
	secondCollected, _, err := second.Next(context.Background())
	if err != nil {
		t.Fatalf("second Next: %v", err)
	}
	if firstCollected.Generation.GenerationID != secondCollected.Generation.GenerationID {
		t.Fatalf("generation ids differ: %q vs %q",
			firstCollected.Generation.GenerationID, secondCollected.Generation.GenerationID)
	}

	firstIDs := factIDSet(drain(t, firstCollected))
	for _, env := range drain(t, secondCollected) {
		if _, ok := firstIDs[env.FactID]; !ok {
			t.Fatalf("fact id %q not stable across replays", env.FactID)
		}
	}
}

func TestSourcePartialScopeAccounting(t *testing.T) {
	provider := NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{
		Partial:             true,
		HiddenResourceCount: 3,
		Reason:              azurecloud.WarningPermissionHidden,
		Message:             "subscription 22222222 not readable",
	})
	src := newFixtureSource(t, provider, testTarget())

	collected, _, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	warnings := factsOfKind(drain(t, collected), facts.AzureCollectionWarningFactKind)
	if len(warnings) != 1 {
		t.Fatalf("emitted %d warnings, want 1", len(warnings))
	}
	if warnings[0].Payload["warning_kind"] != azurecloud.WarningPermissionHidden {
		t.Fatalf("warning kind = %v", warnings[0].Payload["warning_kind"])
	}
	if warnings[0].Payload["outcome"] != azurecloud.OutcomePartial {
		t.Fatalf("outcome = %v", warnings[0].Payload["outcome"])
	}
}

func TestSourceLogOmitsProviderIdentifiers(t *testing.T) {
	provider := NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{})
	var logs bytes.Buffer
	src := newFixtureSource(t, provider, testTarget())
	src.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next ok=%v err=%v", ok, err)
	}
	drain(t, collected)

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v\n%s", err, logs.String())
	}
	for _, key := range []string{"scope_id", "generation_id"} {
		if _, ok := entry[key]; ok {
			t.Fatalf("log entry contains %q: %s", key, logs.String())
		}
	}
	for _, sensitive := range []string{testTarget().TenantID, testTarget().ProviderScopeID} {
		if strings.Contains(logs.String(), sensitive) {
			t.Fatalf("log entry contains provider identifier %q: %s", sensitive, logs.String())
		}
	}
}

func TestSourceMultipleTargetsYieldOneGenerationEach(t *testing.T) {
	second := testTarget()
	second.ProviderScopeID = "22222222-2222-2222-2222-222222222222"
	provider := NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{})
	src := newFixtureSource(t, provider, testTarget(), second)

	seen := make(map[string]struct{})
	for i := 0; i < 2; i++ {
		collected, ok, err := src.Next(context.Background())
		if err != nil || !ok {
			t.Fatalf("target %d: ok=%v err=%v", i, ok, err)
		}
		seen[collected.Scope.ScopeID] = struct{}{}
		drain(t, collected)
	}
	if len(seen) != 2 {
		t.Fatalf("distinct scope ids = %d, want 2: %v", len(seen), seen)
	}
	if _, ok, _ := src.Next(context.Background()); ok {
		t.Fatal("expected batch drained after two targets")
	}
}

func TestSourceRejectsInvalidConfig(t *testing.T) {
	src := &Source{
		Config:          Config{CollectorInstanceID: "", Targets: []TargetConfig{testTarget()}},
		ProviderFactory: StaticFixtureFactory(NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{})),
	}
	if _, _, err := src.Next(context.Background()); err == nil {
		t.Fatal("expected error for blank collector instance id")
	}
}

func TestSourceRequiresProviderFactory(t *testing.T) {
	src := &Source{
		Config: Config{
			CollectorInstanceID: "azure-collector-1",
			Targets:             []TargetConfig{testTarget()},
		},
	}
	if _, _, err := src.Next(context.Background()); err == nil {
		t.Fatal("expected error when provider factory is nil")
	}
}

func TestSourcePropagatesProviderError(t *testing.T) {
	factory := PageProviderFactoryFunc(func(
		context.Context,
		azurecloud.Boundary,
		TargetConfig,
	) (azurecloud.PageProvider, error) {
		return nil, errors.New("resource graph throttled")
	})
	src := &Source{
		Config: Config{
			CollectorInstanceID: "azure-collector-1",
			Targets:             []TargetConfig{testTarget()},
		},
		ProviderFactory: factory,
		Clock:           fixedClock(),
	}
	if _, _, err := src.Next(context.Background()); err == nil {
		t.Fatal("expected provider error to propagate")
	}
}

func TestLiveProviderFactoryIsGated(t *testing.T) {
	_, err := LiveProviderFactory{}.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if !errors.Is(err, ErrLiveProviderGated) {
		t.Fatalf("live provider error = %v, want ErrLiveProviderGated", err)
	}
}

func factsOfKind(envs []facts.Envelope, kind string) []facts.Envelope {
	var out []facts.Envelope
	for _, env := range envs {
		if env.FactKind == kind {
			out = append(out, env)
		}
	}
	return out
}

func factIDSet(envs []facts.Envelope) map[string]struct{} {
	set := make(map[string]struct{}, len(envs))
	for _, env := range envs {
		set[env.FactID] = struct{}{}
	}
	return set
}
