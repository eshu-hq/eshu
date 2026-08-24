// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package security

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type intentBuilder func(string, string, projectorintent.FactLookup) (projectorintent.ReducerIntent, bool)

func TestBuildSecurityIntentsPreserveContracts(t *testing.T) {
	t.Parallel()

	const scopeID = "aws:111122223333:us-east-1"
	const generationID = "generation-1"
	tests := []struct {
		name          string
		fact          facts.Envelope
		build         intentBuilder
		wantDomain    reducer.Domain
		wantEntityKey string
		wantReason    string
		wantSource    string
	}{
		{
			name: "alert prefers source ref",
			fact: facts.Envelope{
				FactID:        "alert-1",
				FactKind:      facts.SecurityAlertRepositoryAlertFactKind,
				CollectorKind: "security-alert-collector",
				SourceRef:     facts.Ref{SourceSystem: " security-alert-provider "},
			},
			build:         BuildSecurityAlertReconciliationReducerIntent,
			wantDomain:    reducer.DomainSecurityAlertReconciliation,
			wantEntityKey: "security_alert_reconciliation:" + scopeID,
			wantReason:    "provider security alert evidence observed",
			wantSource:    "security-alert-provider",
		},
		{
			name: "package alert falls back to collector",
			fact: facts.Envelope{
				FactID:        "package-1",
				FactKind:      facts.PackageRegistryPackageFactKind,
				CollectorKind: " package-registry ",
			},
			build:         BuildSecurityAlertReconciliationReducerIntent,
			wantDomain:    reducer.DomainSecurityAlertReconciliation,
			wantEntityKey: "security_alert_reconciliation:" + scopeID,
			wantReason:    "package registry identity observed",
			wantSource:    "package-registry",
		},
		{
			name:          "security group endpoint",
			fact:          facts.Envelope{FactID: "rule-1", FactKind: facts.AWSSecurityGroupRuleFactKind},
			build:         BuildSecurityGroupEndpointMaterializationReducerIntent,
			wantDomain:    reducer.DomainSecurityGroupCidrMaterialization,
			wantEntityKey: "aws_resource_materialization:" + scopeID,
			wantReason:    "aws security group rule facts observed (endpoint nodes)",
		},
		{
			name:          "security group rule",
			fact:          facts.Envelope{FactID: "rule-1", FactKind: facts.AWSSecurityGroupRuleFactKind},
			build:         BuildSecurityGroupRuleMaterializationReducerIntent,
			wantDomain:    reducer.DomainSecurityGroupRuleMaterialization,
			wantEntityKey: "aws_resource_materialization:" + scopeID,
			wantReason:    "aws security group rule facts observed (rule nodes)",
		},
		{
			name:          "security group reachability",
			fact:          facts.Envelope{FactID: "rule-1", FactKind: facts.AWSSecurityGroupRuleFactKind},
			build:         BuildSecurityGroupReachabilityMaterializationReducerIntent,
			wantDomain:    reducer.DomainSecurityGroupReachabilityMaterialization,
			wantEntityKey: "aws_resource_materialization:" + scopeID,
			wantReason:    "aws security group rule facts observed (reachability edges)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := test.build(scopeID, generationID, projectorintent.NewFactLookup([]facts.Envelope{test.fact}))
			if !ok {
				t.Fatal("build intent ok = false, want true")
			}
			if got.ScopeID != scopeID || got.GenerationID != generationID {
				t.Fatalf("scope generation = %q/%q, want %q/%q", got.ScopeID, got.GenerationID, scopeID, generationID)
			}
			if got.Domain != test.wantDomain || got.EntityKey != test.wantEntityKey {
				t.Fatalf("domain/entity = %q/%q, want %q/%q", got.Domain, got.EntityKey, test.wantDomain, test.wantEntityKey)
			}
			if got.Reason != test.wantReason || got.SourceSystem != test.wantSource {
				t.Fatalf("reason/source = %q/%q, want %q/%q", got.Reason, got.SourceSystem, test.wantReason, test.wantSource)
			}
			if got.FactID != test.fact.FactID {
				t.Fatalf("FactID = %q, want %q", got.FactID, test.fact.FactID)
			}
		})
	}
}

func TestBuildSecurityIntentsPreserveEarliestMatchAndDeterminism(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactID: "package-first", FactKind: facts.PackageRegistryPackageFactKind},
		{FactID: "alert-later", FactKind: facts.SecurityAlertRepositoryAlertFactKind},
		{FactID: "rule-first", FactKind: facts.AWSSecurityGroupRuleFactKind},
		{FactID: "rule-duplicate", FactKind: facts.AWSSecurityGroupRuleFactKind},
	})
	builders := []intentBuilder{
		BuildSecurityAlertReconciliationReducerIntent,
		BuildSecurityGroupEndpointMaterializationReducerIntent,
		BuildSecurityGroupRuleMaterializationReducerIntent,
		BuildSecurityGroupReachabilityMaterializationReducerIntent,
	}
	for index, build := range builders {
		first, ok := build("scope-1", "generation-1", lookup)
		if !ok {
			t.Fatalf("builder %d returned no intent", index)
		}
		second, ok := build("scope-1", "generation-1", lookup)
		if !ok || !reflect.DeepEqual(second, first) {
			t.Fatalf("builder %d second result = %#v, %v; want deterministic %#v, true", index, second, ok, first)
		}
		wantFactID := "rule-first"
		if index == 0 {
			wantFactID = "package-first"
		}
		if first.FactID != wantFactID {
			t.Fatalf("builder %d FactID = %q, want earliest %q", index, first.FactID, wantFactID)
		}
	}
}

func TestBuildSecurityIntentsSkipMissingKinds(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: facts.AWSResourceFactKind}})
	for index, build := range []intentBuilder{
		BuildSecurityAlertReconciliationReducerIntent,
		BuildSecurityGroupEndpointMaterializationReducerIntent,
		BuildSecurityGroupRuleMaterializationReducerIntent,
		BuildSecurityGroupReachabilityMaterializationReducerIntent,
	} {
		if got, ok := build("scope-1", "generation-1", lookup); ok {
			t.Fatalf("builder %d intent = %#v, want no intent", index, got)
		}
	}
}

func TestSecurityGroupBuildersKeepKindOnlyAdmission(t *testing.T) {
	t.Parallel()

	// Payload validation belongs to the reducer handler. The projector trigger
	// intentionally admits a malformed rule envelope by kind, preserving the
	// existing retry and dead-letter ownership boundary.
	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactID:   "malformed-rule",
		FactKind: facts.AWSSecurityGroupRuleFactKind,
		Payload:  map[string]any{"direction": 42},
	}})
	if got, ok := BuildSecurityGroupReachabilityMaterializationReducerIntent("scope-1", "generation-1", lookup); !ok || got.FactID != "malformed-rule" {
		t.Fatalf("intent = %#v, %v; want kind-only admission", got, ok)
	}
}
