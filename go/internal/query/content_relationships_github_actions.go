// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

type githubActionsRelationship struct {
	reason           string
	relationshipType string
	targetName       string
}

func githubActionsMetadataRelationships(metadata map[string]any) []githubActionsRelationship {
	relationships := make([]githubActionsRelationship, 0, 4)
	for _, workflowRef := range metadataStringSlice(metadata, "workflow_refs") {
		if targetName := githubActionsRepositoryRef(workflowRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetName,
				reason:           "github_actions_reusable_workflow_ref",
			})
			continue
		}
		if targetPath := githubActionsLocalReusableWorkflowPath(workflowRef); targetPath != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetPath,
				reason:           "github_actions_local_reusable_workflow_ref",
			})
		}
	}
	for _, workflowRef := range metadataStringSlice(metadata, "workflow_ref") {
		if targetName := githubActionsRepositoryRef(workflowRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetName,
				reason:           "github_actions_reusable_workflow_ref",
			})
			continue
		}
		if targetPath := githubActionsLocalReusableWorkflowPath(workflowRef); targetPath != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetPath,
				reason:           "github_actions_local_reusable_workflow_ref",
			})
		}
	}
	for _, repoRef := range metadataStringSlice(metadata, "checkout_repositories") {
		if targetName := githubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_checkout_repository",
			})
		}
	}
	for _, repoRef := range metadataStringSlice(metadata, "checkout_repository") {
		if targetName := githubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_checkout_repository",
			})
		}
	}
	for _, repoRef := range githubActionsWorkflowInputRepositoryMetadata(metadata) {
		if targetName := githubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_workflow_input_repository",
			})
		}
	}
	for _, repoRef := range metadataStringSlice(metadata, "action_repositories") {
		if targetName := githubActionsActionRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPENDS_ON",
				targetName:       targetName,
				reason:           "github_actions_action_repository",
			})
		}
	}
	return relationships
}

// githubActionsDependencyRefs is the set of first-class GitHub Actions
// dependency reference lists a workflow or composite-action file declares. It
// is produced by a single structured YAML decode of the file content
// (extractGitHubActionsDependencyRefs) and consumed by both
// workflowArtifactDetails (the repository workflow-artifact rollup) and
// githubActionsSourceRelationships (the content-relationship edge builder), so
// the two paths agree on exactly which refs a file declares.
//
// The lists carry leaf-extractor output, not raw scalars: reusableWorkflowRepos
// and actionRepositories are already normalized `owner/repo` slugs,
// localReusableWorkflowPaths are `.github/workflows/*.yml` paths, and
// checkoutRepositories/workflowInputRepositories hold the raw `with:`/job input
// values (the relationship builder normalizes those through
// githubActionsRepositoryRef, matching the metadata path).
type githubActionsDependencyRefs struct {
	reusableWorkflowRepos      []string
	localReusableWorkflowPaths []string
	checkoutRepositories       []string
	actionRepositories         []string
	workflowInputRepositories  []string
}

// extractGitHubActionsDependencyRefs decodes content as (multi-document) YAML
// and returns the dependency references it structurally declares. It is the
// single structured replacement for the former raw-text `uses:` line scanner
// (issue #5337 Detector 4): because it walks the decoded document tree, a
// `uses:` line that lives inside a `run: |` block scalar is just part of a
// string and never mistaken for a real step key.
//
// Both GitHub Actions workflow files (top-level `jobs:`) and composite action
// files (top-level `runs.steps:` with no `jobs:`) are walked, so composite
// action step dependencies are not silently dropped. Job-level `uses:`
// (reusable workflows), step-level `uses:` (actions and checkout), and both
// job-level and step-level `with:` input-repository keys are all covered.
// Candidate values containing a `${{ ... }}` expression are skipped before ref
// parsing, since they are not resolvable to a concrete repository here.
//
// On malformed YAML the function returns nil: an unparseable workflow cannot
// run, so it declares no real dependency, and unrelated content (for example
// prose or docs that merely mention `actions/checkout@`) that does not decode
// to the expected structure now correctly yields nothing. There is
// deliberately no fallback to line scanning.
//
// This is the raw-string entry point, used by githubActionsSourceRelationships
// (which only has entity.SourceCache). Callers that have already decoded the
// content to documents — for example workflowArtifactDetails — should call
// extractGitHubActionsDependencyRefsFromDocuments directly to avoid a second
// YAML decode of the same content.
func extractGitHubActionsDependencyRefs(content string) *githubActionsDependencyRefs {
	documents, err := decodeYAMLMaps(content)
	if err != nil {
		return nil
	}
	return extractGitHubActionsDependencyRefsFromDocuments(documents)
}

