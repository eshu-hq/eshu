// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
)

type githubActionsRelationship struct {
	reason           string
	relationshipType string
	targetName       string
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
// githubActionsRepositoryRef).
//
// reusableWorkflowRefs and actionRefs (issue #5372) carry the RAW `uses:`
// scalar (quotes trimmed, ${{ }} expressions and non-ref shapes already
// excluded) for each entry in reusableWorkflowRepos/actionRepositories
// respectively, at the same index -- so refs.actionRefs[i] is the ref-bearing
// source of refs.actionRepositories[i]. This is deliberately NOT threaded
// into the slug these lists feed to edge targets (githubActionsRepositoryRef
// / githubActionsActionRepositoryRef keep their own local @-split, unchanged,
// because an edge target is a Repository node and correctly has no version --
// see githubActionsSourceRelationships's doc comment). The raw ref pairing
// exists only for callers that need the @ref itself: the repository
// workflow-artifact rollup's unpinned_action_refs signal
// (repository_workflow_artifacts.go) and the ghactionsref both-paths-agree
// regression test. Those callers split the ref value out with
// ghactionsref.Parse, the single implementation this package and
// go/internal/relationships both depend on.
type githubActionsDependencyRefs struct {
	reusableWorkflowRepos      []string
	reusableWorkflowRefs       []string
	localReusableWorkflowPaths []string
	checkoutRepositories       []string
	actionRepositories         []string
	actionRefs                 []string
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
			jobNames := make([]string, 0, len(jobs))
			for jobName := range jobs {
				jobNames = append(jobNames, jobName)
			}
			sort.Strings(jobNames)
			for _, jobName := range jobNames {
				rawJob := jobs[jobName]
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
			refs.reusableWorkflowRefs = append(refs.reusableWorkflowRefs, trimGitHubActionsScalar(uses))
		}
		if localWorkflowPath := githubActionsLocalReusableWorkflowPath(uses); localWorkflowPath != "" {
			refs.localReusableWorkflowPaths = append(refs.localReusableWorkflowPaths, localWorkflowPath)
		}
	}
	refs.workflowInputRepositories = append(
		refs.workflowInputRepositories,
		githubActionsWorkflowInputRepositories(job)...,
	)
	if with, ok := job["with"].(map[string]any); ok {
		refs.workflowInputRepositories = append(
			refs.workflowInputRepositories,
			githubActionsWorkflowInputRepositories(with)...,
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
				refs.actionRefs = append(refs.actionRefs, trimGitHubActionsScalar(uses))
			}
		}
		// Step-level with: may still carry an explicit automation/config
		// repository input even when the step's own uses: is an expression or
		// a local action.
		if with, ok := step["with"].(map[string]any); ok {
			refs.workflowInputRepositories = append(
				refs.workflowInputRepositories,
				githubActionsWorkflowInputRepositories(with)...,
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
// extractGitHubActionsDependencyRefs walk, preserving every relationship type
// and reason string the old scanner emitted so downstream consumers keyed on
// those reasons keep working. entity.SourceCache is populated in production
// (unlike entity.Metadata), so this is the real signal path.
//
// Every targetName below is a bare `owner/repo` slug with its @ref already
// stripped (githubActionsReusableWorkflowRepoRef / githubActionsRepositoryRef
// / githubActionsActionRepositoryRef do this locally, independent of
// ghactionsref). This is intentional and unchanged by issue #5372: an edge
// built here always points at a Repository node, and a Repository has no
// version -- only the artifact/evidence a ref appears in does. The @ref
// itself is not dropped, though: it is exposed as a normalized pin signal
// (ref_value + ref_pinned) on the deployment-evidence artifact surface
// instead (repository_deployment_evidence_read_model.go,
// go/internal/reducer/cross_repo_evidence_artifacts.go), and, for the
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

// githubActionsWorkflowInputRepositories extracts repository slugs from the
// known input-repository keys (workflow_input_repository,
// workflow_input_repositories, automation-repo, automation_repo) of a
// job/with/step map decoded from a workflow's SourceCache YAML. It is called
// from collectJob and collectSteps, not from entity.Metadata.
func githubActionsWorkflowInputRepositories(metadata map[string]any) []string {
	refs := make([]string, 0, 2)
	for _, key := range []string{"workflow_input_repository", "workflow_input_repositories", "automation-repo", "automation_repo"} {
		refs = append(refs, metadataStringSlice(metadata, key)...)
	}
	return refs
}

// githubActionsReusableWorkflowRepoRef delegates to
// ghactionsref.ReusableWorkflowRepo -- the single remote-reusable-workflow
// slug detector issue #5526 consolidates. Behavior-preserving: byte-identical
// to the implementation this function used to contain, modulo the
// trimGitHubActionsScalar quote-strip this package's callers need (a
// decoded-YAML `uses:` scalar can still carry literal quote characters in a
// few edge cases -- see trimGitHubActionsScalar's own callers).
func githubActionsReusableWorkflowRepoRef(value string) string {
	return ghactionsref.ReusableWorkflowRepo(trimGitHubActionsScalar(value))
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

// githubActionsActionRepositoryRef delegates to ghactionsref.ActionRepo for
// the shared docker://\actions/checkout@\reusable-workflow-shape guard and
// owner/repo extraction, then re-splits the result through
// ghactionsref.Parse to strip a trailing "@ref" -- unlike
// go/internal/relationships's sibling (githubActionsActionRepoRef), this
// package's callers want a clean, ref-free slug. Parse is idempotent on an
// already ref-free value (the subdirectory-action shape, where ActionRepo's
// own join never carries an "@ref" suffix in the first place) and strips one
// off cleanly when ActionRepo's plain two-segment "owner/repo@ref" shape
// still has it attached. Behavior-preserving: byte-identical to the
// implementation this function used to contain.
func githubActionsActionRepositoryRef(value string) string {
	repo, _, _ := ghactionsref.Parse(ghactionsref.ActionRepo(trimGitHubActionsScalar(value)))
	return repo
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
