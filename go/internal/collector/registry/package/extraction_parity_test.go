// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestExtractionParityRegisteredEcosystems pins the parser-to-envelope-to-
// factschema contract that the extraction destination must preserve: every
// registered ecosystem parser emits observations that build durable envelopes
// with the chain-pinned fact kind, schema version, reported confidence,
// scope/generation propagation, stable keys, and correlation anchors.
func TestExtractionParityRegisteredEcosystems(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		name      string
		ecosystem Ecosystem
		registry  string
		document  string
	}{
		{
			name:      "npm",
			ecosystem: EcosystemNPM,
			registry:  "https://registry.npmjs.org",
			document:  `{"name": "left-pad", "versions": {"1.3.0": {}}}`,
		},
		{
			name:      "pypi",
			ecosystem: EcosystemPyPI,
			registry:  "https://pypi.org",
			document:  `{"info": {"name": "requests"}, "releases": {"2.0.0": []}}`,
		},
		{
			name:      "gomodule",
			ecosystem: EcosystemGoModule,
			registry:  "https://proxy.golang.org",
			document:  `{"module": "example.com/mod", "version": "v1.0.0"}`,
		},
		{
			name:      "maven",
			ecosystem: EcosystemMaven,
			registry:  "https://repo.maven.apache.org/maven2",
			document:  `<project><groupId>com.example</groupId><artifactId>app</artifactId><version>1.0.0</version></project>`,
		},
		{
			name:      "nuget",
			ecosystem: EcosystemNuGet,
			registry:  "https://api.nuget.org/v3/index.json",
			document:  `<package><metadata><id>Newtonsoft.Json</id><version>13.0.1</version></metadata></package>`,
		},
		{
			name:      "generic",
			ecosystem: EcosystemGeneric,
			registry:  "https://registry.example",
			document:  `{"name": "acme-widget", "version": "1.0.0"}`,
		},
	}

	registry := DefaultMetadataParserRegistry()
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			ctx := parityContext(fixture.ecosystem, fixture.registry)
			parsed, err := registry.Parse(ctx, []byte(fixture.document))
			if err != nil {
				t.Fatalf("Parse(%s) error = %v", fixture.name, err)
			}
			if len(parsed.Packages) == 0 {
				t.Fatalf("Parse(%s) emitted no package observations", fixture.name)
			}

			envelope, err := NewPackageEnvelope(parsed.Packages[0])
			if err != nil {
				t.Fatalf("NewPackageEnvelope(%s) error = %v", fixture.name, err)
			}
			assertParityEnvelope(t, envelope, ctx.ScopeID)

			anchors, ok := envelope.Payload["correlation_anchors"].([]string)
			if !ok || len(anchors) == 0 {
				t.Fatalf("correlation_anchors = %#v, want non-empty", envelope.Payload["correlation_anchors"])
			}
			packageID, _ := envelope.Payload["package_id"].(string)
			if packageID == "" {
				t.Fatalf("package_id payload is empty for %s", fixture.name)
			}
			if !containsAnchor(anchors, packageID) {
				t.Fatalf("correlation_anchors %q missing package_id %q", anchors, packageID)
			}
			// Factschema parity: the contract-encoded payload keys must be
			// present alongside the hand-built ones.
			for _, key := range []string{"ecosystem", "registry", "purl", "bom_ref"} {
				if _, ok := envelope.Payload[key]; !ok {
					t.Fatalf("payload missing factschema parity key %q", key)
				}
			}
		})
	}
}

