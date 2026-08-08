// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

func discoverArgoCDDocumentEvidence(
	controlRepoID, filePath string,
	document map[string]any,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	contentIndex evidenceContentIndex,
) []EvidenceFact {
	var evidence []EvidenceFact

	// #5441 second-P0 fix: this document-level path (not
	// discoverStructuredArgoCDEvidence) is the one that actually fires for a
	// bare top-level ArgoCD Application YAML manifest, so it must carry
	// source_revision itself rather than relying on the structured path's
	// Details. Found via the live golden-corpus gate: rc-156 failed with
	// "2/2 matching edges offending" even after the reducer-side P0 fix
	// landed, because both corpus DEPLOYS_FROM edges came through here with
	// no source_revision key at all (extraDetails was a hard-coded nil).
	//
	// #5441 review round 8, P1-b: iterates argocdApplicationSources, not
	// argocdApplicationRepoURLs, and computes sourceRevisionDetails per
	// source inside the loop -- a multi-source Application can declare a
	// different targetRevision for each repository, and each resulting
	// DEPLOYS_FROM edge must carry its own source's revision, not the first
	// non-empty revision found anywhere in the document.
	for _, source := range argocdApplicationSources(document) {
		sourceRevisionDetails := argocdSourceRevisionDetails(source.targetRevision)
		for _, deployedRepo := range matchingCatalogEntries(source.repoURL, matcher) {
			evidence = append(evidence, matchCatalog(
				controlRepoID, source.repoURL, filePath,
				EvidenceKindArgoCDAppSource, RelDeploysFrom, DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindArgoCDAppSource),
				"ArgoCD Application source references the target repository",
				"argocd", matcher, seen, sourceRevisionDetails,
			)...)
			for _, destination := range argocdDocumentDestinations(document) {
				evidence = append(evidence, appendDestinationPlatformEvidence(
					deployedRepo.RepoID, filePath, destination, seen,
				)...)
			}
		}
	}

	discoveryTargets := argocdApplicationSetDiscoveryTargets(document)
	templateSources := argocdApplicationSetTemplateSources(document)
	templateSourceSpecs := argocdApplicationSetTemplateSourceSpecs(document)
	if len(discoveryTargets) == 0 {
		return evidence
	}

	for _, discovery := range discoveryTargets {
		for _, configRepo := range matchingCatalogEntries(discovery.repoURL, matcher) {
			if configRepo.RepoID == controlRepoID {
				continue
			}
			evidence = append(evidence, appendDiscoveryEvidence(
				controlRepoID, configRepo, filePath, discovery.path, seen,
			)...)
			for _, templateSource := range append(
				templateSources,
				append(
					argocdEvaluatedTemplateSources(templateSourceSpecs, discovery, configRepo.RepoID, contentIndex),
					argocdConfigIdentityDeploySources(discovery, configRepo.RepoID, contentIndex)...,
				)...,
			) {
				for _, deployedRepo := range matchingCatalogEntries(templateSource, matcher) {
					if deployedRepo.RepoID == configRepo.RepoID || deployedRepo.RepoID == controlRepoID {
						continue
					}
					evidence = append(evidence, appendDeploySourceEvidence(
						controlRepoID, deployedRepo, configRepo, filePath, discovery.path, templateSource, seen,
					)...)
					for _, destination := range argocdDocumentDestinations(document) {
						evidence = append(evidence, appendDestinationPlatformEvidence(
							deployedRepo.RepoID, filePath, destination, seen,
						)...)
					}
				}
			}
		}
	}

	return evidence
}

type argocdDiscoveryTarget struct {
	repoURL string
	path    string
}

type argocdTemplateSourceSpec struct {
	repoURL string
	path    string
	chart   string
}

type argocdDestination struct {
	name      string
	namespace string
	server    string
}

