// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// CatalogEntry maps one repository to its known aliases for matching.
type CatalogEntry struct {
	RepoID  string
	Aliases []string
	// RemoteURL is the repositoryidentity.NormalizeRemoteURL-normalized git
	// remote for this repository (issue #5483 C2). It is used ONLY by
	// discoverStructuredFluxEvidence's STRICT equality resolution -- never
	// folded into Aliases, because Aliases feeds the fuzzy token matcher
	// (catalog_matcher.go) and the #3521 alias-drift comparison, and a raw
	// URL would perturb both. Empty when the repository fact carried no
	// remote_url.
	RemoteURL string
}

// DiscoverEvidence scans fact envelopes for IaC relationship evidence
// (Terraform, Helm, ArgoCD, Kustomize, Flux) and returns discovered evidence
// facts. It discards the DiscoveryStats tallies DiscoverEvidenceWithStats
// returns; callers that need those tallies (for example, to emit the Flux
// cross-repo URL resolution telemetry) must call DiscoverEvidenceWithStats
// directly.
func DiscoverEvidence(envelopes []facts.Envelope, catalog []CatalogEntry) []EvidenceFact {
	evidence, _ := DiscoverEvidenceWithStats(envelopes, catalog)
	return evidence
}

// DiscoveryStats aggregates DiscoverEvidenceWithStats outcomes that are not
// representable as an EvidenceFact row -- today, only the Flux cross-repo URL
// resolution tally (issue #5483 C2). It is additive so future extractors can
// grow their own tally without widening DiscoverEvidence's signature for
// every caller.
type DiscoveryStats struct {
	FluxCrossRepoURLResolution FluxCrossRepoURLResolutionStats
}

// DiscoverEvidenceWithStats is DiscoverEvidence plus the DiscoveryStats tally
// for outcomes an extractor intentionally does not turn into an EvidenceFact
// (an unresolved, ambiguous, or same-repo Flux GitRepository url). This is a
// new seam rather than a widened DiscoverEvidence signature (issue #5483 C2
// design note): every existing DiscoverEvidence caller stays untouched, and
// only the Postgres ingestion commit path -- the sole caller that needs the
// tally to emit eshu_dp_flux_cross_repo_url_resolution_total -- calls this
// function directly.
func DiscoverEvidenceWithStats(envelopes []facts.Envelope, catalog []CatalogEntry) ([]EvidenceFact, DiscoveryStats) {
	if len(envelopes) == 0 {
		return nil, DiscoveryStats{}
	}

	var evidence []EvidenceFact
	var stats DiscoveryStats
	seen := make(map[evidenceKey]struct{})
	contentIndex := buildEvidenceContentIndex(envelopes)
	matcher := newCatalogMatcher(catalog)

	for i := range envelopes {
		discovered := discoverFromEnvelopeWithIndex(envelopes[i], matcher, seen, contentIndex, &stats)
		evidence = append(evidence, discovered...)
	}

	return evidence, stats
}

// evidenceKey deduplicates evidence within a single discovery pass.
type evidenceKey struct {
	EvidenceKind   EvidenceKind
	SourceRepoID   string
	TargetRepoID   string
	SourceEntityID string
	TargetEntityID string
	Path           string
	MatchedValue   string
}

// helmChartFilenames are the recognized Helm chart metadata files.
var helmChartFilenames = map[string]struct{}{
	"chart.yaml": {},
	"chart.yml":  {},
}

// kustomizationFilenames are the recognized Kustomize files.
var kustomizationFilenames = map[string]struct{}{
	"kustomization.yaml": {},
	"kustomization.yml":  {},
}

