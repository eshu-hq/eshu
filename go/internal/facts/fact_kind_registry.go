// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"fmt"
	"slices"
	"strings"
)

// FactKindTruthProfile classifies whether a fact kind is deterministic,
// provider-gated, fixture-gated, or optional semantic evidence.
type FactKindTruthProfile string

const (
	// FactKindTruthDeterministic marks fact kinds that can be emitted and
	// consumed without provider or LLM configuration.
	FactKindTruthDeterministic FactKindTruthProfile = "deterministic"
	// FactKindTruthProviderGated marks fact kinds whose evidence depends on an
	// external provider or configured source system.
	FactKindTruthProviderGated FactKindTruthProfile = "provider_gated"
	// FactKindTruthFixtureGated marks fact kinds with fixture-proven contracts
	// whose live provider path is not generally enabled.
	FactKindTruthFixtureGated FactKindTruthProfile = "fixture_gated"
	// FactKindTruthOptionalSemantic marks optional semantic or LLM-derived facts.
	FactKindTruthOptionalSemantic FactKindTruthProfile = "optional_semantic"
)

// FactKindRegistryEntry is one generated fact-kind contract row.
type FactKindRegistryEntry struct {
	Kind                   string
	SchemaVersion          string
	LifecycleOwner         string
	ReducerDomain          string
	ProjectionHook         string
	AdmissionHook          string
	ReadSurface            string
	TruthProfile           FactKindTruthProfile
	PolicyGate             string
	ProviderKeyIndependent bool
	// PayloadSchema is the repo-relative path to the checked-in JSON Schema
	// artifact under sdk/go/factschema/schema/ that describes this fact
	// kind's payload shape. Optional: a kind whose payload has not yet been
	// migrated to a typed sdk/go/factschema struct leaves this blank. See
	// registry v1.1 (specs/fact-kind-registry.v1.yaml) and
	// docs/public/reference/fact-schema-versioning.md.
	PayloadSchema string
	// DeprecatedIn is the registry-spec semver at which this fact kind (or
	// field, when a future per-field marker lands) was marked deprecated.
	// Optional; blank means not deprecated.
	DeprecatedIn string
	// RemovedIn is the registry-spec semver at which this fact kind is
	// planned for removal. Optional; blank means no removal is scheduled.
	RemovedIn string
}

// FactKindRegistry returns the generated fact-kind registry in stable order.
func FactKindRegistry() []FactKindRegistryEntry {
	return slices.Clone(factKindRegistryEntries)
}

// FactKindRegistryEntryFor returns the generated registry entry for kind.
func FactKindRegistryEntryFor(kind string) (FactKindRegistryEntry, bool) {
	entry, ok := factKindRegistryByKind[strings.TrimSpace(kind)]
	return entry, ok
}

// ValidateFactKindRegistry verifies that entries cover every live versioned
// first-party fact kind and carry the metadata required by reducer, admission,
// projection, and read-surface contract gates.
func ValidateFactKindRegistry(entries []FactKindRegistryEntry) error {
	expected := liveSchemaVersionRegistry()
	seen := make(map[string]FactKindRegistryEntry, len(entries))
	for _, entry := range entries {
		if err := validateFactKindRegistryEntry(entry, expected); err != nil {
			return err
		}
		if _, dup := seen[entry.Kind]; dup {
			return fmt.Errorf("duplicate registry entry for fact kind %q", entry.Kind)
		}
		seen[entry.Kind] = entry
	}
	for kind := range expected {
		if _, ok := seen[kind]; !ok {
			return fmt.Errorf("missing registry entry for fact kind %q", kind)
		}
	}
	return nil
}

func validateFactKindRegistryEntry(entry FactKindRegistryEntry, expected map[string]string) error {
	kind := strings.TrimSpace(entry.Kind)
	if kind == "" {
		return fmt.Errorf("fact kind registry entry has blank kind")
	}
	expectedVersion, ok := expected[kind]
	if !ok {
		return fmt.Errorf("registry entry references missing implementation for fact kind %q", kind)
	}
	if strings.TrimSpace(entry.SchemaVersion) != expectedVersion {
		return fmt.Errorf("fact kind %q schema_version = %q, want %q", kind, entry.SchemaVersion, expectedVersion)
	}
	for field, value := range map[string]string{
		"lifecycle_owner": entry.LifecycleOwner,
		"reducer_domain":  entry.ReducerDomain,
		"projection_hook": entry.ProjectionHook,
		"admission_hook":  entry.AdmissionHook,
		"read_surface":    entry.ReadSurface,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("fact kind %q missing %s", kind, field)
		}
	}
	for field, marker := range map[string]string{
		"deprecated_in": entry.DeprecatedIn,
		"removed_in":    entry.RemovedIn,
	} {
		if v := strings.TrimSpace(marker); v != "" && !IsCanonicalSchemaVersion(v) {
			return fmt.Errorf("fact kind %q %s %q is not a canonical semver (MAJOR.MINOR.PATCH)", kind, field, v)
		}
	}
	if strings.TrimSpace(entry.RemovedIn) != "" && strings.TrimSpace(entry.DeprecatedIn) == "" {
		return fmt.Errorf("fact kind %q has removed_in set without deprecated_in", kind)
	}
	switch entry.TruthProfile {
	case FactKindTruthDeterministic:
		if !entry.ProviderKeyIndependent {
			return fmt.Errorf("fact kind %q truth_profile deterministic requires provider_key_independent", kind)
		}
	case FactKindTruthProviderGated, FactKindTruthFixtureGated:
	case FactKindTruthOptionalSemantic:
		if strings.TrimSpace(entry.PolicyGate) == "" {
			return fmt.Errorf("fact kind %q optional_semantic entry missing policy_gate", kind)
		}
	default:
		return fmt.Errorf("fact kind %q has unsupported truth_profile %q", kind, entry.TruthProfile)
	}
	return nil
}

func buildFactKindRegistryByKind(entries []FactKindRegistryEntry) map[string]FactKindRegistryEntry {
	byKind := make(map[string]FactKindRegistryEntry, len(entries))
	for _, entry := range entries {
		byKind[entry.Kind] = entry
	}
	return byKind
}

func buildFactKindSchemaRegistry(entries []FactKindRegistryEntry) map[string]string {
	registry := make(map[string]string, len(entries))
	for _, entry := range entries {
		registry[entry.Kind] = entry.SchemaVersion
	}
	return registry
}
