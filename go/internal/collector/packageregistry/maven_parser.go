package packageregistry

import (
	"encoding/xml"
	"fmt"
	"path"
	"strings"
)

// ParseMavenPackageMetadata parses one Maven POM XML fixture into reported
// package, version, dependency, artifact, and source-hint observations.
func ParseMavenPackageMetadata(ctx MetadataParserContext, document []byte) (ParsedMetadata, error) {
	if err := validateParserContext(ctx, EcosystemMaven, "maven package"); err != nil {
		return ParsedMetadata{}, err
	}
	var project mavenProject
	if err := xml.Unmarshal(document, &project); err != nil {
		return ParsedMetadata{}, fmt.Errorf("parse maven package metadata: %w", err)
	}
	groupID := firstNonBlank(project.GroupID, project.Parent.GroupID)
	artifactID := strings.TrimSpace(project.ArtifactID)
	version := firstNonBlank(project.Version, project.Parent.Version)
	if groupID == "" || artifactID == "" {
		return ParsedMetadata{}, fmt.Errorf("maven package metadata requires groupId and artifactId")
	}

	identity := PackageIdentity{
		Ecosystem: EcosystemMaven,
		Registry:  ctx.Registry,
		RawName:   artifactID,
		Namespace: groupID,
	}
	parsed := ParsedMetadata{
		Packages: []PackageObservation{packageObservation(ctx, identity)},
	}
	if version != "" {
		versionObservation := versionObservation(ctx, identity, version)
		artifact := mavenArtifactObservation(ctx, identity, version, project.Packaging)
		versionObservation.ArtifactURLs = appendUniqueString(versionObservation.ArtifactURLs, artifact.ArtifactURL)
		parsed.Versions = append(parsed.Versions, versionObservation)
		parsed.Artifacts = append(parsed.Artifacts, artifact)
	}
	parsed.Dependencies = mavenDependencies(ctx, identity, version, project.Dependencies)
	parsed.SourceHints = mavenSourceHints(ctx, identity, version, project)
	return parsed, nil
}

type mavenProject struct {
	GroupID      string            `xml:"groupId"`
	ArtifactID   string            `xml:"artifactId"`
	Version      string            `xml:"version"`
	Packaging    string            `xml:"packaging"`
	URL          string            `xml:"url"`
	Parent       mavenParent       `xml:"parent"`
	SCM          mavenSCM          `xml:"scm"`
	Dependencies []mavenDependency `xml:"dependencies>dependency"`
}

type mavenParent struct {
	GroupID string `xml:"groupId"`
	Version string `xml:"version"`
}

type mavenSCM struct {
	URL          string `xml:"url"`
	Connection   string `xml:"connection"`
	DeveloperURL string `xml:"developerConnection"`
}

type mavenDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
	Classifier string `xml:"classifier"`
}

func mavenArtifactObservation(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	packaging string,
) PackageArtifactObservation {
	artifactType := firstNonBlank(packaging, "jar")
	artifactKey := path.Join(
		strings.ReplaceAll(identity.Namespace, ".", "/"),
		identity.RawName,
		version,
		identity.RawName+"-"+version+"."+artifactType,
	)
	return artifactObservation(ctx, identity, version, artifactKey, artifactType, "", artifactKey, 0, nil)
}

func mavenDependencies(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	rawDependencies []mavenDependency,
) []PackageDependencyObservation {
	dependencies := make([]PackageDependencyObservation, 0, len(rawDependencies))
	for _, dependency := range rawDependencies {
		groupID := strings.TrimSpace(dependency.GroupID)
		artifactID := strings.TrimSpace(dependency.ArtifactID)
		if groupID == "" || artifactID == "" {
			continue
		}
		observation := dependencyObservation(
			ctx,
			identity,
			version,
			PackageIdentity{
				Ecosystem:  EcosystemMaven,
				Registry:   ctx.Registry,
				RawName:    artifactID,
				Namespace:  groupID,
				Classifier: strings.TrimSpace(dependency.Classifier),
			},
			dependency.Version,
			firstNonBlank(dependency.Scope, "compile"),
		)
		observation.Optional = strings.EqualFold(strings.TrimSpace(dependency.Optional), "true")
		dependencies = append(dependencies, observation)
	}
	return dependencies
}

func mavenSourceHints(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	project mavenProject,
) []SourceHintObservation {
	var hints []SourceHintObservation
	if scmURL := mavenSCMURL(project.SCM); scmURL != "" {
		hints = append(hints, sourceHintObservation(ctx, identity, version, "repository", scmURL, "maven-scm-field"))
	}
	if strings.TrimSpace(project.URL) != "" {
		hints = append(hints, sourceHintObservation(ctx, identity, version, "homepage", project.URL, "maven-url-field"))
	}
	return hints
}

func mavenSCMURL(scm mavenSCM) string {
	raw := firstNonBlank(scm.URL, scm.Connection, scm.DeveloperURL)
	raw = strings.TrimPrefix(raw, "scm:git:")
	raw = strings.TrimPrefix(raw, "scm:")
	return raw
}
