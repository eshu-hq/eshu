// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"strings"
	"testing"
)

func TestGeneratedFactKindRegistryCoversCoreContracts(t *testing.T) {
	t.Parallel()

	entries := FactKindRegistry()
	if err := ValidateFactKindRegistry(entries); err != nil {
		t.Fatalf("ValidateFactKindRegistry(FactKindRegistry()) error = %v, want nil", err)
	}

	byKind := testFactKindRegistryByKind(entries)
	for _, kind := range CoreFactKinds() {
		entry, ok := byKind[kind]
		if !ok {
			t.Fatalf("FactKindRegistry() missing core kind %q", kind)
		}
		if entry.AdmissionExempt {
			// Exempt kinds carry no schema version by design; assert only the
			// non-version contract metadata and that they classify unknown.
			if entry.SchemaVersion != "" {
				t.Fatalf("admission-exempt kind %q has schema version %q, want blank", kind, entry.SchemaVersion)
			}
			if _, ok := SchemaVersion(kind); ok {
				t.Fatalf("SchemaVersion(%q) ok = true for admission-exempt kind, want false", kind)
			}
		} else {
			schemaVersion, ok := SchemaVersion(kind)
			if !ok {
				t.Fatalf("SchemaVersion(%q) ok = false, want true", kind)
			}
			if entry.SchemaVersion != schemaVersion {
				t.Fatalf("registry schema for %q = %q, want %q", kind, entry.SchemaVersion, schemaVersion)
			}
		}
		if entry.LifecycleOwner == "" || entry.ReducerDomain == "" || entry.ProjectionHook == "" || entry.AdmissionHook == "" || entry.ReadSurface == "" {
			t.Fatalf("registry entry for %q has incomplete contract metadata: %+v", kind, entry)
		}
	}
}

func TestValidateFactKindRegistryCatchesDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func([]FactKindRegistryEntry) []FactKindRegistryEntry
		wantErr string
	}{
		{
			name: "missing fact kind",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				return entries[1:]
			},
			wantErr: "missing registry entry",
		},
		{
			name: "missing schema version",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				entries[0].SchemaVersion = ""
				return entries
			},
			wantErr: "schema_version",
		},
		{
			name: "missing projection hook",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				entries[0].ProjectionHook = ""
				return entries
			},
			wantErr: "projection_hook",
		},
		{
			name: "missing admission hook",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				entries[0].AdmissionHook = ""
				return entries
			},
			wantErr: "admission_hook",
		},
		{
			name: "missing read surface",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				entries[0].ReadSurface = ""
				return entries
			},
			wantErr: "read_surface",
		},
		{
			name: "semantic fact without policy gate",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				for i := range entries {
					if entries[i].Kind == SemanticCodeHintFactKind {
						entries[i].TruthProfile = FactKindTruthOptionalSemantic
						entries[i].PolicyGate = ""
						return entries
					}
				}
				t.Fatalf("test fixture missing %q", SemanticCodeHintFactKind)
				return entries
			},
			wantErr: "policy_gate",
		},
		{
			name: "deterministic fact provider key dependence",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				for i := range entries {
					if entries[i].TruthProfile == FactKindTruthDeterministic {
						entries[i].ProviderKeyIndependent = false
						return entries
					}
				}
				t.Fatal("test fixture missing deterministic registry entry")
				return entries
			},
			wantErr: "provider_key_independent",
		},
		{
			name: "removed_in without deprecated_in",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				entries[0].RemovedIn = "2.0.0"
				entries[0].DeprecatedIn = ""
				return entries
			},
			wantErr: "removed_in",
		},
		{
			name: "non-semver deprecated_in",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				entries[0].DeprecatedIn = "next"
				return entries
			},
			wantErr: "deprecated_in",
		},
		{
			name: "non-semver removed_in",
			mutate: func(entries []FactKindRegistryEntry) []FactKindRegistryEntry {
				entries[0].DeprecatedIn = "1.2.0"
				entries[0].RemovedIn = "soon"
				return entries
			},
			wantErr: "removed_in",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entries := tc.mutate(FactKindRegistry())
			err := ValidateFactKindRegistry(entries)
			if err == nil {
				t.Fatalf("ValidateFactKindRegistry() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateFactKindRegistry() error = %q, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateFactKindRegistryAcceptsPreV11EntriesWithoutOptionalFields(t *testing.T) {
	t.Parallel()

	entries := FactKindRegistry()
	for i := range entries {
		entries[i].PayloadSchema = ""
		entries[i].DeprecatedIn = ""
		entries[i].RemovedIn = ""
	}
	if err := ValidateFactKindRegistry(entries); err != nil {
		t.Fatalf("ValidateFactKindRegistry() with blank v1.1 fields error = %v, want nil", err)
	}
}

func TestValidateFactKindRegistryAcceptsPopulatedV11Fields(t *testing.T) {
	t.Parallel()

	entries := FactKindRegistry()
	found := false
	for i := range entries {
		if entries[i].Kind == AWSResourceFactKind {
			entries[i].PayloadSchema = "sdk/go/factschema/schema/aws_resource.v1.schema.json"
			entries[i].DeprecatedIn = "1.2.0"
			entries[i].RemovedIn = "2.0.0"
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("test fixture missing %q", AWSResourceFactKind)
	}
	if err := ValidateFactKindRegistry(entries); err != nil {
		t.Fatalf("ValidateFactKindRegistry() with populated v1.1 fields error = %v, want nil", err)
	}
}

func TestFactKindRegistryClassifiesSemanticFacts(t *testing.T) {
	t.Parallel()

	entry, ok := FactKindRegistryEntryFor(SemanticDocumentationObservationFactKind)
	if !ok {
		t.Fatalf("FactKindRegistryEntryFor(%q) ok = false, want true", SemanticDocumentationObservationFactKind)
	}
	if entry.TruthProfile != FactKindTruthOptionalSemantic {
		t.Fatalf("semantic truth profile = %q, want %q", entry.TruthProfile, FactKindTruthOptionalSemantic)
	}
	if entry.PolicyGate == "" {
		t.Fatalf("semantic policy gate is empty: %+v", entry)
	}
	if entry.ProviderKeyIndependent {
		t.Fatalf("semantic entry provider_key_independent = true, want false: %+v", entry)
	}
}

func TestReducerDerivedFindingGovernanceRegistry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind          string
		readSurface   string
		payloadSchema string
	}{
		{
			kind:          "reducer_supply_chain_impact_finding",
			readSurface:   "GET /api/v0/supply-chain/impact/findings",
			payloadSchema: "sdk/go/factschema/schema/reducer_supply_chain_impact_finding.v1.schema.json",
		},
		{
			kind:          "reducer_aws_cloud_runtime_drift_finding",
			readSurface:   "POST /api/v0/aws/runtime-drift/findings",
			payloadSchema: "sdk/go/factschema/schema/reducer_aws_cloud_runtime_drift_finding.v1.schema.json",
		},
		{
			kind:          "reducer_multi_cloud_runtime_drift_finding",
			readSurface:   "POST /api/v0/cloud/runtime-drift/findings",
			payloadSchema: "sdk/go/factschema/schema/reducer_multi_cloud_runtime_drift_finding.v1.schema.json",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()

			entry, ok := FactKindRegistryEntryFor(tc.kind)
			if !ok {
				t.Fatalf("FactKindRegistryEntryFor(%q) ok = false, want true", tc.kind)
			}
			if entry.AdmissionExempt {
				t.Fatalf("registry entry for %q AdmissionExempt = true, want false", tc.kind)
			}
			if got, want := entry.SchemaVersion, "1.0.0"; got != want {
				t.Fatalf("registry entry for %q SchemaVersion = %q, want %q", tc.kind, got, want)
			}
			if got := entry.PayloadSchema; got != tc.payloadSchema {
				t.Fatalf("registry entry for %q PayloadSchema = %q, want %q", tc.kind, got, tc.payloadSchema)
			}
			if got := entry.ReadSurface; got != tc.readSurface {
				t.Fatalf("registry entry for %q ReadSurface = %q, want %q", tc.kind, got, tc.readSurface)
			}
			if version, ok := SchemaVersion(tc.kind); !ok || version != "1.0.0" {
				t.Fatalf("SchemaVersion(%q) = (%q, %v), want (1.0.0, true)", tc.kind, version, ok)
			}
			if err := ValidateSchemaVersion(tc.kind, "2.0.0"); err == nil {
				t.Fatalf("ValidateSchemaVersion(%q, 2.0.0) error = nil, want unsupported major", tc.kind)
			}
		})
	}
}