func appendDiscoveryEvidence(
	controlRepoID string,
	configRepo CatalogEntry,
	filePath, discoveryPath string,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	key := evidenceKey{
		EvidenceKind: EvidenceKindArgoCDApplicationSetDiscovery,
		SourceRepoID: controlRepoID,
		TargetRepoID: configRepo.RepoID,
		Path:         filePath,
	}
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}

	return []EvidenceFact{{
		EvidenceKind:     EvidenceKindArgoCDApplicationSetDiscovery,
		RelationshipType: RelDiscoversConfigIn,
		SourceRepoID:     controlRepoID,
		TargetRepoID:     configRepo.RepoID,
		Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindArgoCDApplicationSetDiscovery),
		Rationale:        "ArgoCD ApplicationSet discovers config in the target repository",
		Details: map[string]any{
			"path":           filePath,
			"discovery_path": discoveryPath,
			"matched_alias":  firstAlias(configRepo),
			"extractor":      "argocd",
		},
	}}
}

func appendDeploySourceEvidence(
	controlRepoID string,
	deployedRepo, configRepo CatalogEntry,
	filePath, discoveryPath, templateSource string,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	key := evidenceKey{
		EvidenceKind: EvidenceKindArgoCDApplicationSetDeploySource,
		SourceRepoID: deployedRepo.RepoID,
		TargetRepoID: configRepo.RepoID,
		Path:         filePath,
	}
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}

	return []EvidenceFact{{
		EvidenceKind:     EvidenceKindArgoCDApplicationSetDeploySource,
		RelationshipType: RelDeploysFrom,
		SourceRepoID:     deployedRepo.RepoID,
		TargetRepoID:     configRepo.RepoID,
		Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindArgoCDApplicationSetDeploySource),
		Rationale:        "The deployed repository sources manifests or overlays from the config repository",
		Details: map[string]any{
			"path":                  filePath,
			"control_plane_repo_id": controlRepoID,
			"config_repo_id":        configRepo.RepoID,
			"discovery_path":        discoveryPath,
			"deploy_repo_url":       templateSource,
			"extractor":             "argocd",
			"matched_alias":         firstAlias(deployedRepo),
		},
	}}
}

func appendDestinationPlatformEvidence(
	sourceRepoID, filePath string,
	destination argocdDestination,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	platformID := argocdDestinationPlatformID(destination)
	if platformID == "" {
		return nil
	}

	key := evidenceKey{
		EvidenceKind:   EvidenceKindArgoCDDestinationPlatform,
		SourceRepoID:   sourceRepoID,
		TargetEntityID: platformID,
		Path:           filePath,
	}
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}

	return []EvidenceFact{{
		EvidenceKind:     EvidenceKindArgoCDDestinationPlatform,
		RelationshipType: RelRunsOn,
		SourceRepoID:     sourceRepoID,
		TargetEntityID:   platformID,
		Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindArgoCDDestinationPlatform),
		Rationale:        "ArgoCD destination points at the runtime platform where the deployed repository runs",
		Details: map[string]any{
			"path":                  filePath,
			"destination_name":      destination.name,
			"destination_namespace": destination.namespace,
			"destination_server":    destination.server,
			"extractor":             "argocd",
		},
	}}
}

func parseYAMLDocuments(content string) []map[string]any {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var documents []map[string]any
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if err == nil {
			if len(document) > 0 {
				documents = append(documents, document)
			}
			continue
		}
		if err == io.EOF {
			return documents
		}
		return documents
	}
}

func matchingCatalogEntries(candidate string, matcher *catalogMatcher) []CatalogEntry {
	matches := matcher.match(candidate, "")
	result := make([]CatalogEntry, 0, len(matches))
	for _, match := range matches {
		result = append(result, match.entry)
	}
	return result
}

func firstAlias(entry CatalogEntry) string {
	if len(entry.Aliases) == 0 {
		return ""
	}
	return entry.Aliases[0]
}

func nestedMap(root map[string]any, key string) (map[string]any, bool) {
	if root == nil {
		return nil, false
	}
	value, ok := root[key]
	if !ok {
		return nil, false
	}
	result, ok := value.(map[string]any)
	return result, ok
}

func sliceValue(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	return items
}

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func normalizePlatformToken(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	replacer := strings.NewReplacer(" ", "-", "\t", "-", "\n", "-", "\r", "-")
	return replacer.Replace(raw)
}

func uniqueDestinations(values []argocdDestination) []argocdDestination {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[argocdDestination]struct{}, len(values))
	result := make([]argocdDestination, 0, len(values))
	for _, value := range values {
		if value.name == "" && value.server == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
