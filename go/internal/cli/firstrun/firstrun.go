// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// QueryEndpoint is the bounded, API-backed query the command runs to
// prove a useful answer is reachable. Listing repositories with a small limit
// is the smallest truthful end-to-end probe.
const QueryEndpoint = "/api/v0/repositories?limit=5"

// Repository is the slice of one repository-list entry the first-run needs:
// enough identity to summarize the answer and enough path context for the
// caller-supplied selector matcher to recognize the workspace target. The
// cobra wrapper in go/cmd/eshu decodes the API response and copies it here, so
// this package never owns the wire contract.
type Repository struct {
	ID        string
	Name      string
	Path      string
	LocalPath string
	RepoSlug  string
}

// RepositoryList carries the repositories the bounded query returned.
type RepositoryList struct {
	Repositories []Repository
}

// Deps groups the injectable seams used by the orchestration so each
// step is unit-testable with fakes. Production wiring lives in the cobra
// wrapper in go/cmd/eshu, which owns process contact: cobra flags, the API
// client, PATH lookup, and the config/env store.
type Deps struct {
	Probe       RuntimeProbe
	FetchStatus func(client scan.Client) (scan.PipelineStatus, error)
	ListRepos   func(client scan.Client) (RepositoryList, error)
	RunScan     func(ctx context.Context, stdout, stderr io.Writer, rt scan.Runtime, opts scan.Options, announce bool) (scan.Result, error)
	ReposDir    func(root string) (string, error)
	// ScanRuntime is the process-owned scan runtime handed to RunScan. The
	// wrapper builds it from the resolved API client because the runtime wires
	// the bootstrap child process and the inherited environment, which stay in
	// package main by the extraction shape rule.
	ScanRuntime scan.Runtime
	// MatchesSelector reports whether a repository entry matches a resolved
	// local target. Production wiring uses go/cmd/eshu's repository selector
	// matcher, which stays there because thirteen other command families share
	// it. It must be non-nil whenever ListRepos can return entries; the
	// production wrapper always sets it.
	MatchesSelector func(repo Repository, selector string) bool
	// ResolveMCPEndpoint returns the configured MCP endpoint, if any, for the
	// API-vs-MCP misconfiguration heuristic. The wrapper wires it to the
	// env/config store; a nil seam reads as "no endpoint configured".
	ResolveMCPEndpoint func() string
	WorkspaceRoot      string
	WorkspaceError     error
}

// resolveMCPEndpoint reads the configured MCP endpoint through the seam,
// treating a nil seam as "no endpoint configured" so the heuristic is skipped.
func (d Deps) resolveMCPEndpoint() string {
	if d.ResolveMCPEndpoint == nil {
		return ""
	}
	return d.ResolveMCPEndpoint()
}

// Options captures the resolved command flags.
type Options struct {
	Path         string
	JSON         bool
	NoStart      bool
	Timeout      time.Duration
	PollInterval time.Duration
	Profile      string
	// Report enables the terminal evidence summary in addition to the normal
	// human or JSON output.
	Report bool
	// ReportFormat selects the artifact format ("md" or "json") for ReportOut.
	ReportFormat string
	// ReportOut is the optional path the redacted evidence artifact is written
	// to. An empty value writes no artifact.
	ReportOut string
}

