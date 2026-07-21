// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

// argocdApplicationSource pairs one ArgoCD Application source's repository
// URL with its own declared targetRevision (#5441 review round 8, P1-b).
type argocdApplicationSource struct {
	repoURL        string
	targetRevision string
}

// argocdApplicationSources walks an ArgoCD Application document's
// spec.source (singular) and spec.sources[] (plural, multi-source
// Applications) and returns each declared source's own repoURL/targetRevision
// pair, in document order. A source with no repoURL is skipped. A source
// with no targetRevision is kept (its repoURL still resolves a DEPLOYS_FROM
// edge) with an empty targetRevision -- never borrowed from a sibling
// source.
//
// This is deliberately per-source, unlike argocdApplicationRepoURLs
// (yaml_iac_evidence.go), which flattens and dedupes every source down to a
// bare []string and is still the right shape for callers that only need
// distinct URLs (ApplicationSet template-source discovery). Discovering
// which repository an Application deploys from and which revision that
// deployment declares are two different questions, and the second one is
// answered per source, not once for the whole document.
func argocdApplicationSources(document map[string]any) []argocdApplicationSource {
	spec, _ := nestedMap(document, "spec")
	if spec == nil {
		return nil
	}
	var result []argocdApplicationSource
	if source, _ := nestedMap(spec, "source"); source != nil {
		if repoURL := stringValue(source["repoURL"]); repoURL != "" {
			result = append(result, argocdApplicationSource{
				repoURL:        repoURL,
				targetRevision: stringValue(source["targetRevision"]),
			})
		}
	}
	for _, item := range sliceValue(spec["sources"]) {
		source, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if repoURL := stringValue(source["repoURL"]); repoURL != "" {
			result = append(result, argocdApplicationSource{
				repoURL:        repoURL,
				targetRevision: stringValue(source["targetRevision"]),
			})
		}
	}
	return result
}

// argocdSourceRevisionDetails wraps one source's own targetRevision as the
// EvidenceFact.Details map matchCatalog's extraDetails parameter expects.
//
// This is the #5441 second-P0 fix (discoverArgoCDDocumentEvidence,
// yaml_iac_evidence.go, is the evidence path that actually fires for a bare
// top-level ArgoCD Application YAML manifest, not discoverStructuredArgoCDEvidence
// — see the caller for the full mechanism), now paired per source (#5441
// review round 8, P1-b) instead of computing one document-wide revision and
// stamping it onto every resulting DEPLOYS_FROM edge: a multi-source
// Application declaring different revisions for different repositories
// would otherwise fabricate the first repo's revision onto every other
// repo's edge.
//
// Returns nil when the source declares no targetRevision, so the resulting
// evidence fact's Details carries no source_revision key at all rather than
// a fabricated empty one — matchCatalog already handles a nil extraDetails
// safely (ranging over a nil map merges nothing).
func argocdSourceRevisionDetails(targetRevision string) map[string]any {
	if targetRevision == "" {
		return nil
	}
	return map[string]any{"source_revision": targetRevision}
}
