// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "testing"

// TestDeclareBindAreIdentity pins the build-time-only contract: Declare
// and Bind return their argument unchanged, so declaration values carry
// no runtime behavior and no hidden normalization.
func TestDeclareBindAreIdentity(t *testing.T) {
	family := Family{
		ID:              "aws",
		FactDir:         "go/internal/facts/cloud",
		OwnerRepository: "eshu-hq/eshu",
		Kinds: []Kind{
			{Kind: "aws_resource"},
			{
				Kind: "aws_warning", Disposition: DispositionUnconsumed,
				NoReducerReason: "no consumer", NoReducerEvidence: "ledger",
			},
		},
		Producers: []Producer{{
			ID: "collector-aws-cloud", Repository: "eshu-hq/eshu",
			AuthorityMode: AuthorityCoreIssuedGrant,
			Kinds:         []string{"aws_resource", "aws_warning"},
		}},
	}
	if got := Declare(family); got.ID != family.ID || len(got.Kinds) != 2 ||
		got.Producers[0].AuthorityMode != AuthorityCoreIssuedGrant {
		t.Fatalf("Declare mutated its input: %+v", got)
	}
	binding := Binding{
		ID: "reducer/reducer_aws", FamilyID: "aws",
		ReducerDir: "go/internal/reducer", Symbol: "Handler",
		Kinds: []string{"aws_resource"},
	}
	if got := Bind(binding); got.Symbol != "Handler" || got.Kinds[0] != "aws_resource" {
		t.Fatalf("Bind mutated its input: %+v", got)
	}
}

// TestDispositionAndAuthorityWireValues pins the wire spellings that the
// generated registry shards and catalog emit. Renaming a value here is a
// contract change for every shard consumer, so it must be deliberate.
func TestDispositionAndAuthorityWireValues(t *testing.T) {
	if DispositionReadOnly != "read_only" || DispositionProjectorOnly != "projector_only" ||
		DispositionCollectorOnly != "collector_only" || DispositionUnconsumed != "unconsumed" {
		t.Fatal("disposition wire values changed")
	}
	if AuthorityCoreOwned != "core_owned" || AuthorityCoreIssuedGrant != "core_issued_grant" {
		t.Fatal("authority mode wire values changed")
	}
}
