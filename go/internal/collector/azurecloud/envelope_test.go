// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testBoundary() Boundary {
	return Boundary{
		CollectorInstanceID: "azure-collector-1",
		TenantID:            "tenant-abc",
		ScopeKind:           ScopeKindSubscription,
		ProviderScopeID:     "11111111-1111-1111-1111-111111111111",
		ResourceTypeFamily:  "microsoft.compute",
		LocationBucket:      "eastus",
		SourceLane:          SourceLaneResourceGraph,
		ScopeID:             "azure:tenant-abc:subscription:11111111:microsoft.compute:eastus:resource_graph",
		GenerationID:        "gen-1",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	}
}

func testResourceObservation(t *testing.T) ResourceObservation {
	t.Helper()
	const armID = "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-web-01"
	identity, err := ParseARMIdentity(armID)
	if err != nil {
		t.Fatalf("parse identity: %v", err)
	}
	providerTime := time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC)
	return ResourceObservation{
		Boundary:      testBoundary(),
		ARMResourceID: armID,
		Identity:      identity,
		Kind:          "Linux",
		SKUClass:      "Standard_D2s_v3",
		APIVersion:    "2023-09-01",
		ProviderTime:  &providerTime,
		Tags:          map[string]string{"env": "prod"},
		HasIdentity:   true,
		RawExtension: map[string]any{
			"provisioningState": "Succeeded",
			"adminPassword":     "hunter2",
		},
	}
}

func TestNewResourceEnvelopeBuildsContractFields(t *testing.T) {
	obs := testResourceObservation(t)
	env, err := NewResourceEnvelope(obs)
	if err != nil {
		t.Fatalf("NewResourceEnvelope error: %v", err)
	}

	if env.FactKind != facts.AzureCloudResourceFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.AzureCloudResourceFactKind)
	}
	if env.SchemaVersion != facts.AzureCloudResourceSchemaVersion {
		t.Fatalf("SchemaVersion = %q", env.SchemaVersion)
	}
	if env.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", env.CollectorKind, CollectorKind)
	}
	if env.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want reported", env.SourceConfidence)
	}
	if env.FencingToken != obs.Boundary.FencingToken {
		t.Fatalf("FencingToken = %d", env.FencingToken)
	}
	if env.ScopeID != obs.Boundary.ScopeID || env.GenerationID != obs.Boundary.GenerationID {
		t.Fatalf("scope/generation not propagated: %q %q", env.ScopeID, env.GenerationID)
	}
	if env.SourceRef.SourceSystem != CollectorKind {
		t.Fatalf("SourceRef.SourceSystem = %q", env.SourceRef.SourceSystem)
	}

	payload := env.Payload
	checks := map[string]any{
		"collector_kind":           CollectorKind,
		"collector_instance_id":    "azure-collector-1",
		"tenant_id":                "tenant-abc",
		"subscription_id":          "11111111-1111-1111-1111-111111111111",
		"resource_group":           "rg-app",
		"provider_namespace":       "microsoft.compute",
		"resource_type":            "microsoft.compute/virtualmachines",
		"resource_name":            "vm-web-01",
		"location":                 "eastus",
		"arm_resource_id":          obs.ARMResourceID,
		"scope_kind":               ScopeKindSubscription,
		"source_lane":              SourceLaneResourceGraph,
		"redaction_policy_version": RedactionPolicyVersion,
	}
	for key, want := range checks {
		if got := payload[key]; got != want {
			t.Fatalf("payload[%q] = %v, want %v", key, got, want)
		}
	}

	ext, ok := payload["extension"].(map[string]any)
	if !ok {
		t.Fatalf("extension payload missing or wrong type: %T", payload["extension"])
	}
	if ext["schema_version"] != AzureExtensionSchemaVersion {
		t.Fatalf("extension schema_version = %v", ext["schema_version"])
	}
	data, ok := ext["data"].(map[string]any)
	if !ok {
		t.Fatalf("extension data missing: %T", ext["data"])
	}
	if _, leaked := data["adminPassword"]; leaked {
		t.Fatal("adminPassword leaked into emitted extension data")
	}
	if data["provisioningState"] != "Succeeded" {
		t.Fatalf("safe extension field missing: %v", data["provisioningState"])
	}
	if ext["redacted"] != true {
		t.Fatalf("extension redacted flag = %v, want true", ext["redacted"])
	}
}

