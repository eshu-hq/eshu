package packageregistry

import (
	"testing"
	"time"
)

func TestDefaultMetadataParserRegistryRoutesByExplicitEcosystem(t *testing.T) {
	t.Parallel()

	registry := DefaultMetadataParserRegistry()
	metadata, err := registry.Parse(
		parserTestContext(EcosystemNPM, "https://registry.npmjs.org/"),
		[]byte(`{"name":"left-pad","versions":{"1.0.0":{}}}`),
	)
	if err != nil {
		t.Fatalf("MetadataParserRegistry.Parse() error = %v", err)
	}

	if got := metadata.Packages[0].Identity.Ecosystem; got != EcosystemNPM {
		t.Fatalf("parsed ecosystem = %q, want %q", got, EcosystemNPM)
	}
}

func TestMetadataParserRegistryAllowsEcosystemRegistration(t *testing.T) {
	t.Parallel()

	registry := MetadataParserRegistry{}
	err := registry.Register(EcosystemMaven, func(ctx MetadataParserContext, _ []byte) (ParsedMetadata, error) {
		return ParsedMetadata{
			Packages: []PackageObservation{{
				Identity: PackageIdentity{
					Ecosystem: ctx.Ecosystem,
					Registry:  ctx.Registry,
					Namespace: "org.example",
					RawName:   "demo",
				},
				ScopeID:             ctx.ScopeID,
				GenerationID:        ctx.GenerationID,
				CollectorInstanceID: ctx.CollectorInstanceID,
				FencingToken:        ctx.FencingToken,
				ObservedAt:          ctx.ObservedAt,
				SourceURI:           ctx.SourceURI,
			}},
		}, nil
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	metadata, err := registry.Parse(MetadataParserContext{
		Ecosystem:           EcosystemMaven,
		Registry:            "https://repo.maven.apache.org/maven2",
		ScopeID:             "maven://repo.maven.apache.org/maven2/org.example:demo",
		GenerationID:        "fixture",
		CollectorInstanceID: "collector",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
		SourceURI:           "https://repo.maven.apache.org/maven2/org/example/demo",
	}, []byte(`<metadata/>`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := metadata.Packages[0].Identity.Ecosystem; got != EcosystemMaven {
		t.Fatalf("registered parser ecosystem = %q, want %q", got, EcosystemMaven)
	}
}

func TestMetadataParserRegistryRejectsMissingOrDuplicateParsers(t *testing.T) {
	t.Parallel()

	registry := MetadataParserRegistry{}
	if err := registry.Register("", func(MetadataParserContext, []byte) (ParsedMetadata, error) {
		return ParsedMetadata{}, nil
	}); err == nil {
		t.Fatal("Register() missing ecosystem error = nil, want error")
	}
	if err := registry.Register(EcosystemGoModule, nil); err == nil {
		t.Fatal("Register() nil parser error = nil, want error")
	}
	if err := registry.Register(EcosystemGoModule, func(MetadataParserContext, []byte) (ParsedMetadata, error) {
		return ParsedMetadata{}, nil
	}); err != nil {
		t.Fatalf("Register() first parser error = %v", err)
	}
	if err := registry.Register(EcosystemGoModule, func(MetadataParserContext, []byte) (ParsedMetadata, error) {
		return ParsedMetadata{}, nil
	}); err == nil {
		t.Fatal("Register() duplicate parser error = nil, want error")
	}
	if _, err := registry.Parse(parserTestContext(EcosystemNuGet, "https://api.nuget.org/v3/index.json"), []byte(`{}`)); err == nil {
		t.Fatal("Parse() missing parser error = nil, want error")
	}
}
