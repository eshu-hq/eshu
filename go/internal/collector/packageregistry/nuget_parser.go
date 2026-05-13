package packageregistry

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// ParseNuGetPackageMetadata parses one NuGet nuspec XML fixture into reported
// package, version, dependency, artifact, and source-hint observations.
func ParseNuGetPackageMetadata(ctx MetadataParserContext, document []byte) (ParsedMetadata, error) {
	if err := validateParserContext(ctx, EcosystemNuGet, "nuget package"); err != nil {
		return ParsedMetadata{}, err
	}
	var pkg nugetPackage
	if err := xml.Unmarshal(document, &pkg); err != nil {
		return ParsedMetadata{}, fmt.Errorf("parse nuget package metadata: %w", err)
	}
	name := strings.TrimSpace(pkg.Metadata.ID)
	version := strings.TrimSpace(pkg.Metadata.Version)
	if name == "" {
		return ParsedMetadata{}, fmt.Errorf("nuget package id must not be blank")
	}

	identity := PackageIdentity{Ecosystem: EcosystemNuGet, Registry: ctx.Registry, RawName: name}
	parsed := ParsedMetadata{
		Packages: []PackageObservation{packageObservation(ctx, identity)},
	}
	if version != "" {
		artifactKey := strings.ToLower(name) + "." + strings.ToLower(version) + ".nupkg"
		versionObservation := versionObservation(ctx, identity, version)
		parsed.Versions = append(parsed.Versions, versionObservation)
		parsed.Artifacts = append(parsed.Artifacts, artifactObservation(
			ctx,
			identity,
			version,
			artifactKey,
			"nupkg",
			"",
			artifactKey,
			0,
			nil,
		))
	}
	parsed.Dependencies = nugetDependencies(ctx, identity, version, pkg.Metadata.Dependencies)
	parsed.SourceHints = nugetSourceHints(ctx, identity, version, pkg.Metadata)
	return parsed, nil
}

type nugetPackage struct {
	Metadata nugetMetadata `xml:"metadata"`
}

type nugetMetadata struct {
	ID           string               `xml:"id"`
	Version      string               `xml:"version"`
	ProjectURL   string               `xml:"projectUrl"`
	Repository   nugetRepository      `xml:"repository"`
	Dependencies nugetDependenciesXML `xml:"dependencies"`
}

type nugetRepository struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

type nugetDependenciesXML struct {
	Direct []nugetDependency      `xml:"dependency"`
	Groups []nugetDependencyGroup `xml:"group"`
}

type nugetDependencyGroup struct {
	TargetFramework string            `xml:"targetFramework,attr"`
	Dependencies    []nugetDependency `xml:"dependency"`
}

type nugetDependency struct {
	ID      string `xml:"id,attr"`
	Version string `xml:"version,attr"`
}

func nugetDependencies(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	dependenciesXML nugetDependenciesXML,
) []PackageDependencyObservation {
	var dependencies []PackageDependencyObservation
	for _, group := range dependenciesXML.Groups {
		for _, dependency := range group.Dependencies {
			observation, ok := nugetDependencyObservation(ctx, identity, version, dependency)
			if !ok {
				continue
			}
			observation.TargetFramework = strings.TrimSpace(group.TargetFramework)
			dependencies = append(dependencies, observation)
		}
	}
	for _, dependency := range dependenciesXML.Direct {
		observation, ok := nugetDependencyObservation(ctx, identity, version, dependency)
		if ok {
			dependencies = append(dependencies, observation)
		}
	}
	return dependencies
}

func nugetDependencyObservation(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	dependency nugetDependency,
) (PackageDependencyObservation, bool) {
	name := strings.TrimSpace(dependency.ID)
	if name == "" {
		return PackageDependencyObservation{}, false
	}
	return dependencyObservation(
		ctx,
		identity,
		version,
		PackageIdentity{Ecosystem: EcosystemNuGet, Registry: ctx.Registry, RawName: name},
		dependency.Version,
		"runtime",
	), true
}

func nugetSourceHints(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	metadata nugetMetadata,
) []SourceHintObservation {
	var hints []SourceHintObservation
	if strings.TrimSpace(metadata.Repository.URL) != "" {
		hints = append(hints, sourceHintObservation(ctx, identity, version, "repository", metadata.Repository.URL, "nuget-repository-field"))
	}
	if strings.TrimSpace(metadata.ProjectURL) != "" {
		hints = append(hints, sourceHintObservation(ctx, identity, version, "homepage", metadata.ProjectURL, "nuget-project-url-field"))
	}
	return hints
}