func discoverFromEnvelopeWithIndex(
	envelope facts.Envelope,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	contentIndex evidenceContentIndex,
	stats *DiscoveryStats,
) []EvidenceFact {
	if envelope.FactKind == facts.GCPCloudRelationshipFactKind {
		return discoverGCPCloudRelationshipEvidence(envelope, matcher, seen)
	}

	// TODO(#4783 W1): fact kind "content" has no typed struct yet (producer
	// go/internal/collector/git_content_fact_envelopes.go emits artifact_type,
	// content_path/content_body, etc.); route through the decode seam once the
	// content family lands in sdk/go/factschema.
	artifactType, _ := envelope.Payload["artifact_type"].(string)
	// This raw read of parsed_file_data itself is DELIBERATE, not a pending
	// migration (issue #5445 slice 1 review): routing it through
	// factschema.DecodeCodegraphFile would newly impose the "file" fact
	// kind's required-field dead-lettering (repo_id, relative_path,
	// parsed_file_data) on a pipeline that today tolerates a missing or
	// malformed parsed_file_data by producing no structured evidence -- a
	// much bigger behavior change than typing the INNER keys warrants. The
	// inner keys this map feeds ARE typed: discoverStructuredTerraformEvidence,
	// discoverStructuredHelmEvidence, discoverStructuredArgoCDEvidence, and
	// discoverStructuredFluxEvidence below each decode their own bucket
	// through a factschema.DecodeParsedFileData* accessor.
	parsedFileData, _ := envelope.Payload["parsed_file_data"].(map[string]any)
	sourceRepoID, filePath, content := envelopeContentIdentity(envelope)
	commitSHA := envelopeCommitSHA(envelope.Payload)

	if filePath == "" {
		return nil
	}
	if content == "" && len(parsedFileData) == 0 {
		return nil
	}

	var evidence []EvidenceFact

	if len(parsedFileData) > 0 {
		evidence = append(evidence, discoverStructuredTerraformEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
		evidence = append(evidence, discoverStructuredTerragruntConfigEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
		evidence = append(evidence, discoverStructuredHelmEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
		evidence = append(evidence, discoverStructuredArgoCDEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
		evidence = append(evidence, discoverStructuredFluxEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen, stats,
		)...)
	}

	switch {
	case isAnsibleArtifact(artifactType, filePath):
		evidence = append(evidence, discoverAnsibleEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case isTerraformArtifact(artifactType, filePath):
		evidence = append(evidence, discoverTerraformEvidence(
			sourceRepoID, filePath, content, commitSHA, matcher, seen,
		)...)
	case isHelmArtifact(artifactType, filePath):
		evidence = append(evidence, discoverHelmEvidence(
			sourceRepoID, filePath, content, commitSHA, matcher, seen,
		)...)
	case isKustomizeArtifact(filePath):
		evidence = append(evidence, discoverKustomizeEvidence(
			sourceRepoID, filePath, content, commitSHA, matcher, seen,
		)...)
	case isArgoCDArtifact(artifactType, content):
		evidence = append(evidence, discoverArgoCDEvidence(
			sourceRepoID, filePath, content, matcher, seen, contentIndex,
		)...)
	case isJenkinsArtifact(filePath):
		evidence = append(evidence, discoverJenkinsEvidence(
			sourceRepoID, filePath, content, commitSHA, parsedFileData, matcher, seen,
		)...)
	case isPuppetArtifact(filePath):
		evidence = append(evidence, discoverPuppetEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case isChefArtifact(filePath):
		evidence = append(evidence, discoverChefEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case isDockerfileArtifact(artifactType, filePath):
		evidence = append(evidence, discoverDockerfileEvidence(
			sourceRepoID, filePath, commitSHA, parsedFileData, matcher, seen,
		)...)
	case artifactType == "docker_compose":
		evidence = append(evidence, discoverDockerComposeEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case artifactType == "github_actions_workflow":
		evidence = append(evidence, discoverGitHubActionsEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case isSaltGitfsArtifact(content):
		// Content-based fallback: runs only after every artifact-type-specific
		// case above, so a known YAML artifact (Compose, GitHub Actions, …) is
		// never preempted by a file that merely contains a top-level
		// gitfs_remotes key. A genuine Salt config matches no other case.
		evidence = append(evidence, discoverSaltEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	}

	return evidence
}

func sourceRepositoryIDFromEnvelope(envelope facts.Envelope) string {
	// TODO(#4783 W1): this reads repo_id raw because it is fact-kind-agnostic —
	// it serves the typed "file" kind (codegraphv1.File) AND the untyped
	// "content" kind, which has no typed struct yet (producer
	// go/internal/collector/git_content_fact_envelopes.go). Route each caller
	// through its kind's decode seam once the content family lands.
	if repoID, _ := envelope.Payload["repo_id"].(string); strings.TrimSpace(repoID) != "" {
		return strings.TrimSpace(repoID)
	}
	return normalizeRepositoryIdentifier(envelope.ScopeID)
}

func normalizeRepositoryIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, "repository:"); idx > 0 {
		prefix := value[:idx]
		if strings.HasSuffix(prefix, "scope:") {
			return value[idx:]
		}
	}
	return value
}

// discoverHelmEvidence extracts DEPLOYS_FROM evidence from Helm chart content.
// commitSHA is forwarded from the fact envelope's commit_sha payload field and
// stored in Details so Canonical() can project a typed byte-level citation.
func discoverHelmEvidence(
	sourceRepoID, filePath, content, commitSHA string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	lowerName := strings.ToLower(fileBaseName(filePath))
	var evidenceKind EvidenceKind
	var rationale string

	if _, ok := helmChartFilenames[lowerName]; ok {
		evidenceKind = EvidenceKindHelmChart
		rationale = "Helm chart metadata references the target repository"
	} else {
		evidenceKind = EvidenceKindHelmValues
		rationale = "Helm values reference the target repository"
	}
	confidence := DefaultConfidenceRegistry.ConfidenceFor(evidenceKind)

	// The YAML string scanner does not track byte positions; we pass only
	// the commit_sha so Canonical() at least gets a version pin. Line/byte
	// offsets remain zero (safe degradation) for this code path.
	extra := mergeCommitSHA(nil, commitSHA)

	var evidence []EvidenceFact
	for _, candidate := range extractYAMLStringValues(content) {
		evidence = append(evidence, matchCatalog(
			sourceRepoID, candidate, filePath,
			evidenceKind, RelDeploysFrom, confidence, rationale,
			"helm", matcher, seen, extra,
		)...)
	}

	return evidence
}

// discoverKustomizeEvidence extracts DEPLOYS_FROM evidence from Kustomize overlays.
// commitSHA is forwarded from the fact envelope's commit_sha payload field and
// stored in Details so Canonical() can project a typed version pin.
func discoverKustomizeEvidence(
	sourceRepoID, filePath, content, commitSHA string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	extra := mergeCommitSHA(nil, commitSHA)

	var evidence []EvidenceFact
	for _, document := range parseYAMLDocuments(content) {
		evidence = append(evidence, discoverKustomizeDocumentEvidence(
			sourceRepoID, filePath, document, matcher, seen, commitSHA,
		)...)
	}
	for _, candidate := range extractYAMLStringValues(content) {
		evidence = append(evidence, matchCatalog(
			sourceRepoID, candidate, filePath,
			EvidenceKindKustomizeResource, RelDeploysFrom, DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindKustomizeResource),
			"Kustomize resources source deployment config from the target repository",
			"kustomize", matcher, seen, extra,
		)...)
	}

	return evidence
}

// discoverArgoCDEvidence extracts ArgoCD Application source references.
func discoverArgoCDEvidence(
	sourceRepoID, filePath, content string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	contentIndex evidenceContentIndex,
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, document := range parseYAMLDocuments(content) {
		evidence = append(evidence, discoverArgoCDDocumentEvidence(
			sourceRepoID, filePath, document, matcher, seen, contentIndex,
		)...)
	}

	return evidence
}

// extractYAMLStringValues extracts potential string values from YAML content
// using simple pattern matching (not a full YAML parser).
var yamlStringPattern = regexp.MustCompile(`:\s*['"]?([A-Za-z0-9._/-]+)['"]?`)

func extractYAMLStringValues(content string) []string {
	matches := yamlStringPattern.FindAllStringSubmatch(content, -1)
	values := make([]string, 0, len(matches))
	seen := make(map[string]struct{})
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		val := strings.TrimSpace(match[1])
		if val == "" {
			continue
		}
		if _, ok := seen[val]; ok {
			continue
		}
		seen[val] = struct{}{}
		values = append(values, val)
	}
	return values
}

// isHelmArtifact checks if a file is a Helm chart file.
func isHelmArtifact(artifactType, filePath string) bool {
	if artifactType == "helm" {
		return true
	}
	lowerName := strings.ToLower(fileBaseName(filePath))
	if _, ok := helmChartFilenames[lowerName]; ok {
		return true
	}
	return strings.HasPrefix(lowerName, "values") &&
		(strings.HasSuffix(lowerName, ".yaml") || strings.HasSuffix(lowerName, ".yml"))
}

// isKustomizeArtifact checks if a file is a Kustomize file.
func isKustomizeArtifact(filePath string) bool {
	lowerName := strings.ToLower(fileBaseName(filePath))
	_, ok := kustomizationFilenames[lowerName]
	return ok
}

// isArgoCDArtifact checks if content appears to be an ArgoCD Application spec.
func isArgoCDArtifact(artifactType, content string) bool {
	if artifactType == "argocd" {
		return true
	}
	return strings.Contains(content, "kind: Application") ||
		strings.Contains(content, "kind: ApplicationSet")
}

func isDockerfileArtifact(artifactType, filePath string) bool {
	if strings.EqualFold(artifactType, "dockerfile") {
		return true
	}
	lowerName := strings.ToLower(fileBaseName(filePath))
	return lowerName == "dockerfile" || strings.HasPrefix(lowerName, "dockerfile.")
}

func isJenkinsArtifact(filePath string) bool {
	base := strings.ToLower(fileBaseName(filePath))
	return base == "jenkinsfile" || strings.HasPrefix(base, "jenkinsfile.")
}

// fileBaseName returns the last path component of a file path.
func fileBaseName(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