// extractGitHubActionsDependencyRefsFromDocuments walks already-decoded YAML
// documents and returns the GitHub Actions dependency references they declare.
// It is the shared implementation behind extractGitHubActionsDependencyRefs
// (the raw-string entry) so callers holding pre-decoded documents reuse the
// same walk without re-decoding the content.
func extractGitHubActionsDependencyRefsFromDocuments(documents []map[string]any) *githubActionsDependencyRefs {
	refs := &githubActionsDependencyRefs{}
	for _, document := range documents {
		if jobs, ok := document["jobs"].(map[string]any); ok {
			for _, rawJob := range jobs {
				job, ok := rawJob.(map[string]any)
				if !ok {
					continue
				}
				refs.collectJob(job)
			}
			continue
		}
		// Composite action files model their steps under runs.steps and carry
		// no jobs key; walk them so composite step dependencies survive.
		if runs, ok := document["runs"].(map[string]any); ok {
			refs.collectSteps(runs["steps"])
		}
	}
	return refs
}

// collectJob records a workflow job's reusable-workflow ref, its input
// repositories (job-level and via job with:), and its steps' dependencies.
func (refs *githubActionsDependencyRefs) collectJob(job map[string]any) {
	if uses := StringVal(job, "uses"); !githubActionsExpressionRef(uses) {
		if workflowRef := githubActionsReusableWorkflowRepoRef(uses); workflowRef != "" {
			refs.reusableWorkflowRepos = append(refs.reusableWorkflowRepos, workflowRef)
		}
		if localWorkflowPath := githubActionsLocalReusableWorkflowPath(uses); localWorkflowPath != "" {
			refs.localReusableWorkflowPaths = append(refs.localReusableWorkflowPaths, localWorkflowPath)
		}
	}
	refs.workflowInputRepositories = append(
		refs.workflowInputRepositories,
		githubActionsWorkflowInputRepositoryMetadata(job)...,
	)
	if with, ok := job["with"].(map[string]any); ok {
		refs.workflowInputRepositories = append(
			refs.workflowInputRepositories,
			githubActionsWorkflowInputRepositoryMetadata(with)...,
		)
	}
	refs.collectSteps(job["steps"])
}

// collectSteps records action, checkout, and step-level input-repository
// dependencies for a job's or composite action's steps.
func (refs *githubActionsDependencyRefs) collectSteps(rawSteps any) {
	steps, ok := rawSteps.([]any)
	if !ok {
		return
	}
	for _, rawStep := range steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			continue
		}
		if uses := StringVal(step, "uses"); !githubActionsExpressionRef(uses) {
			if strings.HasPrefix(strings.TrimSpace(uses), "actions/checkout@") {
				refs.checkoutRepositories = append(refs.checkoutRepositories, githubActionsCheckoutRepositories(step)...)
			}
			if actionRepository := githubActionsActionRepositoryRef(uses); actionRepository != "" {
				refs.actionRepositories = append(refs.actionRepositories, actionRepository)
			}
		}
		// Step-level with: may still carry an explicit automation/config
		// repository input even when the step's own uses: is an expression or
		// a local action.
		if with, ok := step["with"].(map[string]any); ok {
			refs.workflowInputRepositories = append(
				refs.workflowInputRepositories,
				githubActionsWorkflowInputRepositoryMetadata(with)...,
			)
		}
	}
}

