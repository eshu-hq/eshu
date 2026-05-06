package relationships

import (
	"io"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func discoverAnsibleEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	if !isAnsibleRelationshipEvidenceSource(filePath, content) {
		return nil
	}
	var evidence []EvidenceFact
	for _, document := range parseAnsibleDocuments(content) {
		evidence = append(evidence, discoverAnsibleDocumentEvidence(
			sourceRepoID, filePath, document, catalog, seen,
		)...)
	}
	return evidence
}

func discoverAnsibleDocumentEvidence(
	sourceRepoID, filePath string,
	document any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, candidate := range ansibleRoleCandidates(document) {
		refKind, refName, normalized := normalizeAnsibleReference(candidate)
		evidence = append(evidence, matchCatalog(
			sourceRepoID,
			candidate.value,
			filePath,
			EvidenceKindAnsibleRoleReference,
			RelDependsOn,
			0.92,
			"Ansible playbook role reference points at the target repository",
			"ansible",
			catalog,
			seen,
			withFirstPartyRefDetails(
				map[string]any{
					"role_name":      candidate.roleName,
					"source_ref":     candidate.value,
					"reference_key":  candidate.key,
					"reference_name": refName,
				},
				refKind,
				refName,
				candidate.value,
				"",
				"",
				normalized,
			),
		)...)
	}
	return evidence
}

type ansibleRoleCandidate struct {
	key      string
	roleName string
	value    string
}

func ansibleRoleCandidates(document any) []ansibleRoleCandidate {
	var candidates []ansibleRoleCandidate
	switch typed := document.(type) {
	case map[string]any:
		candidates = append(candidates, ansibleRoleCandidatesFromMap(typed)...)
	case []any:
		for _, item := range typed {
			play, ok := item.(map[string]any)
			if !ok {
				continue
			}
			candidates = append(candidates, ansibleRoleCandidatesFromMap(play)...)
		}
	}
	return uniqueAnsibleCandidates(candidates)
}

func ansibleRoleCandidatesFromMap(document map[string]any) []ansibleRoleCandidate {
	var candidates []ansibleRoleCandidate
	for _, item := range sliceValue(document["roles"]) {
		switch typed := item.(type) {
		case string:
			roleName := strings.TrimSpace(typed)
			if roleName == "" {
				continue
			}
			candidates = append(candidates, ansibleRoleCandidate{
				key:      "roles",
				roleName: roleName,
				value:    roleName,
			})
		case map[string]any:
			roleName := strings.TrimSpace(stringValue(typed["role"]))
			if roleName == "" {
				roleName = strings.TrimSpace(stringValue(typed["name"]))
			}
			if roleName == "" {
				continue
			}
			candidateValue := strings.TrimSpace(stringValue(typed["src"]))
			if candidateValue == "" {
				candidateValue = roleName
			}
			candidates = append(candidates, ansibleRoleCandidate{
				key:      "roles",
				roleName: roleName,
				value:    candidateValue,
			})
		}
	}

	if candidate := strings.TrimSpace(stringValue(document["import_playbook"])); candidate != "" {
		candidates = append(candidates, ansibleRoleCandidate{
			key:   "import_playbook",
			value: candidate,
		})
	}

	for _, item := range sliceValue(document["import_playbook"]) {
		if candidate := strings.TrimSpace(stringValue(item)); candidate != "" {
			candidates = append(candidates, ansibleRoleCandidate{
				key:   "import_playbook",
				value: candidate,
			})
		}
	}

	return candidates
}

func uniqueAnsibleCandidates(candidates []ansibleRoleCandidate) []ansibleRoleCandidate {
	seen := make(map[string]struct{}, len(candidates))
	result := make([]ansibleRoleCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := candidate.key + "|" + candidate.roleName + "|" + candidate.value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func isAnsibleRelationshipEvidenceSource(filePath, content string) bool {
	lower := strings.ToLower(filepath.ToSlash(filePath))
	if lower == "" {
		return false
	}

	switch {
	case strings.Contains(lower, "/inventories/"),
		strings.Contains(lower, "/inventory/"),
		strings.Contains(lower, "inventories/"),
		strings.Contains(lower, "inventory/"):
		return false
	case strings.Contains(lower, "/group_vars/"),
		strings.Contains(lower, "/host_vars/"),
		strings.Contains(lower, "group_vars/"),
		strings.Contains(lower, "host_vars/"):
		return false
	case strings.Contains(lower, "/roles/"), strings.HasPrefix(lower, "roles/"):
		return false
	case strings.Contains(lower, "/playbooks/"), strings.Contains(lower, "playbooks/"):
		return true
	}

	lowerBase := strings.ToLower(filepath.Base(lower))
	if lowerBase == "site.yml" || lowerBase == "site.yaml" {
		return true
	}

	return ansiblePlaybookLikeContent(content)
}

func isAnsibleArtifact(artifactType, filePath string) bool {
	if strings.HasPrefix(artifactType, "ansible_") {
		return true
	}

	lower := strings.ToLower(filepath.ToSlash(filePath))
	for _, part := range []string{"playbooks/", "/playbooks/", "roles/", "/roles/", "group_vars/", "/group_vars/", "host_vars/", "/host_vars/", "inventories/", "/inventories/", "inventory/", "/inventory/"} {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

func parseAnsibleDocuments(content string) []any {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	documents := make([]any, 0)
	for {
		var document any
		err := decoder.Decode(&document)
		if err == io.EOF {
			return documents
		}
		if err != nil {
			return documents
		}
		if document == nil {
			continue
		}
		documents = append(documents, document)
	}
}

func ansiblePlaybookLikeContent(content string) bool {
	lowered := strings.ToLower(content)
	return strings.Contains(lowered, "hosts:") ||
		strings.Contains(lowered, "roles:") ||
		strings.Contains(lowered, "tasks:") ||
		strings.Contains(lowered, "import_playbook:")
}