func TestReducerCloudAssetResolutionRegistryIsAdmissionExempt(t *testing.T) {
	t.Parallel()

	const kind = "reducer_cloud_asset_resolution"
	entry, ok := FactKindRegistryEntryFor(kind)
	if !ok {
		t.Fatalf("FactKindRegistryEntryFor(%q) ok = false, want true", kind)
	}
	if !entry.AdmissionExempt {
		t.Fatalf("registry entry for %q AdmissionExempt = false, want true", kind)
	}
	if entry.SchemaVersion != "" {
		t.Fatalf("registry entry for %q SchemaVersion = %q, want blank", kind, entry.SchemaVersion)
	}
	if entry.PayloadSchema != "" {
		t.Fatalf("registry entry for %q PayloadSchema = %q, want blank", kind, entry.PayloadSchema)
	}
	if entry.ReadSurface != "none" {
		t.Fatalf("registry entry for %q ReadSurface = %q, want none", kind, entry.ReadSurface)
	}
	if version, ok := SchemaVersion(kind); ok {
		t.Fatalf("SchemaVersion(%q) = (%q, true), want ok=false", kind, version)
	}
	for _, candidate := range []string{"", "1.0.0", "2.0.0", "not-a-version"} {
		if got := ClassifySchemaVersion(kind, candidate); got != CompatibilityUnknownKind {
			t.Fatalf("ClassifySchemaVersion(%q, %q) = %q, want %q", kind, candidate, got, CompatibilityUnknownKind)
		}
		if err := ValidateSchemaVersion(kind, candidate); err != nil {
			t.Fatalf("ValidateSchemaVersion(%q, %q) = %v, want nil", kind, candidate, err)
		}
	}
}

func TestW1fTypedFamiliesCarryPayloadSchemas(t *testing.T) {
	t.Parallel()

	for _, kind := range append(append([]string{}, WorkItemFactKinds()...), append(IncidentContextFactKinds(), IncidentRoutingFactKinds()...)...) {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			entry, ok := FactKindRegistryEntryFor(kind)
			if !ok {
				t.Fatalf("FactKindRegistryEntryFor(%q) ok = false, want true", kind)
			}
			if entry.PayloadSchema == "" {
				t.Fatalf("FactKindRegistryEntryFor(%q).PayloadSchema is blank; W1f typed families must register schema refs", kind)
			}
		})
	}
}

func testFactKindRegistryByKind(entries []FactKindRegistryEntry) map[string]FactKindRegistryEntry {
	byKind := make(map[string]FactKindRegistryEntry, len(entries))
	for _, entry := range entries {
		byKind[entry.Kind] = entry
	}
	return byKind
}
