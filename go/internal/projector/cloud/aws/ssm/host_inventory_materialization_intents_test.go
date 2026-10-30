// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssm

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func ssmIntentEnvelope(kind, factID string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      kind,
		CollectorKind: "collector-aws-cloud",
		SourceRef:     facts.Ref{SourceSystem: "ssm-inventory"},
	}
}

func TestBuildHostInventoryIntentFiresOnEitherKind(t *testing.T) {
	t.Parallel()

	const scopeID = "aws:000000000001:us-east-1:ssm"
	const generationID = "ssm-generation-1"

	for _, envelopes := range [][]facts.Envelope{
		{ssmIntentEnvelope(cloud.SSMHostApplicationFactKind, "app-first")},
		{ssmIntentEnvelope(cloud.SSMHostPlatformFactKind, "plat-first")},
		{
			ssmIntentEnvelope(cloud.SSMHostPlatformFactKind, "plat-first"),
			ssmIntentEnvelope(cloud.SSMHostApplicationFactKind, "app-second"),
		},
	} {
		lookup := projectorintent.NewFactLookup(envelopes)
		intent, ok := BuildHostInventoryMaterializationReducerIntent(scopeID, generationID, lookup)
		if !ok {
			t.Fatalf("no intent for %v, want one", envelopes)
		}
		if intent.Domain != reducer.DomainSSMHostInventoryMaterialization {
			t.Fatalf("Domain = %q, want ssm_host_inventory_materialization", intent.Domain)
		}
		if intent.EntityKey != "ssm_host_inventory_materialization:"+scopeID {
			t.Fatalf("EntityKey = %q, want the domain-distinct scope key", intent.EntityKey)
		}
		if intent.ScopeID != scopeID || intent.GenerationID != generationID {
			t.Fatalf("intent scope/generation = %q/%q, want %q/%q", intent.ScopeID, intent.GenerationID, scopeID, generationID)
		}
		if intent.Reason == "" || intent.FactID == "" {
			t.Fatalf("intent missing reason/fact anchor: %+v", intent)
		}
	}
}

func TestBuildHostInventoryIntentPrefersApplicationAnchor(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		ssmIntentEnvelope(cloud.SSMHostPlatformFactKind, "plat-first"),
		ssmIntentEnvelope(cloud.SSMHostApplicationFactKind, "app-second"),
	})
	intent, ok := BuildHostInventoryMaterializationReducerIntent("scope", "gen", lookup)
	if !ok {
		t.Fatal("no intent, want one")
	}
	if intent.FactID != "app-second" {
		t.Fatalf("FactID = %q, want the application anchor", intent.FactID)
	}
}

func TestBuildHostInventoryIntentSilentWithoutSSM(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: cloud.AWSResourceFactKind, FactID: "resource-only"},
	})
	if _, ok := BuildHostInventoryMaterializationReducerIntent("scope", "gen", lookup); ok {
		t.Fatal("intent fired with no SSM facts, want silence")
	}
	if _, ok := BuildHostInventoryMaterializationReducerIntent("scope", "gen", projectorintent.NewFactLookup(nil)); ok {
		t.Fatal("intent fired on an empty generation, want silence")
	}
}
