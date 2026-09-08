// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// GithubActionsDependencyRefs is the set of first-class GitHub Actions
// dependency reference lists a workflow or composite-action file declares. It
// is produced by a single structured YAML decode of the file content
// (ExtractGitHubActionsDependencyRefs) and consumed by both
// WorkflowArtifactDetails (the repository workflow-artifact rollup) and
// githubActionsSourceRelationships (the content-relationship edge builder), so
// the two paths agree on exactly which refs a file declares.
//
// The lists carry leaf-extractor output, not raw scalars: ReusableWorkflowRepos
// and ActionRepositories are already normalized `owner/repo` slugs,
// LocalReusableWorkflowPaths are `.github/workflows/*.yml` paths, and
// CheckoutRepositories/WorkflowInputRepositories hold the raw `with:`/job input
// values (the relationship builder normalizes those through
// GithubActionsRepositoryRef).
//
// ReusableWorkflowRefs and ActionRefs (issue #5372) carry the RAW `uses:`
// scalar (quotes trimmed, ${{ }} expressions and non-ref shapes already
// excluded) for each entry in ReusableWorkflowRepos/ActionRepositories
// respectively, at the same index -- so refs.ActionRefs[i] is the ref-bearing
// source of refs.ActionRepositories[i]. This is deliberately NOT threaded
// into the slug these lists feed to edge targets (GithubActionsRepositoryRef
// / GithubActionsActionRepositoryRef keep their own local @-split, unchanged,
// because an edge target is a Repository node and correctly has no version --
// see githubActionsSourceRelationships's doc comment). The raw ref pairing
// exists only for callers that need the @ref itself: the repository
// workflow-artifact rollup's unpinned_action_refs signal
// (repository_workflow_artifacts.go) and the ghactionsref both-paths-agree
// regression test. Those callers split the ref value out with
// ghactionsref.Parse, the single implementation this package and
// go/internal/relationships both depend on.
type GithubActionsDependencyRefs struct {
	ReusableWorkflowRepos      []string
	ReusableWorkflowRefs       []string
	LocalReusableWorkflowPaths []string
	CheckoutRepositories       []string
	ActionRepositories         []string
	ActionRefs                 []string
	WorkflowInputRepositories  []string
}

// ExtractGitHubActionsDependencyRefs decodes content as (multi-document) YAML
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
// content to documents — for example WorkflowArtifactDetails — should call
// ExtractGitHubActionsDependencyRefsFromDocuments directly to avoid a second
// YAML decode of the same content.
func ExtractGitHubActionsDependencyRefs(content string) *GithubActionsDependencyRefs {
	documents, err := decodeYAMLMaps(content)
	if err != nil {
		return nil
	}
	return ExtractGitHubActionsDependencyRefsFromDocuments(documents)
}

