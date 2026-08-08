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
// the parser separately produced a richer structured bucket (#5609).
//
// It reads Bases and ResourceRefs TOGETHER, and the union is the whole point.
// An earlier version of this function read only ResourceRefs, on the reasoning
// that a same-repo path cannot be a deployment source. That reasoning is wrong
// here, and Eshu's own confidence calibration corpus is where it shows:
// `resources: [../payments-service/base]` is a golden positive at 0.90 — a
// sibling directory naming another repository's config — while `./base` is a
// golden negative at 0.792. Both are meant to reach the catalog matcher, and
// the calibration layer is what tells them apart. Reading ResourceRefs alone
// would have dropped the positive along with the negative.
//
// Measured against the raw path it replaces, the union carries every value the
// raw read produced plus the entries under the legacy `bases:` key, which
// `gatherStrings(document, "resources", "components")` never walked at all.
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
		resources := make([]string, 0, len(overlay.ResourceRefs)+len(overlay.Bases))
		resources = append(resources, overlay.ResourceRefs...)
		resources = append(resources, overlay.Bases...)
		appendValues(resources, EvidenceKindKustomizeResource,
			"Kustomize resources source deployment config from the target repository")
		appendValues(overlay.HelmRefs, EvidenceKindKustomizeHelmChart,
			"Kustomize Helm configuration deploys from the target repository")
		appendValues(overlay.ImageRefs, EvidenceKindKustomizeImage,
			"Kustomize image configuration deploys artifacts from the target repository")
	}

	return evidence
}
