// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// ArgoCDGeneratorConfigRef names a config repository an ArgoCD ApplicationSet's
// git file generator targets. discoverArgoCDDocumentEvidence reads the matching
// generator-path files from that repository (via the content index, keyed by the
// config repo's RepoID) to synthesize template deploy sources, so a per-commit
// backfill that loads only the ApplicationSet's own facts would miss the external
// config files and under-select the synthesized deploy edges. Resolving these
// refs lets the backfill load the external config files in a second phase.
type ArgoCDGeneratorConfigRef struct {
	// ConfigRepoID is the repository whose generator-path files the ApplicationSet
	// reads. It is always a different repository from the control-plane repo that
	// holds the ApplicationSet (discoverArgoCDDocumentEvidence skips
	// configRepo == controlRepo).
	ConfigRepoID string
}

// ResolveArgoCDGeneratorConfigRepos returns the external config repositories that
// the ArgoCD ApplicationSets in envelopes target with a git file generator,
// resolved against the same catalog and the same generator-repoURL matching rules
// discoverArgoCDDocumentEvidence uses. It covers both the content-derived
// ApplicationSets (parsed from the YAML body) and the structured ApplicationSets
// (parsed_file_data.argocd_applicationsets[].generator_source_repos).
//
// The control-plane repo that holds the ApplicationSet is excluded, matching the
// configRepo == controlRepo skip in discoverArgoCDDocumentEvidence; its own files
// are already loaded with the ApplicationSet. Template-string generator repoURLs
// are skipped because they cannot resolve to a concrete repository. Results are
// de-duplicated and stable-sorted by ConfigRepoID.
//
// This is the resolution half of the two-phase per-commit backfill fact load
// (issue #3570): phase one loads the ApplicationSet facts via the ArgoCD marker
// anchors, and phase two loads these config repos' generator-path files so the
// content index DiscoverEvidence builds is complete and no synthesized deploy edge
// is dropped.
func ResolveArgoCDGeneratorConfigRepos(
	envelopes []facts.Envelope,
	catalog []CatalogEntry,
) []ArgoCDGeneratorConfigRef {
	if len(envelopes) == 0 || len(catalog) == 0 {
		return nil
	}

	matcher := newCatalogMatcher(catalog)
	seen := make(map[string]struct{})
	refs := make([]ArgoCDGeneratorConfigRef, 0)

	add := func(controlRepoID, generatorRepoURL string) {
		generatorRepoURL = strings.TrimSpace(generatorRepoURL)
		if generatorRepoURL == "" || isArgoTemplateString(generatorRepoURL) {
			return
		}
		for _, configRepo := range matchingCatalogEntries(generatorRepoURL, matcher) {
			if configRepo.RepoID == controlRepoID {
				continue
			}
			if _, ok := seen[configRepo.RepoID]; ok {
				continue
			}
			seen[configRepo.RepoID] = struct{}{}
			refs = append(refs, ArgoCDGeneratorConfigRef{ConfigRepoID: configRepo.RepoID})
		}
	}

	for i := range envelopes {
		envelope := envelopes[i]
		controlRepoID := sourceRepositoryIDFromEnvelope(envelope)

		// envelope.Payload["parsed_file_data"] is read raw here deliberately
		// (not through factschema.DecodeCodegraphFile): this is a
		// fact-kind-agnostic helper called across every envelope regardless of
		// fact kind, mirroring the same raw extraction evidence.go uses for the
		// identical reason (see that file's discoverFromEnvelopeWithIndex
		// comment). The argocd_applicationsets inner key itself IS typed (issue
		// #5445 slice 1) -- see structuredApplicationSetGeneratorRepos below.
		if parsedFileData, ok := envelope.Payload["parsed_file_data"].(map[string]any); ok {
			for _, generatorRepoURL := range structuredApplicationSetGeneratorRepos(parsedFileData) {
				add(controlRepoID, generatorRepoURL)
			}
		}

		_, _, content := envelopeContentIdentity(envelope)
		if content == "" {
			continue
		}
		for _, document := range parseYAMLDocuments(content) {
			for _, target := range argocdApplicationSetDiscoveryTargets(document) {
				add(controlRepoID, target.repoURL)
			}
		}
	}

	if len(refs) == 0 {
		return nil
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ConfigRepoID < refs[j].ConfigRepoID })
	return refs
}

// structuredApplicationSetGeneratorRepos extracts the git-generator config
// repository URLs from a parsed_file_data payload's argocd_applicationsets, the
// same generator_source_repos field discoverStructuredArgoCDEvidence reads.
// It reads the bucket through the typed
// factschema.DecodeParsedFileDataArgoCDApplicationSets accessor (issue #5445
// slice 1) rather than a raw map lookup; the accessor skips a malformed row
// rather than failing the whole bucket, so the ignored error return matches
// the pre-typing raw-map read's silent tolerance of an absent/wrong-shape
// bucket.
func structuredApplicationSetGeneratorRepos(parsedFileData map[string]any) []string {
	appSets, _ := factschema.DecodeParsedFileDataArgoCDApplicationSets(parsedFileData)
	var repos []string
	for _, appSet := range appSets {
		repos = append(repos, csvValues(appSet.GeneratorSourceRepos)...)
	}
	return repos
}