func TestNewResourceEnvelopeStableKeyDeterministic(t *testing.T) {
	obs := testResourceObservation(t)
	first, err := NewResourceEnvelope(obs)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := NewResourceEnvelope(obs)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("stable key not deterministic: %q vs %q", first.StableFactKey, second.StableFactKey)
	}
	if first.FactID != second.FactID {
		t.Fatalf("fact id not deterministic: %q vs %q", first.FactID, second.FactID)
	}
}

func TestNewResourceEnvelopeStableKeyIgnoresExtensionChurn(t *testing.T) {
	obs := testResourceObservation(t)
	base, err := NewResourceEnvelope(obs)
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	obs.RawExtension = map[string]any{"provisioningState": "Updating", "newField": "x"}
	churned, err := NewResourceEnvelope(obs)
	if err != nil {
		t.Fatalf("churned: %v", err)
	}
	if base.StableFactKey != churned.StableFactKey {
		t.Fatalf("stable key changed with extension churn: %q vs %q", base.StableFactKey, churned.StableFactKey)
	}
}

func TestNewResourceEnvelopeValidation(t *testing.T) {
	valid := testResourceObservation(t)

	noARM := valid
	noARM.ARMResourceID = ""
	if _, err := NewResourceEnvelope(noARM); err == nil {
		t.Fatal("expected error for missing arm_resource_id")
	}

	badBoundary := valid
	badBoundary.Boundary.FencingToken = 0
	if _, err := NewResourceEnvelope(badBoundary); err == nil {
		t.Fatal("expected error for non-positive fencing token")
	}

	noGen := valid
	noGen.Boundary.GenerationID = ""
	if _, err := NewResourceEnvelope(noGen); err == nil {
		t.Fatal("expected error for missing generation id")
	}
}

func TestNewWarningEnvelope(t *testing.T) {
	obs := WarningObservation{
		Boundary:            testBoundary(),
		WarningKind:         WarningPartialScope,
		Outcome:             OutcomePartial,
		Retryable:           true,
		HiddenResourceCount: 3,
		Message:             "subscription 11111111 hidden by rbac",
	}
	env, err := NewWarningEnvelope(obs)
	if err != nil {
		t.Fatalf("NewWarningEnvelope error: %v", err)
	}
	if env.FactKind != facts.AzureCollectionWarningFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	if env.Payload["warning_kind"] != WarningPartialScope {
		t.Fatalf("warning_kind = %v", env.Payload["warning_kind"])
	}
	if env.Payload["outcome"] != OutcomePartial {
		t.Fatalf("outcome = %v", env.Payload["outcome"])
	}
	if env.Payload["retryable"] != true {
		t.Fatalf("retryable = %v", env.Payload["retryable"])
	}
	if env.Payload["hidden_resource_count"] != 3 {
		t.Fatalf("hidden_resource_count = %v", env.Payload["hidden_resource_count"])
	}
}

func TestNewWarningEnvelopeRequiresKind(t *testing.T) {
	obs := WarningObservation{Boundary: testBoundary(), Outcome: OutcomePartial}
	if _, err := NewWarningEnvelope(obs); err == nil {
		t.Fatal("expected error for missing warning_kind")
	}
}

func TestNewWarningEnvelopeRejectsInvalidHiddenResourceCount(t *testing.T) {
	for name, hiddenCount := range map[string]int{
		"negative": -1,
		"overflow": 1 << 31,
	} {
		obs := WarningObservation{
			Boundary:            testBoundary(),
			WarningKind:         WarningPartialScope,
			HiddenResourceCount: hiddenCount,
		}
		if _, err := NewWarningEnvelope(obs); err == nil {
			t.Fatalf("%s hidden_resource_count: error = nil, want non-nil", name)
		}
	}
}
