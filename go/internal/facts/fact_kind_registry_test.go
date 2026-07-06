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

func testFactKindRegistryByKind(entries []FactKindRegistryEntry) map[string]FactKindRegistryEntry {
	byKind := make(map[string]FactKindRegistryEntry, len(entries))
	for _, entry := range entries {
		byKind[entry.Kind] = entry
	}
	return byKind
}
