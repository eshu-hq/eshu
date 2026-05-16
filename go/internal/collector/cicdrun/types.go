package cicdrun

import "time"

// CollectorKind is the durable collector family name for CI/CD run facts.
const CollectorKind = "ci_cd_run"

// Provider identifies the CI/CD provider that reported runtime evidence.
type Provider string

const (
	// ProviderGitHubActions identifies GitHub Actions workflow-run evidence.
	ProviderGitHubActions Provider = "github_actions"
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
	Workflow    githubWorkflow   `json:"workflow"`
	Run         githubRun        `json:"run"`
	Jobs        []githubJob      `json:"jobs"`
	JobsPartial bool             `json:"jobs_partial"`
	Artifacts   []githubArtifact `json:"artifacts"`
	Triggers    []githubTrigger  `json:"triggers"`
	Warnings    []githubWarning  `json:"warnings"`
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