// Execute runs the ordered, individually-testable steps and returns the
// canonical result. It never reports success unless the final bounded query
// actually returned an answer. serviceURL is the resolved API base URL the
// run targets; client is the narrow API read surface the seams consume.
func Execute(
	parentCtx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	client scan.Client,
	serviceURL string,
	deps Deps,
	opts Options,
) (Result, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	result := NewResult(serviceURL)
	result.RepoTarget = strings.TrimSpace(deps.WorkspaceRoot)

	// Step 1: detect runtime shape.
	detection := detectFirstRunRuntime(deps.Probe, serviceURL, deps.WorkspaceRoot)
	result.RuntimeShape = detection.Shape
	result = result.addStep("detect runtime", StepOK, detection.Detail)

	// Step 2: verify the runtime is usable (no destructive auto-start).
	verifyStep := verifyFirstRunRuntime(deps.Probe, detection, serviceURL, opts.NoStart)
	result = result.addStep(verifyStep.Name, verifyStep.Status, verifyStep.Detail)
	if verifyStep.Status == StepFailed {
		verifyErr := fmt.Errorf("verify runtime: %s", verifyStep.Detail)
		result = attachFirstRunDiagnostic(result, firstRunVerifySignal(deps, detection, serviceURL, verifyErr))
		result.NextSteps = firstRunNextSteps(result, detection)
		return result, verifyErr
	}

	// Step 3: index the target repository (or reuse an existing index).
	indexed, runErr := ensureFirstRunIndexed(parentCtx, stdout, stderr, client, deps, opts)
	result.RepoIndexed = indexed.Completeness
	result.Readiness = indexed.Readiness
	result = result.addStep("index repository", indexed.Status, indexed.Detail)
	if runErr != nil {
		result = result.addStep("wait for readiness", StepFailed, runErr.Error())
		result = attachFirstRunDiagnostic(result, firstRunReadinessSignal(deps, client, detection, indexed, runErr))
		result.NextSteps = firstRunNextSteps(result, detection)
		return result, runErr
	}
	result = result.addStep("wait for readiness", StepOK, indexed.Readiness)

	// Step 4: run one bounded API-backed query as the truthful end proof.
	answer, queryErr := runFirstRunQuery(deps, client)
	if queryErr != nil {
		result = result.addStep("first query", StepFailed, queryErr.Error())
		result = attachFirstRunDiagnostic(result, firstRunQuerySignal(queryErr))
		result.NextSteps = firstRunNextSteps(result, detection)
		return result, fmt.Errorf("first query: %w", queryErr)
	}
	result.QueryAnswered = true
	result.QuerySummary = answer
	result = result.addStep("first query", StepOK, answer)
	// A successful query that found zero repositories is truthful success, but the
	// operator has nothing to query yet. Attach the empty-index advisory so the
	// next action is clear without marking the run failed.
	if isEmptyRepositoriesAnswer(answer) {
		result = attachFirstRunDiagnostic(result, firstRunEmptyRepoSignal())
	}
	result.NextSteps = firstRunNextSteps(result, detection)
	return result, nil
}

// firstRunIndexOutcome captures the readiness and completeness of the index
// step in a form the summary can render truthfully.
type firstRunIndexOutcome struct {
	Status       StepStatus
	Detail       string
	Completeness string
	Readiness    string
}

// runFirstRunQuery runs the bounded repositories query and returns a concise
// human summary of the answer. An error is returned only when the query did not
// return; an empty repository list is a valid, truthful answer.
func runFirstRunQuery(deps Deps, client scan.Client) (string, error) {
	if deps.ListRepos == nil {
		return "", errors.New("repositories query seam is not configured")
	}
	response, err := deps.ListRepos(client)
	if err != nil {
		return "", err
	}
	count := len(response.Repositories)
	if count == 0 {
		return "repositories query returned 0 repositories", nil
	}
	first := strings.TrimSpace(response.Repositories[0].Name)
	if first == "" {
		first = strings.TrimSpace(response.Repositories[0].ID)
	}
	return fmt.Sprintf("repositories query returned %d (e.g. %s)", count, first), nil
}

// firstRunNextSteps builds actionable follow-ups tailored to the outcome.
func firstRunNextSteps(result Result, detection firstRunRuntimeDetection) []string {
	if result.succeeded() {
		return []string{
			fmt.Sprintf("Ask a deeper question: eshu story %s", QuoteIfEmpty(result.RepoTarget)),
			"List everything indexed: eshu list",
		}
	}
	switch detection.Shape {
	case ShapeLocalBinaries:
		return []string{
			"Start the local API: eshu api start",
			"Re-run: eshu first-run",
		}
	case ShapeDockerCompose:
		return []string{
			"Start the stack: docker compose up -d",
			"Re-run: eshu first-run",
		}
	case ShapeUnknown:
		return []string{
			"Build the binaries: cd go && make build && export PATH=$PATH:$(pwd)/bin",
			"Or start Docker Compose, then re-run: eshu first-run",
		}
	default:
		return []string{"Re-run: eshu first-run"}
	}
}

// QuoteIfEmpty renders a placeholder for an empty repo target so a
// copy-pasteable command hint never ends in a dangling space. The benchmark
// and demo scorecard renderers in go/cmd/eshu share it for the same reason.
func QuoteIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<repo>"
	}
	return value
}

// Truth labels the freshness and completeness of the first-run outcome
// using the same truth vocabulary as scan.
func Truth(result Result, profile string) map[string]any {
	freshness := "stale"
	completeness := "partial"
	if result.succeeded() && result.RepoIndexed == "complete" {
		freshness = "current"
		completeness = "complete"
	}
	return scan.Truth(freshness, completeness, profile, scan.CurrentGraphBackend())
}
