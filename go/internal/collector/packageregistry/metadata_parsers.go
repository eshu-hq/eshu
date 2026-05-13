package packageregistry

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseNPMPackumentMetadata parses one npm packument fixture into reported
// package, version, dependency, artifact, and source-hint observations.
func ParseNPMPackumentMetadata(ctx MetadataParserContext, document []byte) (ParsedMetadata, error) {
	if err := validateParserContext(ctx, EcosystemNPM, "npm packument"); err != nil {
		return ParsedMetadata{}, err
	}
	var packument npmPackument
	if err := json.Unmarshal(document, &packument); err != nil {
		return ParsedMetadata{}, fmt.Errorf("parse npm packument metadata: %w", err)
	}
	name := strings.TrimSpace(packument.Name)
	if name == "" {
		return ParsedMetadata{}, fmt.Errorf("npm packument package name must not be blank")
	}

	identity := PackageIdentity{Ecosystem: EcosystemNPM, Registry: ctx.Registry, RawName: name}
	parsed := ParsedMetadata{
		Packages: []PackageObservation{packageObservation(ctx, identity)},
	}
	for _, version := range sortedMapKeys(packument.Versions) {
		entry := packument.Versions[version]
		versionObservation := versionObservation(ctx, identity, version)
		versionObservation.Deprecated = entry.deprecated()
		if entry.Dist.Tarball != "" {
			versionObservation.ArtifactURLs = []string{sanitizeURL(entry.Dist.Tarball)}
			versionObservation.Checksums = npmChecksums(entry.Dist)
			parsed.Artifacts = append(parsed.Artifacts, artifactObservation(
				ctx,
				identity,
				version,
				artifactKey(entry.Dist.Tarball),
				"tarball",
				entry.Dist.Tarball,
				"",
				0,
				npmChecksums(entry.Dist),
			))
		}
		parsed.Versions = append(parsed.Versions, versionObservation)
		parsed.Dependencies = append(parsed.Dependencies, npmDependencies(ctx, identity, version, entry)...)
		parsed.SourceHints = append(parsed.SourceHints, npmSourceHints(ctx, identity, version, entry)...)
	}
	return parsed, nil
}

// ParsePyPIProjectMetadata parses one PyPI JSON API fixture into reported
// package, version, dependency, artifact, and source-hint observations.
func ParsePyPIProjectMetadata(ctx MetadataParserContext, document []byte) (ParsedMetadata, error) {
	if err := validateParserContext(ctx, EcosystemPyPI, "pypi project"); err != nil {
		return ParsedMetadata{}, err
	}
	var project pypiProject
	if err := json.Unmarshal(document, &project); err != nil {
		return ParsedMetadata{}, fmt.Errorf("parse pypi project metadata: %w", err)
	}
	name := strings.TrimSpace(project.Info.Name)
	if name == "" {
		return ParsedMetadata{}, fmt.Errorf("pypi project package name must not be blank")
	}

	identity := PackageIdentity{Ecosystem: EcosystemPyPI, Registry: ctx.Registry, RawName: name}
	parsed := ParsedMetadata{
		Packages: []PackageObservation{packageObservation(ctx, identity)},
	}
	for _, version := range sortedMapKeys(project.Releases) {
		files := project.Releases[version]
		versionObservation := versionObservation(ctx, identity, version)
		for _, file := range files {
			artifactURL := sanitizeURL(file.URL)
			if artifactURL != "" {
				versionObservation.ArtifactURLs = appendUniqueString(versionObservation.ArtifactURLs, artifactURL)
			}
			if file.Yanked {
				versionObservation.Yanked = true
			}
			if versionObservation.PublishedAt.IsZero() {
				versionObservation.PublishedAt = parseTimestamp(file.UploadTime)
			}
			parsed.Artifacts = append(parsed.Artifacts, artifactObservation(
				ctx,
				identity,
				version,
				firstNonBlank(file.Filename, artifactKey(file.URL)),
				file.PackageType,
				file.URL,
				file.Filename,
				file.Size,
				file.Digests,
			))
		}
		parsed.Versions = append(parsed.Versions, versionObservation)
	}
	if len(parsed.Versions) == 0 && strings.TrimSpace(project.Info.Version) != "" {
		parsed.Versions = append(parsed.Versions, versionObservation(ctx, identity, project.Info.Version))
	}
	dependencyVersion := firstParsedVersion(parsed.Versions, project.Info.Version)
	parsed.Dependencies = pypiDependencies(ctx, identity, dependencyVersion, project.Info.RequiresDist)
	parsed.SourceHints = pypiSourceHints(ctx, identity, dependencyVersion, project.Info.ProjectURLs)
	return parsed, nil
}

