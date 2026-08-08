// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// discoverStructuredKustomizeEvidence extracts DEPLOYS_FROM evidence from the
// parser's kustomize_overlays bucket, the same bucket that materializes the
// KustomizeOverlay node.
//
// This replaces a second, independent parse of the same file. Kustomize was
// the one source #5445 slice 1 could not type, because it had no
// parsed_file_data read site to convert — evidence discovery re-parsed the raw
// YAML and read `resources`, `helmCharts`, and `images` off the document, while
// the parser separately produced a richer structured bucket. Two extractions of
// one file can disagree, and they did (#5609).
//
// The measured difference across every kustomization file in the repo was nine
// values, and eight of them were same-repo directory paths (`../base` and kin)
// that the raw path handed to the catalog matcher. Those cannot be a deployment
// source: a path inside this repository is not another repository. They only
// ever produced an edge by coincidence, when a repository's name matched a
// sibling directory's.
//
// The ninth was the opposite problem and had to be fixed in the parser before
// this read was safe: a scheme-less remote base
// (github.com/acme/deployable-source//k8s?ref=v1.4.0) was classified as a local
// base, so the bucket dropped a real cross-repo signal that the raw path caught.
// See isRemoteKustomizeRef in go/internal/parser/yaml/kustomize_semantics.go.
//
// commitSHA is forwarded from the fact envelope's commit_sha payload field and
// stored in Details so Canonical() can project a typed version pin. An empty
// string degrades safely — no fabricated citation is emitted.
func discoverStructuredKustomizeEvidence(
	sourceRepoID, filePath, commitSHA string,
	parsedFileData map[string]any,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	overlays, err := factschema.DecodeParsedFileDataKustomizeOverlays(parsedFileData)
	if err != nil || len(overlays) == 0 {
		return nil
	}

	extra := mergeCommitSHA(nil, commitSHA)
	var evidence []EvidenceFact

	appendValues := func(values []string, kind EvidenceKind, rationale string) {
		for _, value := range values {
			evidence = append(evidence, matchCatalog(
				sourceRepoID, value, filePath,
				kind, RelDeploysFrom, DefaultConfidenceRegistry.ConfidenceFor(kind), rationale,
				"kustomize", matcher, seen, extra,
			)...)
		}
	}

	for _, overlay := range overlays {
		appendValues(overlay.ResourceRefs, EvidenceKindKustomizeResource,
			"Kustomize resources source deployment config from the target repository")
		appendValues(overlay.HelmRefs, EvidenceKindKustomizeHelmChart,
			"Kustomize Helm configuration deploys from the target repository")
		appendValues(overlay.ImageRefs, EvidenceKindKustomizeImage,
			"Kustomize image configuration deploys artifacts from the target repository")
	}

	return evidence
}
