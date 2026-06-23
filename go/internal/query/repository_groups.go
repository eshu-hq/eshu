package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	repositoryGroupSourceDependencyCluster = "dependency_cluster"
	repositoryGroupSourceDependencyFlag    = "repository_dependency_flag"
	repositoryGroupSourceSlugNamespace     = "repo_slug_namespace"
	repositoryGroupSourceRemoteOwner       = "remote_url_owner"
	repositoryGroupSourceMissing           = "missing_evidence"
	repositoryGroupTruthDerived            = "derived"
	repositoryGroupTruthMissing            = "missing_evidence"
	repositoryGroupMissingReason           = "repository_group_evidence_missing"
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
	if owner := repositoryRemoteOwner(StringVal(repo, "remote_url")); owner != "" {
		return repositoryGroupEvidence{
			Key:    titleRepositoryGroup(owner),
			Source: repositoryGroupSourceRemoteOwner,
			Truth:  repositoryGroupTruthDerived,
			Kind:   "source",
			Reason: "derived from the org/owner segment of the git remote URL",
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

// repositoryRemoteOwner returns the org/owner namespace derived from a git remote
// URL. It normalizes SSH and HTTPS remotes through the canonical repository
// identity parser, then takes the first path segment (the org or owner). A remote
// with no owner segment (single-component path, or unparsable) returns "" so the
// caller falls through to the honest missing-evidence bucket.
func repositoryRemoteOwner(remoteURL string) string {
	slug := repositoryidentity.RepoSlugFromRemoteURL(remoteURL)
	parts := strings.Split(slug, "/")
	if len(parts) < 2 {
		return ""
	}
	return repositorySlugNamespace(slug)
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