// ParseGenericPackageMetadata parses one provider-specific generic package
// metadata fixture into reported package-registry observations.
func ParseGenericPackageMetadata(ctx MetadataParserContext, document []byte) (ParsedMetadata, error) {
	if err := validateParserContext(ctx, EcosystemGeneric, "generic package"); err != nil {
		return ParsedMetadata{}, err
	}
	var generic genericMetadata
	if err := json.Unmarshal(document, &generic); err != nil {
		return ParsedMetadata{}, fmt.Errorf("parse generic package metadata: %w", err)
	}
	name := strings.TrimSpace(generic.Name)
	if name == "" {
		return ParsedMetadata{}, fmt.Errorf("generic package name must not be blank")
	}

	identity := PackageIdentity{
		Ecosystem: EcosystemGeneric,
		Registry:  ctx.Registry,
		RawName:   name,
		Namespace: strings.TrimSpace(generic.Namespace),
	}
	parsed := ParsedMetadata{
		Packages: []PackageObservation{packageObservationWithVisibility(ctx, identity, parseVisibility(generic.Visibility))},
	}
	version := strings.TrimSpace(generic.Version)
	if version != "" {
		versionObservation := versionObservation(ctx, identity, version)
		seenArtifacts := map[string]bool{}
		for _, artifact := range generic.Artifacts {
			key := firstNonBlank(artifact.Key, artifact.Path, artifactKey(artifact.URL))
			if key == "" || seenArtifacts[key] {
				continue
			}
			seenArtifacts[key] = true
			hashes := artifact.hashes()
			artifactURL := sanitizeURL(artifact.URL)
			versionObservation.ArtifactURLs = appendUniqueString(versionObservation.ArtifactURLs, artifactURL)
			parsed.Artifacts = append(parsed.Artifacts, artifactObservation(
				ctx,
				identity,
				version,
				key,
				firstNonBlank(artifact.Type, "generic"),
				artifact.URL,
				firstNonBlank(artifact.Path, key),
				artifact.Size,
				hashes,
			))
		}
		parsed.Versions = append(parsed.Versions, versionObservation)
	}
	parsed.Vulnerables = append(parsed.Vulnerables, genericVulnerabilityHints(ctx, identity, version, generic)...)
	parsed.Events = append(parsed.Events, genericRegistryEvents(ctx, identity, version, generic.Events)...)
	if generic.SourceURL != "" {
		parsed.SourceHints = append(parsed.SourceHints, sourceHintObservation(
			ctx,
			identity,
			version,
			"repository",
			generic.SourceURL,
			"generic-metadata-source-url",
		))
	}
	if generic.Provider != "" && generic.Repository != "" {
		parsed.Hosting = append(parsed.Hosting, RepositoryHostingObservation{
			Provider:            strings.TrimSpace(generic.Provider),
			Registry:            ctx.Registry,
			Repository:          strings.TrimSpace(generic.Repository),
			RepositoryType:      strings.TrimSpace(generic.RepositoryType),
			Ecosystem:           EcosystemGeneric,
			ScopeID:             ctx.ScopeID,
			GenerationID:        ctx.GenerationID,
			CollectorInstanceID: ctx.CollectorInstanceID,
			FencingToken:        ctx.FencingToken,
			ObservedAt:          ctx.ObservedAt,
			SourceURI:           ctx.SourceURI,
		})
	}
	return parsed, nil
}

type npmPackument struct {
	Name     string                `json:"name"`
	Versions map[string]npmVersion `json:"versions"`
}

type npmVersion struct {
	Deprecated           any               `json:"deprecated"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	Dist                 npmDist           `json:"dist"`
	Repository           any               `json:"repository"`
	Homepage             string            `json:"homepage"`
}

type npmDist struct {
	Tarball   string `json:"tarball"`
	Integrity string `json:"integrity"`
	Shasum    string `json:"shasum"`
}

type pypiProject struct {
	Info     pypiInfo              `json:"info"`
	Releases map[string][]pypiFile `json:"releases"`
}

type pypiInfo struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	RequiresDist []string          `json:"requires_dist"`
	ProjectURLs  map[string]string `json:"project_urls"`
}

type pypiFile struct {
	PackageType string            `json:"packagetype"`
	URL         string            `json:"url"`
	Filename    string            `json:"filename"`
	Size        int64             `json:"size"`
	UploadTime  string            `json:"upload_time_iso_8601"`
	Yanked      bool              `json:"yanked"`
	Digests     map[string]string `json:"digests"`
}

type genericMetadata struct {
	Provider        string                 `json:"provider"`
	Repository      string                 `json:"repository"`
	RepositoryType  string                 `json:"repository_type"`
	Name            string                 `json:"name"`
	Namespace       string                 `json:"namespace"`
	Version         string                 `json:"version"`
	Visibility      string                 `json:"visibility"`
	SourceURL       string                 `json:"source_url"`
	Artifacts       []genericArtifact      `json:"artifacts"`
	Vulnerabilities []genericVulnerability `json:"vulnerabilities"`
	Advisories      []genericVulnerability `json:"advisories"`
	Events          []genericRegistryEvent `json:"events"`
}

type genericArtifact struct {
	Key    string            `json:"key"`
	URL    string            `json:"url"`
	Path   string            `json:"path"`
	Type   string            `json:"type"`
	Size   int64             `json:"size"`
	SHA256 string            `json:"sha256"`
	SHA1   string            `json:"sha1"`
	MD5    string            `json:"md5"`
	Hashes map[string]string `json:"hashes"`
}

type genericVulnerability struct {
	AdvisoryID      string `json:"advisory_id"`
	AdvisorySource  string `json:"advisory_source"`
	VulnerabilityID string `json:"vulnerability_id"`
	SourceSeverity  string `json:"source_severity"`
	AffectedRange   string `json:"affected_range"`
	FixedVersion    string `json:"fixed_version"`
	URL             string `json:"url"`
	Summary         string `json:"summary"`
	PublishedAt     string `json:"published_at"`
	ModifiedAt      string `json:"modified_at"`
}

type genericRegistryEvent struct {
	EventKey    string `json:"event_key"`
	EventType   string `json:"event_type"`
	ArtifactKey string `json:"artifact_key"`
	Actor       string `json:"actor"`
	Message     string `json:"message"`
	OccurredAt  string `json:"occurred_at"`
}
