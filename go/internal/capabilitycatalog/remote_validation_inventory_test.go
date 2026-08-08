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
			Capability: "cap.two",
			Profiles: map[string]MatrixProfile{
				"production": {Status: "supported", Verification: []MatrixVerification{{Kind: "remote_validation", Ref: "prod-shared"}}},
			},
		},
		{
			Capability: "cap.one",
			Profiles: map[string]MatrixProfile{
				"production": {Status: "supported", Verification: []MatrixVerification{{Kind: "remote_validation", Ref: "prod-shared"}}},
			},
		},
		{
			Capability: "cap.experimental",
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
	if len(inventory.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want only the production-supported slug", inventory.Artifacts)
	}
	wantSubjects := []string{"cap.one/production", "cap.two/production"}
	for i, want := range wantSubjects {
		if got := inventory.Artifacts[0].Subjects[i]; got != want {
			t.Fatalf("subject[%d] = %q, want %q", i, got, want)
		}
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

	matrix, err := LoadMatrix(repoSpecsDir(t))
	if err != nil {
		t.Fatalf("load real matrix: %v", err)
	}
	inventory := BuildRemoteValidationInventory(matrix)
	if got, want := len(inventory.Artifacts), 110; got != want {
		t.Fatalf("production-supported remote-validation slugs = %d, want %d", got, want)
	}
}