// TestExtractionParityStableKeyAcrossGenerations pins the key contract the
// extracted producer and core reducer share: the same package identity under
// the same scope keeps its StableFactKey across generations while FactID
// advances with the generation.
func TestExtractionParityStableKeyAcrossGenerations(t *testing.T) {
	t.Parallel()

	first := parityContext(EcosystemNPM, "https://registry.npmjs.org")
	first.GenerationID = "generation-a"
	parsed, err := DefaultMetadataParserRegistry().Parse(first, []byte(`{"name": "left-pad", "versions": {}}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	firstEnvelope, err := NewPackageEnvelope(parsed.Packages[0])
	if err != nil {
		t.Fatalf("NewPackageEnvelope() error = %v", err)
	}

	second := first
	second.GenerationID = "generation-b"
	parsed, err = DefaultMetadataParserRegistry().Parse(second, []byte(`{"name": "left-pad", "versions": {}}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	secondEnvelope, err := NewPackageEnvelope(parsed.Packages[0])
	if err != nil {
		t.Fatalf("NewPackageEnvelope() error = %v", err)
	}

	if firstEnvelope.StableFactKey == "" {
		t.Fatal("StableFactKey is empty")
	}
	if firstEnvelope.StableFactKey != secondEnvelope.StableFactKey {
		t.Fatalf("StableFactKey changed across generations: %q vs %q",
			firstEnvelope.StableFactKey, secondEnvelope.StableFactKey)
	}
	if firstEnvelope.FactID == secondEnvelope.FactID {
		t.Fatal("FactID did not advance across generations")
	}
}

// TestExtractionParityHostileDocuments pins source-parity failure modes: the
// destination must reject malformed and identity-free documents with errors
// (the malformed_metadata path) and must refuse unregistered ecosystems (the
// unsupported_metadata_source path) instead of emitting facts.
func TestExtractionParityHostileDocuments(t *testing.T) {
	t.Parallel()

	registry := DefaultMetadataParserRegistry()
	ctx := parityContext(EcosystemNPM, "https://registry.npmjs.org")

	if _, err := registry.Parse(ctx, []byte(`{"name":`)); err == nil {
		t.Fatal("Parse(malformed npm) error = nil, want malformed_metadata error")
	}
	if _, err := registry.Parse(ctx, []byte(`{"name": "", "versions": {}}`)); err == nil {
		t.Fatal("Parse(blank npm name) error = nil, want identity error")
	}
	if _, err := registry.Parse(ctx, nil); err == nil {
		t.Fatal("Parse(empty document) error = nil, want error")
	}

	unregistered := parityContext(EcosystemCargo, "https://crates.io")
	if _, err := registry.Parse(unregistered, []byte(`{}`)); err == nil {
		t.Fatal("Parse(unregistered ecosystem) error = nil, want unsupported error")
	} else if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Parse(unregistered ecosystem) error = %q, want %q", err, "not registered")
	}

	// Hostile context: a blank scope must fail at the envelope boundary,
	// never emit a fact with an empty key.
	parsed, err := registry.Parse(ctx, []byte(`{"name": "left-pad", "versions": {}}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	bad := parsed.Packages[0]
	bad.ScopeID = ""
	if _, err := NewPackageEnvelope(bad); err == nil {
		t.Fatal("NewPackageEnvelope(blank scope) error = nil, want boundary error")
	}
}

// TestExtractionParityWarningTaxonomy pins the envelope-layer warning
// contract: every known collection warning code builds a durable warning fact
// with that code and correlation anchors, blank keys/codes are rejected, and
// credential material in messages is redacted before emission.
func TestExtractionParityWarningTaxonomy(t *testing.T) {
	t.Parallel()

	codes := []string{
		"version_limit_truncated",
		"registry_not_found",
		"metadata_too_large",
		"unsupported_metadata_source",
		"malformed_metadata",
		"credentials_missing",
	}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			t.Parallel()

			envelope, err := NewWarningEnvelope(WarningObservation{
				WarningKey:          code,
				WarningCode:         code,
				Severity:            "warning",
				Message:             "parity probe for " + code,
				ScopeID:             "package-registry://probe/scope",
				GenerationID:        "probe-generation",
				CollectorInstanceID: "probe-collector",
				SourceURI:           "https://registry.example/probe",
			})
			if err != nil {
				t.Fatalf("NewWarningEnvelope(%s) error = %v", code, err)
			}
			if envelope.FactKind != chain.PackageRegistryWarningFactKind {
				t.Fatalf("FactKind = %q, want %q", envelope.FactKind, chain.PackageRegistryWarningFactKind)
			}
			if envelope.SchemaVersion != chain.PackageRegistryWarningSchemaVersion {
				t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, chain.PackageRegistryWarningSchemaVersion)
			}
			if got := envelope.Payload["warning_code"]; got != code {
				t.Fatalf("warning_code = %#v, want %q", got, code)
			}
		})
	}

	blank := WarningObservation{
		ScopeID:             "package-registry://probe/scope",
		GenerationID:        "probe-generation",
		CollectorInstanceID: "probe-collector",
	}
	if _, err := NewWarningEnvelope(blank); err == nil {
		t.Fatal("NewWarningEnvelope(blank key/code) error = nil, want rejection")
	}

	leaky, err := NewWarningEnvelope(WarningObservation{
		WarningKey:          "parity-leak",
		WarningCode:         "malformed_metadata",
		Severity:            "warning",
		Message:             "see https://user:secret@example.com/x?token=abc for detail",
		ScopeID:             "package-registry://probe/scope",
		GenerationID:        "probe-generation",
		CollectorInstanceID: "probe-collector",
	})
	if err != nil {
		t.Fatalf("NewWarningEnvelope() error = %v", err)
	}
	if message, _ := leaky.Payload["message"].(string); strings.Contains(message, "secret") ||
		strings.Contains(message, "token=abc") {
		t.Fatalf("warning message leaks credential material: %q", message)
	}
}

func parityContext(ecosystem Ecosystem, registry string) MetadataParserContext {
	return MetadataParserContext{
		Ecosystem:           ecosystem,
		Registry:            registry,
		ScopeID:             string(ecosystem) + "://parity-scope",
		GenerationID:        "fixture-generation",
		CollectorInstanceID: "fixture-collector",
		FencingToken:        7,
		SourceURI:           registry + "/parity",
	}
}

func assertParityEnvelope(t *testing.T, envelope facts.Envelope, scopeID string) {
	t.Helper()

	if envelope.FactKind != chain.PackageRegistryPackageFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, chain.PackageRegistryPackageFactKind)
	}
	if envelope.SchemaVersion != chain.PackageRegistryPackageSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, chain.PackageRegistryPackageSchemaVersion)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want reported", envelope.SourceConfidence)
	}
	if envelope.ScopeID != scopeID {
		t.Fatalf("ScopeID = %q, want %q", envelope.ScopeID, scopeID)
	}
	if envelope.StableFactKey == "" || envelope.FactID == "" {
		t.Fatal("StableFactKey and FactID must both be set")
	}
}

func containsAnchor(anchors []string, want string) bool {
	for _, anchor := range anchors {
		if anchor == want {
			return true
		}
	}
	return false
}
