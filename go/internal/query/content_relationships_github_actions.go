// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

type githubActionsRelationship struct {
	reason           string
	relationshipType string
	targetName       string
}

// isGitHubActionsArtifactPath reports whether entity is a GitHub Actions
// artifact file by its path alone. GitHub REQUIRES these exact locations, so
// this is an exact structural gate, not a content heuristic: workflow files
// live at `.github/workflows/*.yml`/`*.yaml`, and a composite or JavaScript
// action's metadata file must be named exactly `action.yml`/`action.yaml`.
//
// The gate exists because githubActionsSourceRelationships runs a structured
// YAML decode over every content entity's SourceCache; without it, an
// unrelated YAML file that merely happens to have a top-level `jobs:` map with
// `steps[].uses` (an internal CI config, a templated example, a GitLab CI
// file) would fabricate github_actions_* edges — and because
// buildOutgoingContentRelationships short-circuits on the first classifier
// that returns any edge, that false positive would also prevent later
// classifiers from handling the entity (issue #5337, codex P1 on PR #5379).
//
// The workflow-path branch delegates to ghactionsref.IsWorkflowPath, the
// single exact-path gate this package shares with
// go/internal/content/shape's isDirectGitHubActionsWorkflowPath (issue
// #5568's content-entity identity gate), so the two packages' workflow-path
// contracts cannot silently drift apart.
func isGitHubActionsArtifactPath(entity EntityContent) bool {
	path := strings.TrimSpace(entity.RelativePath)
	if ghactionsref.IsWorkflowPath(path) {
		return true
	}
	lowerPath := strings.ToLower(path)
	switch lowerPath[strings.LastIndex(lowerPath, "/")+1:] {
	case "action.yml", "action.yaml":
		return true
	default:
		return false
	}
}

// githubActionsSourceRelationships derives content-relationship edges from a
// structured YAML decode of entity.SourceCache. It replaces the former
// YAML-unaware raw-text line scanner (issue #5337 Detector 4) with the shared
// artifacts.ExtractGitHubActionsDependencyRefs walk, preserving every relationship type
// and reason string the old scanner emitted so downstream consumers keyed on
// those reasons keep working. entity.SourceCache is populated in production
// (unlike entity.Metadata), so this is the real signal path.
//
// Every targetName below is a bare `owner/repo` slug with its @ref already
// stripped (githubActionsReusableWorkflowRepoRef / artifacts.GithubActionsRepositoryRef
// / githubActionsActionRepositoryRef do this locally, independent of
// ghactionsref). This is intentional and unchanged by issue #5372: an edge
// built here always points at a Repository node, and a Repository has no
// version -- only the artifact/evidence a ref appears in does. The @ref
// itself is not dropped, though: it is exposed as a normalized pin signal
// (ref_value + ref_pinned) on the deployment-evidence artifact surface
// instead (repository_deployment_evidence_read_model.go,
// go/internal/reducer/crossrepo/cross_repo_evidence_artifacts.go), and, for the
// specific case of an unpinned third-party action, on the repository
// workflow-artifact rollup's unpinned_action_refs
// (repository_workflow_artifacts.go). Version truth lives on those artifact
// surfaces, never on this file's Repository-typed edge targets.
//
// It runs only for entities whose path is a GitHub Actions artifact
// (isGitHubActionsArtifactPath); any other content entity returns nil so a
// non-GitHub YAML with a jobs/steps/uses shape cannot fabricate github_actions_*
// edges.
func githubActionsSourceRelationships(entity EntityContent) []githubActionsRelationship {
	if !isGitHubActionsArtifactPath(entity) {
		return nil
	}
	refs := artifacts.ExtractGitHubActionsDependencyRefs(entity.SourceCache)
	if refs == nil {
		return nil
	}

	relationships := make([]githubActionsRelationship, 0, 4)
	for _, targetName := range refs.ReusableWorkflowRepos {
		if targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetName,
				reason:           "github_actions_reusable_workflow_ref",
			})
		}
	}
	for _, targetPath := range refs.LocalReusableWorkflowPaths {
		if targetPath != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetPath,
				reason:           "github_actions_local_reusable_workflow_ref",
			})
		}
	}
	for _, repoRef := range refs.CheckoutRepositories {
		if targetName := artifacts.GithubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_checkout_repository",
			})
		}
	}
	for _, repoRef := range refs.WorkflowInputRepositories {
		if targetName := artifacts.GithubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_workflow_input_repository",
			})
		}
	}
	for _, targetName := range refs.ActionRepositories {
		if targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPENDS_ON",
				targetName:       targetName,
				reason:           "github_actions_action_repository",
			})
		}
	}
	return relationships
}
