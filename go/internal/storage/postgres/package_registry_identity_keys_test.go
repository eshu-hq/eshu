// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

func TestPackageRegistryIdentityKeyRowsDeriveAllOwnershipKeys(t *testing.T) {
	t.Parallel()

	rows := packageRegistryIdentityKeyRows([]facts.Envelope{
		{
			FactID:       "registry-live",
			ScopeID:      "registry-scope",
			GenerationID: "generation-1",
			FactKind:     chain.PackageRegistryPackageFactKind,
			Payload: map[string]any{
				"package_id":      "pkg:npm/@scope/example@1.0.0",
				"ecosystem":       "npm",
				"raw_name":        "@scope/Example",
				"normalized_name": "example",
				"namespace":       "@scope",
			},
		},
		{FactID: "registry-tombstone", FactKind: chain.PackageRegistryPackageFactKind, IsTombstone: true},
		{FactID: "not-registry", FactKind: "content_entity"},
	})

	want := []packageRegistryIdentityKeyRow{
		{"registry-live", "registry-scope", "generation-1", "npm", "@scope/example", "pkg:npm/@scope/example@1.0.0"},
		{"registry-live", "registry-scope", "generation-1", "npm", "example", "pkg:npm/@scope/example@1.0.0"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("packageRegistryIdentityKeyRows() = %#v, want %#v", rows, want)
	}
}

func TestRefreshPackageRegistryIdentityKeysReplacesAcceptedFacts(t *testing.T) {
	t.Parallel()

	database := &manifestConsumptionExecRecorder{}
	err := refreshPackageRegistryIdentityKeys(context.Background(), database, []facts.Envelope{{
		FactID:       "registry-live",
		ScopeID:      "registry-scope",
		GenerationID: "generation-1",
		FactKind:     chain.PackageRegistryPackageFactKind,
		Payload: map[string]any{
			"package_id": "pkg:npm/example@1.0.0",
			"ecosystem":  "npm",
			"raw_name":   "example",
		},
	}})
	if err != nil {
		t.Fatalf("refreshPackageRegistryIdentityKeys() error = %v, want nil", err)
	}
	if got, want := len(database.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(database.execs[0].query, "DELETE FROM package_registry_identity_keys") {
		t.Fatalf("first query = %q, want sidecar delete", database.execs[0].query)
	}
	if !strings.Contains(database.execs[1].query, "INSERT INTO package_registry_identity_keys") {
		t.Fatalf("second query = %q, want sidecar insert", database.execs[1].query)
	}
}
