package packageregistry

import (
	"encoding/json"
	"fmt"
	"strings"
)

var defaultArtifactoryPackageMetadataParserRegistry = DefaultMetadataParserRegistry()

// ParseArtifactoryPackageMetadata parses an Artifactory package-feed wrapper
// while preserving package-native metadata semantics.
func ParseArtifactoryPackageMetadata(ctx MetadataParserContext, document []byte) (ParsedMetadata, error) {
	return ParseArtifactoryPackageMetadataWithRegistry(ctx, document, defaultArtifactoryPackageMetadataParserRegistry)
}

// ParseArtifactoryPackageMetadataWithRegistry parses an Artifactory
// package-feed wrapper with the caller's ecosystem parser registry.
func ParseArtifactoryPackageMetadataWithRegistry(
	ctx MetadataParserContext,
	document []byte,
	registry MetadataParserRegistry,
) (ParsedMetadata, error) {
	var wrapper artifactoryPackageMetadata
	if err := json.Unmarshal(document, &wrapper); err != nil {
		return ParsedMetadata{}, fmt.Errorf("parse artifactory package metadata: %w", err)
	}
	if err := validateArtifactoryPackageType(ctx.Ecosystem, wrapper.PackageType); err != nil {
		return ParsedMetadata{}, err
	}
	nativeDocument, err := artifactoryNativeMetadataDocument(wrapper.Metadata)
	if err != nil {
		return ParsedMetadata{}, err
	}
	parsed, err := registry.Parse(ctx, nativeDocument)
	if err != nil {
		return ParsedMetadata{}, err
	}
	if hosting, ok := artifactoryRepositoryHosting(ctx, wrapper); ok {
		parsed.Hosting = append(parsed.Hosting, hosting)
	}
	return parsed, nil
}

type artifactoryPackageMetadata struct {
	Provider       string          `json:"provider"`
	Repository     string          `json:"repository"`
	RepositoryType string          `json:"repository_type"`
	PackageType    string          `json:"package_type"`
	UpstreamID     string          `json:"upstream_id"`
	UpstreamURL    string          `json:"upstream_url"`
	Metadata       json.RawMessage `json:"metadata"`
}

func validateArtifactoryPackageType(ecosystem Ecosystem, packageType string) error {
	normalizedPackageType := normalizeArtifactoryPackageType(packageType)
	if normalizedPackageType == "" {
		return nil
	}
	if normalizedPackageType != ecosystem {
		return fmt.Errorf(
			"artifactory package_type %q does not match parser ecosystem %q",
			strings.TrimSpace(packageType),
			ecosystem,
		)
	}
	return nil
}

func normalizeArtifactoryPackageType(packageType string) Ecosystem {
	switch strings.ToLower(strings.TrimSpace(packageType)) {
	case "":
		return ""
	case "npm":
		return EcosystemNPM
	case "pypi", "python":
		return EcosystemPyPI
	case "go", "gomod", "go-module", "go_module":
		return EcosystemGoModule
	case "maven", "gradle":
		return EcosystemMaven
	case "nuget":
		return EcosystemNuGet
	case "generic":
		return EcosystemGeneric
	default:
		return Ecosystem(strings.TrimSpace(packageType))
	}
}

func artifactoryNativeMetadataDocument(metadata json.RawMessage) ([]byte, error) {
	if len(metadata) == 0 || strings.TrimSpace(string(metadata)) == "null" {
		return nil, fmt.Errorf("artifactory package metadata is required")
	}
	var text string
	if err := json.Unmarshal(metadata, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("artifactory package metadata must not be blank")
		}
		return []byte(text), nil
	}
	return []byte(metadata), nil
}

func artifactoryRepositoryHosting(
	ctx MetadataParserContext,
	wrapper artifactoryPackageMetadata,
) (RepositoryHostingObservation, bool) {
	provider := firstNonBlank(wrapper.Provider, "jfrog")
	repository := strings.TrimSpace(wrapper.Repository)
	if repository == "" {
		return RepositoryHostingObservation{}, false
	}
	return RepositoryHostingObservation{
		Provider:            provider,
		Registry:            ctx.Registry,
		Repository:          repository,
		RepositoryType:      strings.TrimSpace(wrapper.RepositoryType),
		Ecosystem:           ctx.Ecosystem,
		UpstreamID:          strings.TrimSpace(wrapper.UpstreamID),
		UpstreamURL:         sanitizeURL(wrapper.UpstreamURL),
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		SourceURI:           ctx.SourceURI,
	}, true
}