// githubActionsExpressionRef reports whether a candidate ref value is a GitHub
// Actions `${{ ... }}` expression, which cannot be resolved to a concrete
// repository at parse time and must be skipped before ref extraction.
func githubActionsExpressionRef(value string) bool {
	return strings.Contains(value, "${{")
}

// githubActionsSourceRelationships derives content-relationship edges from a
// structured YAML decode of entity.SourceCache. It replaces the former
// YAML-unaware raw-text line scanner (issue #5337 Detector 4) with the shared
// extractGitHubActionsDependencyRefs walk, preserving every relationship type
// and reason string the old scanner emitted so downstream consumers keyed on
// those reasons keep working. entity.SourceCache is populated in production
// (unlike entity.Metadata), so this is the real signal path.
func githubActionsSourceRelationships(entity EntityContent) []githubActionsRelationship {
	refs := extractGitHubActionsDependencyRefs(entity.SourceCache)
	if refs == nil {
		return nil
	}

	relationships := make([]githubActionsRelationship, 0, 4)
	for _, targetName := range refs.reusableWorkflowRepos {
		if targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetName,
				reason:           "github_actions_reusable_workflow_ref",
			})
		}
	}
	for _, targetPath := range refs.localReusableWorkflowPaths {
		if targetPath != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DEPLOYS_FROM",
				targetName:       targetPath,
				reason:           "github_actions_local_reusable_workflow_ref",
			})
		}
	}
	for _, repoRef := range refs.checkoutRepositories {
		if targetName := githubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_checkout_repository",
			})
		}
	}
	for _, repoRef := range refs.workflowInputRepositories {
		if targetName := githubActionsRepositoryRef(repoRef); targetName != "" {
			relationships = append(relationships, githubActionsRelationship{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       targetName,
				reason:           "github_actions_workflow_input_repository",
			})
		}
	}
	for _, targetName := range refs.actionRepositories {
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

func githubActionsWorkflowInputRepositoryMetadata(metadata map[string]any) []string {
	refs := make([]string, 0, 2)
	for _, key := range []string{"workflow_input_repository", "workflow_input_repositories", "automation-repo", "automation_repo"} {
		refs = append(refs, metadataStringSlice(metadata, key)...)
	}
	return refs
}

func githubActionsReusableWorkflowRepoRef(value string) string {
	trimmed := strings.TrimSpace(trimGitHubActionsScalar(value))
	if trimmed == "" {
		return ""
	}
	at := strings.Index(trimmed, "@")
	if at >= 0 {
		trimmed = trimmed[:at]
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return ""
	}
	if parts[0] == "." {
		return ""
	}
	if parts[2] != ".github" {
		return ""
	}
	return strings.Join(parts[:2], "/")
}

func githubActionsRepositoryRef(value string) string {
	trimmed := strings.TrimSpace(trimGitHubActionsScalar(value))
	if trimmed == "" {
		return ""
	}
	if repoRef := githubActionsReusableWorkflowRepoRef(trimmed); repoRef != "" {
		return repoRef
	}
	if isGitHubRepoSlug(trimmed) {
		return trimmed
	}
	return ""
}

func githubActionsActionRepositoryRef(value string) string {
	trimmed := strings.TrimSpace(trimGitHubActionsScalar(value))
	if trimmed == "" || strings.HasPrefix(trimmed, "docker://") {
		return ""
	}
	if strings.HasPrefix(trimmed, "actions/checkout@") {
		return ""
	}
	if repoRef := githubActionsReusableWorkflowRepoRef(trimmed); repoRef != "" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	if strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, ".github/") {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] == "." {
		return ""
	}
	return strings.Join(parts[:2], "/")
}

func isGitHubRepoSlug(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}

func trimGitHubActionsScalar(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 {
		return trimmed
	}
	if (strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
		(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
		return trimmed[1 : len(trimmed)-1]
	}
	return trimmed
}
