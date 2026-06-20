package facts

import "testing"

func TestEveryCoreFactKindHasRegisteredSchemaVersion(t *testing.T) {
	t.Parallel()

	for _, kind := range CoreFactKinds() {
		version, ok := SchemaVersion(kind)
		if !ok {
			t.Fatalf("core fact kind %q has no registered schema version; add its family to schemaVersionFamilies", kind)
		}
		if !schemaSemverPattern.MatchString(version) {
			t.Fatalf("core fact kind %q registered version %q is not semantic version", kind, version)
		}
	}
}

func TestDocumentationFamilySchemaVersions(t *testing.T) {
	t.Parallel()

	if got, _ := SchemaVersion(DocumentationSectionFactKind); got != DocumentationSectionFactSchemaVersion {
		t.Fatalf("SchemaVersion(documentation_section) = %q, want %q", got, DocumentationSectionFactSchemaVersion)
	}
	if got, _ := SchemaVersion(DocumentationSourceFactKind); got != DocumentationFactSchemaVersion {
		t.Fatalf("SchemaVersion(documentation_source) = %q, want %q", got, DocumentationFactSchemaVersion)
	}
	// A core documentation kind on an unsupported major must be rejected, not
	// treated as unknown_kind.
	if got := ClassifySchemaVersion(DocumentationSectionFactKind, "9.0.0"); got != CompatibilityUnsupportedMajor {
		t.Fatalf("ClassifySchemaVersion(documentation_section, 9.0.0) = %q, want %q", got, CompatibilityUnsupportedMajor)
	}
	if err := ValidateSchemaVersion(DocumentationSectionFactKind, "9.0.0"); err == nil {
		t.Fatal("ValidateSchemaVersion(documentation_section, 9.0.0) error = nil, want unsupported")
	}
}

func TestSchemaVersionDispatchesToFamilies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		factKind string
	}{
		{"terraform state", TerraformStateFactKinds()[0]},
		{"oci registry", OCIRegistryFactKinds()[0]},
		{"aws", AWSFactKinds()[0]},
		{"observability", ObservabilityFactKinds()[0]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := SchemaVersion(tc.factKind)
			if !ok {
				t.Fatalf("SchemaVersion(%q) ok = false, want true", tc.factKind)
			}
			if got == "" {
				t.Fatalf("SchemaVersion(%q) = empty, want a version", tc.factKind)
			}
		})
	}
}

func TestSchemaVersionUnknownKind(t *testing.T) {
	t.Parallel()

	if _, ok := SchemaVersion("dev.example.not_a_core_kind"); ok {
		t.Fatal("SchemaVersion(unknown) ok = true, want false")
	}
}

func TestSchemaVersionRegistryCoversEveryVersionedFamily(t *testing.T) {
	t.Parallel()

	registry := SupportedSchemaVersions()
	if len(registry) == 0 {
		t.Fatal("SupportedSchemaVersions() is empty")
	}
	// Every kind in the registry must round-trip through SchemaVersion and have
	// a non-empty semantic version. This is the drift guard: a new family with a
	// schema version must register here.
	for kind, version := range registry {
		got, ok := SchemaVersion(kind)
		if !ok || got != version {
			t.Fatalf("SchemaVersion(%q) = (%q, %v), want (%q, true)", kind, got, ok, version)
		}
		if !schemaSemverPattern.MatchString(version) {
			t.Fatalf("registry version for %q = %q, want semantic version", kind, version)
		}
	}
}

func TestClassifySchemaVersion(t *testing.T) {
	t.Parallel()

	// Anchor on a real core fact kind and its registered version so the
	// old / current / unsupported-future regression uses production data.
	factKind := TerraformStateFactKinds()[0]
	current, ok := SchemaVersion(factKind)
	if !ok {
		t.Fatalf("SchemaVersion(%q) ok = false, want true", factKind)
	}
	if current != "1.0.0" {
		t.Fatalf("anchor version for %q = %q, want 1.0.0 (update the regression anchor)", factKind, current)
	}

	cases := []struct {
		name      string
		factKind  string
		candidate string
		want      Compatibility
	}{
		{"current is supported", factKind, "1.0.0", CompatibilitySupported},
		// The older-same-major backward-compatible path (e.g. core supports
		// 1.2.0, collector sends 1.1.0) cannot be exercised against real data
		// until a core family advances past 1.0.0; it is covered by inspection
		// (semver.Compare <= 0 falls through to CompatibilitySupported).
		{"older major is unsupported", factKind, "0.9.0", CompatibilityUnsupportedMajor},
		{"future major is unsupported", factKind, "2.0.0", CompatibilityUnsupportedMajor},
		{"future minor not yet supported", factKind, "1.1.0", CompatibilityUnsupportedMinor},
		{"future patch not yet supported", factKind, "1.0.1", CompatibilityUnsupportedMinor},
		{"unknown kind", "dev.example.unknown", "1.0.0", CompatibilityUnknownKind},
		{"blank version fails closed as major", factKind, "", CompatibilityUnsupportedMajor},
		{"non-semver version fails closed as major", factKind, "v1", CompatibilityUnsupportedMajor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifySchemaVersion(tc.factKind, tc.candidate); got != tc.want {
				t.Fatalf("ClassifySchemaVersion(%q, %q) = %q, want %q", tc.factKind, tc.candidate, got, tc.want)
			}
		})
	}
}

func TestValidateSchemaVersion(t *testing.T) {
	t.Parallel()

	factKind := TerraformStateFactKinds()[0]
	if err := ValidateSchemaVersion(factKind, "1.0.0"); err != nil {
		t.Fatalf("ValidateSchemaVersion(current) error = %v, want nil", err)
	}
	if err := ValidateSchemaVersion(factKind, "2.0.0"); err == nil {
		t.Fatal("ValidateSchemaVersion(future major) error = nil, want unsupported")
	}
	// Unknown kinds are not owned here; the caller decides. Validate returns nil
	// so non-core kinds are not falsely rejected by core validation.
	if err := ValidateSchemaVersion("dev.example.unknown", "1.0.0"); err != nil {
		t.Fatalf("ValidateSchemaVersion(unknown kind) error = %v, want nil", err)
	}
}
