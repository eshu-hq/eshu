// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

// discoverKustomizeDocumentEvidence extracts DEPLOYS_FROM evidence from one
// parsed Kustomize YAML document for resources, helmCharts, and images fields.
// commitSHA is forwarded from the fact envelope's commit_sha payload field and
// stored in Details so Canonical() can project a typed version pin. An empty
// string degrades safely — no fabricated citation is emitted.
func discoverKustomizeDocumentEvidence(
	sourceRepoID, filePath string,
	document map[string]any,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	commitSHA string,
) []EvidenceFact {
	extra := mergeCommitSHA(nil, commitSHA)

	var evidence []EvidenceFact

	appendValues := func(values []string, kind EvidenceKind, confidence float64, rationale string) {
		for _, value := range values {
			evidence = append(evidence, matchCatalog(
				sourceRepoID, value, filePath,
				kind, RelDeploysFrom, confidence, rationale,
				"kustomize", matcher, seen, extra,
			)...)
		}
	}

	appendValues(kustomizeResourceStrings(document), EvidenceKindKustomizeResource,
		DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindKustomizeResource),
		"Kustomize resources source deployment config from the target repository")
	appendValues(kustomizeHelmStrings(document), EvidenceKindKustomizeHelmChart,
		DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindKustomizeHelmChart),
		"Kustomize Helm configuration deploys from the target repository")
	appendValues(kustomizeImageStrings(document), EvidenceKindKustomizeImage,
		DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindKustomizeImage),
		"Kustomize image configuration deploys artifacts from the target repository")

	return evidence
}

// kustomizeResourceStrings gathers `bases` as well as `resources` and
// `components`. The legacy `bases:` key was not walked at all, so a target
// written there — remote or not — produced no evidence on this path, while the
// same target under `resources:` did.
//
// This keeps the raw read and the structured read (discoverStructuredKustomizeEvidence,
// which reads the parser's bases and resource_refs together) producing the same
// set. They both run: the collector emits a content fact and a file fact for one
// file, and only the file fact carries parsed_file_data, so the content fact
// still takes this path. Two extractions of one file that disagree do not
// conflict — they union, and the graph shows the wider answer with nothing
// reporting why (#5609).
func kustomizeResourceStrings(document map[string]any) []string {
	return gatherStrings(document, "resources", "components", "bases")
}

func kustomizeHelmStrings(document map[string]any) []string {
	return gatherObjectStrings(document, "helmCharts", "name", "repo", "releaseName")
}

func kustomizeImageStrings(document map[string]any) []string {
	return gatherObjectStrings(document, "images", "name", "newName")
}

func gatherStrings(document map[string]any, keys ...string) []string {
	var result []string
	for _, key := range keys {
		for _, item := range sliceValue(document[key]) {
			if value := stringValue(item); value != "" {
				result = append(result, value)
			}
		}
	}
	return uniqueStrings(result)
}

func gatherObjectStrings(document map[string]any, listKey string, fieldKeys ...string) []string {
	var result []string
	for _, item := range sliceValue(document[listKey]) {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range fieldKeys {
			if value := stringValue(entry[key]); value != "" {
				result = append(result, value)
			}
		}
	}
	return uniqueStrings(result)
}
