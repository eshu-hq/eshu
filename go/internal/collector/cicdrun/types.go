// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import "time"

// CollectorKind is the durable collector family name for CI/CD run facts.
const CollectorKind = "ci_cd_run"

// Provider identifies the CI/CD provider that reported runtime evidence.
type Provider string

const (
	// ProviderGitHubActions identifies GitHub Actions workflow-run evidence.
	ProviderGitHubActions Provider = "github_actions"
	// ProviderGitLabCI identifies GitLab CI/CD pipeline-run evidence. GitLab
	// facts share the same ci.* fact kinds and reducer join key shape
	// (provider, run_id, run_attempt) as GitHub Actions -- see
	// gitlab_ci_fixture.go.
	ProviderGitLabCI Provider = "gitlab_ci"
)

// FixtureContext carries the collector boundary fields copied into fixture
// normalized facts.
type FixtureContext struct {
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

type githubActionsFixture struct {
	Workflow         githubWorkflow   `json:"workflow"`
	Run              githubRun        `json:"run"`
	Jobs             []githubJob      `json:"jobs"`
	JobsPartial      bool             `json:"jobs_partial"`
	Artifacts        []githubArtifact `json:"artifacts"`
	ArtifactsPartial bool             `json:"artifacts_partial"`
	Triggers         []githubTrigger  `json:"triggers"`
	Warnings         []githubWarning  `json:"warnings"`
}

type githubWorkflow struct {
	ID      any    `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	State   string `json:"state"`
	Trigger string `json:"trigger"`
}

type githubRun struct {
	ID           any              `json:"id"`
	RunAttempt   any              `json:"run_attempt"`
	RunNumber    any              `json:"run_number"`
	Name         string           `json:"name"`
	Event        string           `json:"event"`
	Status       string           `json:"status"`
	Conclusion   string           `json:"conclusion"`
	HeadBranch   string           `json:"head_branch"`
	HeadSHA      string           `json:"head_sha"`
	RunStartedAt string           `json:"run_started_at"`
	UpdatedAt    string           `json:"updated_at"`
	HTMLURL      string           `json:"html_url"`
	Repository   githubRepository `json:"repository"`
	Actor        githubActor      `json:"actor"`
}

type githubRepository struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

type githubActor struct {
	Login string `json:"login"`
}

type githubJob struct {
	ID               any          `json:"id"`
	Name             string       `json:"name"`
	Status           string       `json:"status"`
	Conclusion       string       `json:"conclusion"`
	StartedAt        string       `json:"started_at"`
	CompletedAt      string       `json:"completed_at"`
	Labels           []string     `json:"labels"`
	Environment      string       `json:"environment"`
	DeploymentStatus string       `json:"deployment_status"`
	Steps            []githubStep `json:"steps"`
}

type githubStep struct {
	Name        string `json:"name"`
	Number      any    `json:"number"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

type githubArtifact struct {
	ID                 any                  `json:"id"`
	Name               string               `json:"name"`
	SizeBytes          int64                `json:"size_in_bytes"`
	Digest             string               `json:"digest"`
	ArtifactType       string               `json:"artifact_type"`
	ArchiveDownloadURL string               `json:"archive_download_url"`
	Expired            bool                 `json:"expired"`
	CreatedAt          string               `json:"created_at"`
	ExpiresAt          string               `json:"expires_at"`
	WorkflowRun        githubArtifactRunRef `json:"workflow_run"`
}

type githubArtifactRunRef struct {
	ID      any    `json:"id"`
	HeadSHA string `json:"head_sha"`
}

type githubTrigger struct {
	TriggerKind    string `json:"trigger_kind"`
	SourceRunID    any    `json:"source_run_id"`
	SourceProvider string `json:"source_provider"`
}

type githubWarning struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// gitlabCIFixture is the fixture-shaped decode target for one GitLab CI/CD
// pipeline: the GitLab Pipelines API
// (https://docs.gitlab.com/ee/api/pipelines.html) plus the Jobs API
// (https://docs.gitlab.com/ee/api/jobs.html#list-pipeline-jobs), joined by
// the collector the same way ghactionsruntime joins a run to its jobs.
type gitlabCIFixture struct {
	Pipeline    gitlabPipeline `json:"pipeline"`
	Jobs        []gitlabJob    `json:"jobs"`
	JobsPartial bool           `json:"jobs_partial"`
}

type gitlabPipeline struct {
	ID         any        `json:"id"`
	IID        any        `json:"iid"`
	ProjectID  any        `json:"project_id"`
	Ref        string     `json:"ref"`
	SHA        string     `json:"sha"`
	Status     string     `json:"status"`
	Source     string     `json:"source"`
	CreatedAt  string     `json:"created_at"`
	UpdatedAt  string     `json:"updated_at"`
	StartedAt  string     `json:"started_at"`
	FinishedAt string     `json:"finished_at"`
	WebURL     string     `json:"web_url"`
	User       gitlabUser `json:"user"`
}

type gitlabUser struct {
	Username string `json:"username"`
}

type gitlabJob struct {
	ID         any              `json:"id"`
	Name       string           `json:"name"`
	Stage      string           `json:"stage"`
	Status     string           `json:"status"`
	CreatedAt  string           `json:"created_at"`
	StartedAt  string           `json:"started_at"`
	FinishedAt string           `json:"finished_at"`
	WebURL     string           `json:"web_url"`
	Artifacts  []gitlabArtifact `json:"artifacts"`
}

// gitlabArtifact is one entry of a GitLab job's "artifacts" array. GitLab's
// Jobs API reports artifact metadata inline with each job and never includes
// a content digest at this level (unlike GitHub Actions' separate Artifacts
// API, which does) -- gitlabArtifactEnvelope always emits a blank
// artifact_digest and the caller always follows with an
// "artifact_missing_digest" warning, matching real GitLab API shape rather
// than a fixture gap.
type gitlabArtifact struct {
	FileType   string `json:"file_type"`
	Size       int64  `json:"size"`
	Filename   string `json:"filename"`
	FileFormat string `json:"file_format"`
}
