// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteValidationInventoryIsDeterministicAndProductionScoped(t *testing.T) {
	t.Parallel()

	matrix := Matrix{Capabilities: []MatrixCapability{
		{
			Capability:      "cap.two",
			AnsweringStores: []string{"status_fixed"},
			Profiles: map[string]MatrixProfile{
				"production": {Status: "supported", Verification: []MatrixVerification{{Kind: "remote_validation", Ref: "prod-shared"}}},
			},
		},
		{
			Capability:      "cap.one",
			AnsweringStores: []string{"status_fixed"},
			Profiles: map[string]MatrixProfile{
				"production": {Status: "supported", Verification: []MatrixVerification{{Kind: "remote_validation", Ref: "prod-shared"}}},
			},
		},
		{
			Capability:      "cap.experimental",
			AnsweringStores: []string{"status_fixed"},
			Profiles: map[string]MatrixProfile{
				"production": {Status: "experimental", Verification: []MatrixVerification{{Kind: "remote_validation", Ref: "prod-experimental"}}},
			},
		},
	}}

	first, err := MarshalRemoteValidationInventory(BuildRemoteValidationInventory(matrix))
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}
	second, err := MarshalRemoteValidationInventory(BuildRemoteValidationInventory(matrix))
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("inventory rendering is not byte-for-byte deterministic")
	}
	inventory := BuildRemoteValidationInventory(matrix)
	if got, want := inventory.SchemaVersion, 2; got != want {
		t.Fatalf("schema version = %d, want %d", got, want)
	}
	if len(inventory.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want only the production-supported slug", inventory.Artifacts)
	}
	wantSubjects := []string{"cap.one/production", "cap.two/production"}
	for i, want := range wantSubjects {
		if got := inventory.Artifacts[0].Subjects[i]; got != want {
			t.Fatalf("subject[%d] = %q, want %q", i, got, want)
		}
	}
	if got, want := inventory.Artifacts[0].AssertionCount, len(wantSubjects); got != want {
		t.Fatalf("assertion_count = %d, want %d", got, want)
	}
}

func TestCheckRemoteValidationInventoryDetectsDrift(t *testing.T) {
	t.Parallel()

	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})
	path := filepath.Join(t.TempDir(), RemoteValidationInventoryFileName)
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write stale inventory: %v", err)
	}
	if err := CheckRemoteValidationInventory(matrix, path); err == nil {
		t.Fatal("expected stale generated inventory to fail")
	}
	if err := WriteRemoteValidationInventory(matrix, path); err != nil {
		t.Fatalf("regenerate inventory: %v", err)
	}
	if err := CheckRemoteValidationInventory(matrix, path); err != nil {
		t.Fatalf("fresh inventory failed: %v", err)
	}
}

func TestRemoteValidationInventoryRealSpecsCount(t *testing.T) {
	t.Parallel()

	specsDir := repoSpecsDir(t)
	matrix, err := LoadMatrix(specsDir)
	if err != nil {
		t.Fatalf("load real matrix: %v", err)
	}
	inventory := BuildRemoteValidationInventory(matrix)
	// 111 since #5970 restored symbol_graph.inheritance to production:
	// supported with prod-symbol-graph-inheritance. The other four rows that
	// issue re-ran stayed experimental, so they add no slug here.
	if got, want := len(inventory.Artifacts), 111; got != want {
		t.Fatalf("production-supported remote-validation slugs = %d, want %d", got, want)
	}
	repoRoot := filepath.Dir(specsDir)
	catalog, err := loadRemoteValidationProofCatalog(repoRoot)
	if err != nil {
		t.Fatalf("load remote-validation proofs: %v", err)
	}
	for _, item := range inventory.Artifacts {
		artifact, ok := catalog[item.Slug]
		if !ok {
			t.Fatalf("missing proof entry for %s (%s)", item.Slug, item.ArtifactPath)
		}
		if got := len(artifact.assertions); got != item.AssertionCount {
			t.Errorf("%s assertions count = %d, inventory assertion_count = %d", item.Slug, got, item.AssertionCount)
		}
		if got := len(artifact.b12Assertions); got != item.AssertionCount {
			t.Errorf("%s b12_assertions count = %d, inventory assertion_count = %d", item.Slug, got, item.AssertionCount)
		}
	}
}
