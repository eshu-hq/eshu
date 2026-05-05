package relationships

import "strings"

func discoverDockerfileEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, reference := range dockerfileSourceLabelReferences(parsedFileData) {
		evidence = append(evidence, matchCatalog(
			sourceRepoID, reference.value, filePath,
			EvidenceKindDockerfileSourceLabel, RelDeploysFrom, 0.93,
			"Dockerfile source label points at the target repository",
			"dockerfile", catalog, seen, map[string]any{
				"source_label": reference.label,
				"source_ref":   reference.value,
			},
		)...)
	}
	return evidence
}

type dockerfileSourceReference struct {
	label string
	value string
}

func dockerfileSourceLabelReferences(parsedFileData map[string]any) []dockerfileSourceReference {
	if len(parsedFileData) == 0 {
		return nil
	}

	references := make([]dockerfileSourceReference, 0)
	for _, item := range payloadAnyMapSlice(parsedFileData["dockerfile_labels"]) {
		labelName := strings.ToLower(strings.TrimSpace(stringValue(item["name"])))
		labelValue := strings.TrimSpace(stringValue(item["value"]))
		if labelValue == "" || !isDockerfileSourceLabel(labelName) || !looksLikeExplicitRepoRef(labelValue) {
			continue
		}
		references = append(references, dockerfileSourceReference{
			label: labelName,
			value: labelValue,
		})
	}
	return uniqueDockerfileSourceReferences(references)
}

func uniqueDockerfileSourceReferences(references []dockerfileSourceReference) []dockerfileSourceReference {
	seen := make(map[string]struct{}, len(references))
	result := make([]dockerfileSourceReference, 0, len(references))
	for _, reference := range references {
		key := reference.label + "|" + reference.value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, reference)
	}
	return result
}

func isDockerfileSourceLabel(label string) bool {
	switch label {
	case "org.opencontainers.image.source", "org.label-schema.vcs-url":
		return true
	default:
		return false
	}
}

func looksLikeExplicitRepoRef(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "github.com") || strings.HasPrefix(lower, "git@github.com:") {
		return true
	}
	parts := strings.Split(lower, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
