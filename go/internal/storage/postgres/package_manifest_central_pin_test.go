// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// centralVersionDeclarationCase is one payload shape for the central pin
// flag. The Go predicate (IsCentralVersionDeclaration) and the SQL predicate
// (NotCentralVersionDeclarationSQL) must classify every case the same way;
// the unit test checks the Go side and the live test checks the SQL side
// against the same table (#8824).
type centralVersionDeclarationCase struct {
	name    string
	payload map[string]any
	pin     bool
}

func centralVersionDeclarationCases() []centralVersionDeclarationCase {
	metadata := func(value any) map[string]any {
		return map[string]any{"entity_metadata": map[string]any{
			"config_kind": "dependency", "central_version_declaration": value,
		}}
	}
	return []centralVersionDeclarationCase{
		{"metadata bool true (parser shape)", metadata(true), true},
		{"metadata string true", metadata("true"), true},
		{"metadata string TRUE padded", metadata("  TRUE "), true},
		{"metadata bool false", metadata(false), false},
		{"metadata string false", metadata("false"), false},
		{"metadata empty string", metadata(""), false},
		{"metadata number 1", metadata(1), false},
		{"metadata null", metadata(nil), false},
		{"flag absent", map[string]any{"entity_metadata": map[string]any{"config_kind": "dependency"}}, false},
		{"no metadata", map[string]any{"entity_type": "Variable"}, false},
		{"top-level true", map[string]any{"central_version_declaration": true}, true},
		{"top-level false overrides metadata true", map[string]any{
			"central_version_declaration": false,
			"entity_metadata":             map[string]any{"central_version_declaration": true},
		}, false},
		{"top-level empty falls back to metadata true", map[string]any{
			"central_version_declaration": "",
			"entity_metadata":             map[string]any{"central_version_declaration": true},
		}, true},
		{"top-level null falls back to metadata true", map[string]any{
			"central_version_declaration": nil,
			"entity_metadata":             map[string]any{"central_version_declaration": true},
		}, true},
	}
}

func TestIsCentralVersionDeclarationTable(t *testing.T) {
	t.Parallel()
	for _, tc := range centralVersionDeclarationCases() {
		if got := IsCentralVersionDeclaration(tc.payload); got != tc.pin {
			t.Errorf("%s: IsCentralVersionDeclaration() = %v, want %v", tc.name, got, tc.pin)
		}
	}
}

// TestPackageManifestConsumptionKeyRowsSkipCentralPins keeps central version
// pins out of package_manifest_consumption_keys on both writers (the ingest
// refresh and the reducer-owned backfill share packageManifestConsumptionKeyRows),
// so the package-anchored readiness arm never reads a pin as consumption.
func TestPackageManifestConsumptionKeyRowsSkipCentralPins(t *testing.T) {
	t.Parallel()
	dependency := func(factID string, pin bool) facts.Envelope {
		metadata := map[string]any{"config_kind": "dependency", "package_manager": "nuget"}
		if pin {
			metadata["central_version_declaration"] = true
		}
		return manifestConsumptionTestEnvelope(factID, false, map[string]any{
			"entity_type":     "Variable",
			"repo_id":         "repository:app",
			"entity_name":     "Newtonsoft.Json",
			"entity_metadata": metadata,
		})
	}
	rows := packageManifestConsumptionKeyRows([]facts.Envelope{
		dependency("central-pin", true),
		dependency("project-reference", false),
	})
	want := []packageManifestConsumptionKeyRow{{
		FactID:       "project-reference",
		ScopeID:      "git-repository-scope:repository:app",
		GenerationID: "generation-1",
		RepositoryID: "repository:app",
		Ecosystem:    "nuget",
		PackageName:  "newtonsoft.json",
	}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("packageManifestConsumptionKeyRows() = %#v, want %#v", rows, want)
	}
}