// ExtractGitHubActionsDependencyRefsFromDocuments walks already-decoded YAML
// documents and returns the GitHub Actions dependency references they declare.
// It is the shared implementation behind ExtractGitHubActionsDependencyRefs
// (the raw-string entry) so callers holding pre-decoded documents reuse the
// same walk without re-decoding the content.
func ExtractGitHubActionsDependencyRefsFromDocuments(documents []map[string]any) *GithubActionsDependencyRefs {
	refs := &GithubActionsDependencyRefs{}
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
func (refs *GithubActionsDependencyRefs) collectJob(job map[string]any) {
	if uses := querycontract.StringVal(job, "uses"); !githubActionsExpressionRef(uses) {
		if workflowRef := GithubActionsReusableWorkflowRepoRef(uses); workflowRef != "" {
			refs.ReusableWorkflowRepos = append(refs.ReusableWorkflowRepos, workflowRef)
			refs.ReusableWorkflowRefs = append(refs.ReusableWorkflowRefs, querycontract.TrimGitHubActionsScalar(uses))
		}
		if localWorkflowPath := GithubActionsLocalReusableWorkflowPath(uses); localWorkflowPath != "" {
			refs.LocalReusableWorkflowPaths = append(refs.LocalReusableWorkflowPaths, localWorkflowPath)
		}
	}
	refs.WorkflowInputRepositories = append(
		refs.WorkflowInputRepositories,
		githubActionsWorkflowInputRepositories(job)...,
	)
	if with, ok := job["with"].(map[string]any); ok {
		refs.WorkflowInputRepositories = append(
			refs.WorkflowInputRepositories,
			githubActionsWorkflowInputRepositories(with)...,
		)
	}
	refs.collectSteps(job["steps"])
}

// collectSteps records action, checkout, and step-level input-repository
// dependencies for a job's or composite action's steps.
func (refs *GithubActionsDependencyRefs) collectSteps(rawSteps any) {
	steps, ok := rawSteps.([]any)
	if !ok {
		return
	}
	for _, rawStep := range steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			continue
		}
		if uses := querycontract.StringVal(step, "uses"); !githubActionsExpressionRef(uses) {
			if strings.HasPrefix(strings.TrimSpace(uses), "actions/checkout@") {
				refs.CheckoutRepositories = append(refs.CheckoutRepositories, githubActionsCheckoutRepositories(step)...)
			}
			if actionRepository := GithubActionsActionRepositoryRef(uses); actionRepository != "" {
				refs.ActionRepositories = append(refs.ActionRepositories, actionRepository)
				refs.ActionRefs = append(refs.ActionRefs, querycontract.TrimGitHubActionsScalar(uses))
			}
		}
		// Step-level with: may still carry an explicit automation/config
		// repository input even when the step's own uses: is an expression or
		// a local action.
		if with, ok := step["with"].(map[string]any); ok {
			refs.WorkflowInputRepositories = append(
				refs.WorkflowInputRepositories,
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

// githubActionsWorkflowInputRepositories extracts repository slugs from the
// known input-repository keys (workflow_input_repository,
// workflow_input_repositories, automation-repo, automation_repo) of a
// job/with/step map decoded from a workflow's SourceCache YAML. It is called
// from collectJob and collectSteps, not from entity.Metadata.
func githubActionsWorkflowInputRepositories(metadata map[string]any) []string {
	refs := make([]string, 0, 2)
	for _, key := range []string{"workflow_input_repository", "workflow_input_repositories", "automation-repo", "automation_repo"} {
		refs = append(refs, querycontract.MetadataStringSlice(metadata, key)...)
	}
	return refs
}

// GithubActionsReusableWorkflowRepoRef delegates to
// ghactionsref.ReusableWorkflowRepo -- the single remote-reusable-workflow
// slug detector issue #5526 consolidates. Behavior-preserving: byte-identical
// to the implementation this function used to contain, modulo the
// trimGitHubActionsScalar quote-strip this package's callers need (a
// decoded-YAML `uses:` scalar can still carry literal quote characters in a
// few edge cases -- see trimGitHubActionsScalar's own callers).
func GithubActionsReusableWorkflowRepoRef(value string) string {
	return ghactionsref.ReusableWorkflowRepo(querycontract.TrimGitHubActionsScalar(value))
}

func GithubActionsRepositoryRef(value string) string {
	trimmed := strings.TrimSpace(querycontract.TrimGitHubActionsScalar(value))
	if trimmed == "" {
		return ""
	}
	if repoRef := GithubActionsReusableWorkflowRepoRef(trimmed); repoRef != "" {
		return repoRef
	}
	if isGitHubRepoSlug(trimmed) {
		return trimmed
	}
	return ""
}

// GithubActionsActionRepositoryRef delegates to ghactionsref.ActionRepo for
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
func GithubActionsActionRepositoryRef(value string) string {
	repo, _, _ := ghactionsref.Parse(ghactionsref.ActionRepo(querycontract.TrimGitHubActionsScalar(value)))
	return repo
}

func isGitHubRepoSlug(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}
