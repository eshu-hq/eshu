package packageregistry

import (
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

func validateParserContext(ctx MetadataParserContext, ecosystem Ecosystem, documentType string) error {
	if ctx.Ecosystem != ecosystem {
		return fmt.Errorf("%s parser ecosystem = %q, want %q", documentType, ctx.Ecosystem, ecosystem)
	}
	if ctx.Registry == "" {
		return fmt.Errorf("%s parser registry must not be blank", documentType)
	}
	return validateObservationBoundary(
		ctx.ScopeID,
		ctx.GenerationID,
		ctx.CollectorInstanceID,
		documentType+" parser context",
	)
}

func packageObservation(ctx MetadataParserContext, identity PackageIdentity) PackageObservation {
	return packageObservationWithVisibility(ctx, identity, ctx.Visibility)
}

func packageObservationWithVisibility(
	ctx MetadataParserContext,
	identity PackageIdentity,
	visibility Visibility,
) PackageObservation {
	if visibility == "" {
		visibility = VisibilityUnknown
	}
	return PackageObservation{
		Identity:            identity,
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		Visibility:          visibility,
		SourceURI:           ctx.SourceURI,
	}
}

func versionObservation(ctx MetadataParserContext, identity PackageIdentity, version string) PackageVersionObservation {
	return PackageVersionObservation{
		Package:             identity,
		Version:             strings.TrimSpace(version),
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		SourceURI:           ctx.SourceURI,
	}
}

func dependencyObservation(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	dependency PackageIdentity,
	rangeValue string,
	dependencyType string,
) PackageDependencyObservation {
	return PackageDependencyObservation{
		Package:             identity,
		Version:             strings.TrimSpace(version),
		Dependency:          dependency,
		Range:               strings.TrimSpace(rangeValue),
		DependencyType:      strings.TrimSpace(dependencyType),
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		SourceURI:           ctx.SourceURI,
	}
}

func artifactObservation(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	key string,
	artifactType string,
	artifactURL string,
	artifactPath string,
	sizeBytes int64,
	hashes map[string]string,
) PackageArtifactObservation {
	return PackageArtifactObservation{
		Package:             identity,
		Version:             strings.TrimSpace(version),
		ArtifactKey:         strings.TrimSpace(key),
		ArtifactType:        strings.TrimSpace(artifactType),
		ArtifactURL:         sanitizeURL(artifactURL),
		ArtifactPath:        strings.TrimSpace(artifactPath),
		SizeBytes:           sizeBytes,
		Hashes:              cloneStringMap(hashes),
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		SourceURI:           ctx.SourceURI,
	}
}

func sourceHintObservation(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	hintKind string,
	rawURL string,
	confidenceReason string,
) SourceHintObservation {
	sanitized := sanitizeURL(stripGitURLDecorations(rawURL))
	return SourceHintObservation{
		Package:             identity,
		Version:             strings.TrimSpace(version),
		HintKind:            strings.TrimSpace(hintKind),
		RawURL:              sanitized,
		NormalizedURL:       sanitized,
		ConfidenceReason:    strings.TrimSpace(confidenceReason),
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		SourceURI:           ctx.SourceURI,
	}
}

func genericVulnerabilityHints(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	metadata genericMetadata,
) []VulnerabilityHintObservation {
	vulnerabilities := append([]genericVulnerability{}, metadata.Vulnerabilities...)
	vulnerabilities = append(vulnerabilities, metadata.Advisories...)
	seen := map[string]bool{}
	observations := make([]VulnerabilityHintObservation, 0, len(vulnerabilities))
	for _, vulnerability := range vulnerabilities {
		advisoryID := firstNonBlank(vulnerability.AdvisoryID, vulnerability.VulnerabilityID)
		advisorySource := firstNonBlank(vulnerability.AdvisorySource, metadata.Provider, "generic")
		if advisoryID == "" || advisorySource == "" {
			continue
		}
		key := advisorySource + "\x00" + advisoryID + "\x00" + strings.TrimSpace(vulnerability.VulnerabilityID)
		if seen[key] {
			continue
		}
		seen[key] = true
		observations = append(observations, VulnerabilityHintObservation{
			Package:             identity,
			Version:             strings.TrimSpace(version),
			AdvisoryID:          advisoryID,
			AdvisorySource:      advisorySource,
			VulnerabilityID:     strings.TrimSpace(vulnerability.VulnerabilityID),
			SourceSeverity:      strings.TrimSpace(vulnerability.SourceSeverity),
			AffectedRange:       strings.TrimSpace(vulnerability.AffectedRange),
			FixedVersion:        strings.TrimSpace(vulnerability.FixedVersion),
			URL:                 sanitizeURL(vulnerability.URL),
			Summary:             sanitizeText(strings.TrimSpace(vulnerability.Summary)),
			PublishedAt:         parseTimestamp(vulnerability.PublishedAt),
			ModifiedAt:          parseTimestamp(vulnerability.ModifiedAt),
			ScopeID:             ctx.ScopeID,
			GenerationID:        ctx.GenerationID,
			CollectorInstanceID: ctx.CollectorInstanceID,
			FencingToken:        ctx.FencingToken,
			ObservedAt:          ctx.ObservedAt,
			SourceURI:           ctx.SourceURI,
		})
	}
	return observations
}

func genericRegistryEvents(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	events []genericRegistryEvent,
) []RegistryEventObservation {
	seen := map[string]bool{}
	observations := make([]RegistryEventObservation, 0, len(events))
	for _, event := range events {
		eventKey := strings.TrimSpace(event.EventKey)
		eventType := strings.TrimSpace(event.EventType)
		if eventKey == "" || eventType == "" || seen[eventKey] {
			continue
		}
		seen[eventKey] = true
		observations = append(observations, RegistryEventObservation{
			Package:             identity,
			Version:             strings.TrimSpace(version),
			EventKey:            eventKey,
			EventType:           eventType,
			ArtifactKey:         strings.TrimSpace(event.ArtifactKey),
			Actor:               strings.TrimSpace(event.Actor),
			Message:             sanitizeText(strings.TrimSpace(event.Message)),
			OccurredAt:          parseTimestamp(event.OccurredAt),
			ScopeID:             ctx.ScopeID,
			GenerationID:        ctx.GenerationID,
			CollectorInstanceID: ctx.CollectorInstanceID,
			FencingToken:        ctx.FencingToken,
			ObservedAt:          ctx.ObservedAt,
			SourceURI:           ctx.SourceURI,
		})
	}
	return observations
}

func npmDependencies(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	entry npmVersion,
) []PackageDependencyObservation {
	var dependencies []PackageDependencyObservation
	dependencies = append(dependencies, npmDependencySet(ctx, identity, version, entry.Dependencies, "runtime", false)...)
	dependencies = append(dependencies, npmDependencySet(ctx, identity, version, entry.DevDependencies, "development", false)...)
	dependencies = append(dependencies, npmDependencySet(ctx, identity, version, entry.OptionalDependencies, "optional", true)...)
	dependencies = append(dependencies, npmDependencySet(ctx, identity, version, entry.PeerDependencies, "peer", false)...)
	return dependencies
}

func npmDependencySet(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	dependencies map[string]string,
	dependencyType string,
	optional bool,
) []PackageDependencyObservation {
	var observations []PackageDependencyObservation
	for _, name := range sortedMapKeys(dependencies) {
		observation := dependencyObservation(
			ctx,
			identity,
			version,
			PackageIdentity{Ecosystem: EcosystemNPM, Registry: ctx.Registry, RawName: name},
			dependencies[name],
			dependencyType,
		)
		observation.Optional = optional
		observations = append(observations, observation)
	}
	return observations
}

func npmSourceHints(ctx MetadataParserContext, identity PackageIdentity, version string, entry npmVersion) []SourceHintObservation {
	var hints []SourceHintObservation
	if repositoryURL := npmRepositoryURL(entry.Repository); repositoryURL != "" {
		hints = append(hints, sourceHintObservation(ctx, identity, version, "repository", repositoryURL, "npm-repository-field"))
	}
	if strings.TrimSpace(entry.Homepage) != "" {
		hints = append(hints, sourceHintObservation(ctx, identity, version, "homepage", entry.Homepage, "npm-homepage-field"))
	}
	return hints
}

func npmRepositoryURL(repository any) string {
	switch value := repository.(type) {
	case string:
		return value
	case map[string]any:
		if raw, ok := value["url"].(string); ok {
			return raw
		}
	}
	return ""
}

func (entry npmVersion) deprecated() bool {
	switch value := entry.Deprecated.(type) {
	case bool:
		return value
	case string:
		return strings.TrimSpace(value) != ""
	default:
		return false
	}
}

func npmChecksums(dist npmDist) map[string]string {
	checksums := map[string]string{}
	if strings.TrimSpace(dist.Integrity) != "" {
		checksums["integrity"] = strings.TrimSpace(dist.Integrity)
	}
	if strings.TrimSpace(dist.Shasum) != "" {
		checksums["sha1"] = strings.TrimSpace(dist.Shasum)
	}
	if len(checksums) == 0 {
		return nil
	}
	return checksums
}

func pypiDependencies(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	requirements []string,
) []PackageDependencyObservation {
	var dependencies []PackageDependencyObservation
	for _, requirement := range requirements {
		name, rangeValue, marker := parseRequirement(requirement)
		if name == "" {
			continue
		}
		observation := dependencyObservation(
			ctx,
			identity,
			version,
			PackageIdentity{Ecosystem: EcosystemPyPI, Registry: ctx.Registry, RawName: name},
			rangeValue,
			"runtime",
		)
		observation.Marker = marker
		dependencies = append(dependencies, observation)
	}
	return dependencies
}

func pypiSourceHints(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	projectURLs map[string]string,
) []SourceHintObservation {
	var hints []SourceHintObservation
	for _, label := range sortedMapKeys(projectURLs) {
		rawURL := projectURLs[label]
		kind := "project"
		normalizedLabel := strings.ToLower(label)
		if strings.Contains(normalizedLabel, "source") || strings.Contains(normalizedLabel, "repo") {
			kind = "repository"
		}
		if strings.Contains(normalizedLabel, "home") {
			kind = "homepage"
		}
		hints = append(hints, sourceHintObservation(ctx, identity, version, kind, rawURL, "pypi-project-url:"+normalizedLabel))
	}
	return hints
}

func parseRequirement(requirement string) (string, string, string) {
	beforeMarker, marker, _ := strings.Cut(requirement, ";")
	beforeMarker = strings.TrimSpace(beforeMarker)
	nameEnd := strings.IndexFunc(beforeMarker, func(r rune) bool {
		return r == ' ' || r == '(' || r == '<' || r == '>' || r == '=' || r == '!' || r == '~' || r == '['
	})
	if nameEnd < 0 {
		return beforeMarker, "", strings.TrimSpace(marker)
	}
	return strings.TrimSpace(beforeMarker[:nameEnd]), strings.TrimSpace(beforeMarker[nameEnd:]), strings.TrimSpace(marker)
}

func (artifact genericArtifact) hashes() map[string]string {
	hashes := cloneStringMap(artifact.Hashes)
	if hashes == nil {
		hashes = map[string]string{}
	}
	if strings.TrimSpace(artifact.SHA256) != "" {
		hashes["sha256"] = strings.TrimSpace(artifact.SHA256)
	}
	if strings.TrimSpace(artifact.SHA1) != "" {
		hashes["sha1"] = strings.TrimSpace(artifact.SHA1)
	}
	if strings.TrimSpace(artifact.MD5) != "" {
		hashes["md5"] = strings.TrimSpace(artifact.MD5)
	}
	if len(hashes) == 0 {
		return nil
	}
	return hashes
}

func sortedMapKeys[V any](input map[string]V) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func appendUniqueString(input []string, value string) []string {
	if value == "" {
		return input
	}
	for _, existing := range input {
		if existing == value {
			return input
		}
	}
	return append(input, value)
}

func artifactKey(rawURL string) string {
	sanitized := sanitizeURL(rawURL)
	parsed, err := url.Parse(sanitized)
	if err != nil || parsed.Path == "" {
		return strings.TrimSpace(sanitized)
	}
	return strings.TrimPrefix(path.Clean(parsed.Path), "/")
}

func stripGitURLDecorations(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	trimmed = strings.TrimPrefix(trimmed, "git+")
	return strings.TrimSuffix(trimmed, ".git")
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstParsedVersion(versions []PackageVersionObservation, fallback string) string {
	if len(versions) > 0 {
		return versions[0].Version
	}
	return strings.TrimSpace(fallback)
}

func parseTimestamp(raw string) time.Time {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func parseVisibility(raw string) Visibility {
	switch Visibility(strings.ToLower(strings.TrimSpace(raw))) {
	case VisibilityPublic:
		return VisibilityPublic
	case VisibilityPrivate:
		return VisibilityPrivate
	default:
		return VisibilityUnknown
	}
}
