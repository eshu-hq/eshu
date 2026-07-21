// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

// argocdApplicationSourceRevisionDetails reads the declared targetRevision
// off an ArgoCD Application document's spec.source (or the first
// spec.sources[] entry carrying one) and returns it as the
// EvidenceFact.Details map matchCatalog's extraDetails parameter expects.
//
// This is the #5441 second-P0 fix: discoverArgoCDDocumentEvidence
// (yaml_iac_evidence.go), not discoverStructuredArgoCDEvidence
// (structured_family_evidence.go), is the path that actually fires for a
// bare top-level ArgoCD Application YAML manifest — the structured path
// requires a parser to have already populated
// parsedFileData["argocd_applications"], which a plain Application YAML file
// does not trigger in this corpus. Before this fix,
// discoverArgoCDDocumentEvidence's matchCatalog call passed a hard-coded nil
// for extraDetails, so every EvidenceKindArgoCDAppSource fact produced by
// this dominant path carried no source_revision key at all. The reducer-side
// #5441 fix (evidenceFactSourceRevision reading
// EvidenceFact.Details["source_revision"] in aggregateCandidate,
// evidence_edge_fields.go) was correct but had no data to read; the live
// golden-corpus gate caught the gap (rc-156_edge_prop_source_revision failed
// "2/2 matching edges offending" even after the reducer fix landed).
//
// Only the first source carrying a non-empty targetRevision is used, even
// for a multi-source Application with several spec.sources[] entries with
// different revisions — a real ArgoCD Application overwhelmingly declares one
// meaningful revision-bearing primary source plus zero or more value-file-only
// sources, so attaching the primary revision to every resulting edge is the
// correct common-case behavior; per-source revision pairing (matching each
// resulting DEPLOYS_FROM edge to its own source's revision) would require
// restructuring argocdApplicationRepoURLs itself and is out of scope for this
// fix.
//
// Returns nil when no source declares a targetRevision, so the resulting
// evidence fact's Details carries no source_revision key at all rather than
// a fabricated empty one — matchCatalog already handles a nil extraDetails
// safely (ranging over a nil map merges nothing).
func argocdApplicationSourceRevisionDetails(document map[string]any) map[string]any {
	spec, _ := nestedMap(document, "spec")
	if spec == nil {
		return nil
	}
	if source, _ := nestedMap(spec, "source"); source != nil {
		if revision := stringValue(source["targetRevision"]); revision != "" {
			return map[string]any{"source_revision": revision}
		}
	}
	for _, item := range sliceValue(spec["sources"]) {
		source, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if revision := stringValue(source["targetRevision"]); revision != "" {
			return map[string]any{"source_revision": revision}
		}
	}
	return nil
}
