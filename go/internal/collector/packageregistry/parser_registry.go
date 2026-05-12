package packageregistry

import (
	"fmt"
	"strings"
)

// MetadataParser parses one ecosystem-native package metadata document into
// package-registry observations.
type MetadataParser func(MetadataParserContext, []byte) (ParsedMetadata, error)

// MetadataParserRegistry routes package metadata documents to explicit
// ecosystem parsers.
type MetadataParserRegistry struct {
	parsers map[Ecosystem]MetadataParser
}

// DefaultMetadataParserRegistry returns the deterministic fixture parsers that
// are implemented in this package today.
func DefaultMetadataParserRegistry() MetadataParserRegistry {
	registry := MetadataParserRegistry{}
	mustRegisterParser(&registry, EcosystemNPM, ParseNPMPackumentMetadata)
	mustRegisterParser(&registry, EcosystemPyPI, ParsePyPIProjectMetadata)
	mustRegisterParser(&registry, EcosystemGeneric, ParseGenericPackageMetadata)
	return registry
}

// Register adds one ecosystem parser. Callers add new ecosystems by registering
// another parser rather than by expanding source-runtime switches.
func (r *MetadataParserRegistry) Register(ecosystem Ecosystem, parser MetadataParser) error {
	ecosystem = Ecosystem(strings.TrimSpace(string(ecosystem)))
	if ecosystem == "" {
		return fmt.Errorf("metadata parser ecosystem is required")
	}
	if parser == nil {
		return fmt.Errorf("metadata parser for ecosystem %q is required", ecosystem)
	}
	if r.parsers == nil {
		r.parsers = map[Ecosystem]MetadataParser{}
	}
	if _, exists := r.parsers[ecosystem]; exists {
		return fmt.Errorf("metadata parser for ecosystem %q is already registered", ecosystem)
	}
	r.parsers[ecosystem] = parser
	return nil
}

// Parse routes one metadata document to the parser registered for
// ctx.Ecosystem.
func (r MetadataParserRegistry) Parse(ctx MetadataParserContext, document []byte) (ParsedMetadata, error) {
	ecosystem := Ecosystem(strings.TrimSpace(string(ctx.Ecosystem)))
	if ecosystem == "" {
		return ParsedMetadata{}, fmt.Errorf("metadata parser ecosystem is required")
	}
	parser, ok := r.parsers[ecosystem]
	if !ok {
		return ParsedMetadata{}, fmt.Errorf("metadata parser for ecosystem %q is not registered", ecosystem)
	}
	ctx.Ecosystem = ecosystem
	return parser(ctx, document)
}

func mustRegisterParser(registry *MetadataParserRegistry, ecosystem Ecosystem, parser MetadataParser) {
	if err := registry.Register(ecosystem, parser); err != nil {
		panic(err)
	}
}
