// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strconv"
	"testing"
)

// TestBuildCloudStateFactsJoinKeysAgree pins the gate corpus's join
// contract: every admission identity's raw_identity must equal its provider
// fact's identity key, or /cloud/inventory's provider join finds nothing and
// the corpus measures an empty join instead of the real read. Every state
// resource address must likewise equal its binding's resource_address.
//
// The contract predates #6912 and outlived the read model it was written
// for: #6843 added these facts for its Postgres read model, #6912 removed
// that read model, and the corpus stays because /cloud/inventory reads the
// same rows on its own.
func TestBuildCloudStateFactsJoinKeysAgree(t *testing.T) {
	facts := BuildCloudStateFacts("scope-x", "gen-x", 9)
	if len(facts) == 0 {
		t.Fatal("no cloud/state facts built")
	}
	byID := map[string]SeedCloudStateFact{}
	for _, f := range facts {
		if f.ScopeID != "scope-x" || f.GenerationID != "gen-x" {
			t.Fatalf("fact %s anchors on %s/%s, want scope-x/gen-x", f.FactID, f.ScopeID, f.GenerationID)
		}
		byID[f.FactID] = f
	}
	providerKey := map[string]string{
		"aws_resource":         "arn",
		"gcp_cloud_resource":   "full_resource_name",
		"azure_cloud_resource": "arm_resource_id",
	}
	admissions, providers, states, bindings, ec2 := 0, 0, 0, 0, 0
	for _, f := range facts {
		switch f.Kind {
		case cloudIdentityFactKind:
			admissions++
		case "aws_resource", "gcp_cloud_resource", "azure_cloud_resource":
			providers++
			key := providerKey[f.Kind]
			if f.Payload[key] == nil || f.Payload[key] == "" {
				t.Fatalf("provider fact %s lacks identity key %s", f.FactID, key)
			}
		case ec2PostureFactKind:
			ec2++
			if f.Payload["instance_id"] == nil || f.Payload["instance_id"] == "" {
				t.Fatalf("EC2 fact %s lacks instance_id", f.FactID)
			}
		case stateResourceFactKind:
			states++
		case stateBindingFactKind:
			bindings++
			if f.Payload["resource_address"] == nil || f.Payload["resource_address"] == "" {
				t.Fatalf("binding fact %s lacks resource_address", f.FactID)
			}
			if f.Payload["provider_type"] == nil || f.Payload["provider_type"] == "" {
				t.Fatalf("binding fact %s lacks provider_type", f.FactID)
			}
		default:
			t.Fatalf("unexpected fact kind %s", f.Kind)
		}
	}
	if admissions != 9 || providers != 9 || states != 9 || bindings != 9 {
		t.Fatalf("counts = adm %d prov %d tsr %d bind %d, want 9 each", admissions, providers, states, bindings)
	}
	if ec2 != 3 {
		t.Fatalf("ec2 facts = %d, want 3 (one per aws i of 9)", ec2)
	}
	// The i-th admission's raw_identity must equal the i-th provider fact's
	// identity value; the i-th state address must equal the i-th binding's
	// resource_address. Fact IDs encode the index, so compare pairwise.
	for i := 0; i < 9; i++ {
		adm := byID[keyFor("gen-x-graphonly-adm-", i)]
		provKind := []string{"aws_resource", "gcp_cloud_resource", "azure_cloud_resource"}[i%3]
		prov := byID[keyFor("gen-x-graphonly-prov-", i)]
		if prov.Kind != provKind {
			t.Fatalf("provider fact %d kind = %s, want %s", i, prov.Kind, provKind)
		}
		if adm.Payload["raw_identity"] != prov.Payload[providerKey[provKind]] {
			t.Fatalf("admission %d raw_identity %v != provider identity %v",
				i, adm.Payload["raw_identity"], prov.Payload[providerKey[provKind]])
		}
		state := byID[keyFor("gen-x-graphonly-tsr-", i)]
		bind := byID[keyFor("gen-x-graphonly-bind-", i)]
		if state.Payload["address"] != bind.Payload["resource_address"] {
			t.Fatalf("state %d address %v != binding resource_address %v",
				i, state.Payload["address"], bind.Payload["resource_address"])
		}
	}
}

func keyFor(prefix string, i int) string {
	return prefix + strconv.Itoa(i)
}
