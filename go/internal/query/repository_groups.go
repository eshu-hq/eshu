package query

import "strings"

const (
	repositoryGroupSourceDependencyFlag = "repository_dependency_flag"
	repositoryGroupSourceSlugNamespace  = "repo_slug_namespace"
	repositoryGroupSourceMissing        = "missing_evidence"
	repositoryGroupTruthDerived         = "derived"
	repositoryGroupTruthMissing         = "missing_evidence"
	repositoryGroupMissingReason        = "repository_group_evidence_missing"
)

type repositoryGroupEvidence struct {
	Key    string
	Source string
	Truth  string
	Kind   string
	Reason string
}

func decorateRepositoryGroupEvidence(repo map[string]any) map[string]any {
	evidence := deriveRepositoryGroupEvidence(repo)
	repo["group_key"] = evidence.Key
	repo["group_source"] = evidence.Source
	repo["group_truth"] = evidence.Truth
	repo["group_kind"] = evidence.Kind
	repo["group_reason"] = evidence.Reason
	return repo
}

func deriveRepositoryGroupEvidence(repo map[string]any) repositoryGroupEvidence {
	if BoolVal(repo, "is_dependency") {
		return repositoryGroupEvidence{
			Key:    "Dependencies",
			Source: repositoryGroupSourceDependencyFlag,
			Truth:  repositoryGroupTruthDerived,
			Kind:   "dependency",
			Reason: "repository is marked as a dependency in the repository catalog",
		}
	}
	if namespace := repositorySlugNamespace(StringVal(repo, "repo_slug")); namespace != "" {
		return repositoryGroupEvidence{
			Key:    titleRepositoryGroup(namespace),
			Source: repositoryGroupSourceSlugNamespace,
			Truth:  repositoryGroupTruthDerived,
			Kind:   "source",
			Reason: "derived from the first source-backed repository slug namespace",
		}
	}
	return repositoryGroupEvidence{
		Source: repositoryGroupSourceMissing,
		Truth:  repositoryGroupTruthMissing,
		Kind:   "unknown",
		Reason: "no source-backed repository group evidence is available",
	}
}

func repositorySlugNamespace(slug string) string {
	for _, part := range strings.Split(slug, "/") {
		part = strings.TrimSpace(part)
		if part != "" {
			return part
		}
	}
	return ""
}

func titleRepositoryGroup(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		parts[i] = strings.ToUpper(string(runes[:1])) + strings.ToLower(string(runes[1:]))
	}
	return strings.Join(parts, " ")
}

func repositoryGroupEvidenceMissing(repos []map[string]any) bool {
	for _, repo := range repos {
		if StringVal(repo, "group_source") == repositoryGroupSourceMissing {
			return true
		}
	}
	return false
}
