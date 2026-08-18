// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/reposelector"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// RepoOptions is the resolved `eshu vuln-scan repo` request. go/cmd/eshu
// builds it from cobra flags and validates the ranges there; this package
// never reads a flag itself.
type RepoOptions struct {
	// Scan is the request handed to the scan family for the index-and-wait
	// step that precedes the findings read.
	Scan scan.Options
	// Limit caps the findings read; the wrapper has already bounded it to
	// 1..200.
	Limit int
	// ImpactStatus filters findings server-side when set.
	ImpactStatus string
	// RepoID, when set, is the exact repository id to read and skips selector
	// resolution of the scanned root.
	RepoID string
	// Broad selects the wider scope mode; see ResolveScopeMode.
	Broad bool
	// ExportFormat is empty, ExportFormatSARIF, or ExportFormatVEX. The
	// wrapper rejects anything else and rejects it alongside --json.
	ExportFormat string
}

// RepoClient is the API surface RunRepo needs: the plain GET the repository
// selector and the scan preflight use, and the envelope GET the findings read
// uses. go/cmd/eshu's *APIClient satisfies it as written; it is an interface
// here because that type lives in package main.
type RepoClient interface {
	reposelector.Getter
	EnvelopeFetcher
}

// RepoDeps carries the process-owned collaborators RunRepo needs. Every field
// is supplied by the cobra wrapper in go/cmd/eshu, which owns process contact:
// the API client (configured or the one-shot local runtime's), the scan
// runtime, the output streams, and the local runtime's shutdown.
type RepoDeps struct {
	// Client reads the repository listing and the impact findings.
	Client RepoClient
	// ServiceURL is the API base URL the envelope reports as evidence.
	ServiceURL string
	// ScanRuntime is the scan family's runtime for the same client.
	ScanRuntime scan.Runtime
	// Stdout receives the JSON envelope, the export document, or the human
	// summary. The scan banner and bootstrap output also go here unless
	// --json or --export claims stdout for the machine-readable document.
	Stdout io.Writer
	// Stderr receives the scan child's stderr and the cleanup warning.
	Stderr io.Writer
	// StartedAt is the instant the command began, taken by the wrapper before
	// it started or attached to the local runtime, so the envelope's
	// scan_performance wall time covers that startup as it always has. The
	// zero value means "now", for a caller with no work before RunRepo.
	StartedAt time.Time
	// CloseLocalRuntime stops the one-shot local runtime the wrapper started,
	// or is nil when a configured service URL was used. It runs once, before
	// any output is written, so a shutdown failure reaches the envelope as a
	// warning.
	CloseLocalRuntime func() error
}

// repoEnvelope is the `eshu vuln-scan repo --json` document.
type repoEnvelope struct {
	Data  Result         `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *RepoError     `json:"error"`
}

// RunRepo runs one `eshu vuln-scan repo` invocation: the scan and readiness
// wait, repository resolution, the findings read, the scope guards, the
// performance stamp, local runtime shutdown, and the output document on
// deps.Stdout in whichever shape opts selects.
//
// It returns nil for a clean ready-zero answer. A scanner verdict -- findings
// present, evidence not established, unsupported target evidence, or a scan
// that did not reach ready -- is returned as a *Failure carrying the exit code
// and the operator-facing message; recover it with errors.As. Any other error
// is one the run could not classify: a scan preflight or bootstrap failure, a
// selector that resolved nothing, a transport error, or a write failure. The
// caller decides the process exit code; this function never exits and writes
// only to the writers it is given.
//
// The output document is written for every path that reaches the scan, so an
// early failure still produces a fail-closed envelope rather than an empty
// stream. Which paths still write a report and which carry an error member is
// finishRepo's contract.
func RunRepo(ctx context.Context, deps RepoDeps, opts RepoOptions) error {
	if deps.Stdout == nil || deps.Stderr == nil {
		return errors.New("vulnscan: RunRepo requires Stdout and Stderr")
	}
	startedAt := deps.StartedAt
	if startedAt.IsZero() {
		startedAt = Now()
	}
	result := NewResult(
		Target{
			Path: opts.Scan.Target.Path,
			Root: opts.Scan.Target.Root,
			Kind: opts.Scan.Target.Kind,
		},
		opts.Limit,
		opts.Broad,
		deps.ServiceURL,
	)
	scanStdout := deps.Stdout
	if opts.Scan.JSON || opts.ExportFormat != "" {
		scanStdout = deps.Stderr
	}
	scanResult, err := scan.Execute(ctx, scanStdout, deps.Stderr, deps.ScanRuntime, opts.Scan, !opts.Scan.JSON)
	result.Scan = scanResult
	result.Status = scanResult.Status
	result.Warnings = append(result.Warnings, scanResult.Warnings...)
	if err != nil {
		result.ReadinessState = "target_incomplete"
		RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishRepoAfterCleanup(deps, opts, result, scanResult.Truth, err)
	}
	if scanResult.Status != "ready" {
		result.ReadinessState = "target_incomplete"
		RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		failure := &Failure{
			Message: fmt.Sprintf("vulnerability scan target is not ready; rerun with --%s=true before reading findings", scan.WaitFlag),
			Code:    4,
		}
		return finishRepoAfterCleanup(deps, opts, result, scanResult.Truth, failure)
	}

	repositoryID, err := resolveRepoID(deps.Client, opts)
	if err != nil {
		result.ReadinessState = "evidence_incomplete"
		RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishRepoAfterCleanup(deps, opts, result, scanResult.Truth, err)
	}
	result.RepositoryID = repositoryID

	findings, err := FetchImpactFindings(deps.Client, repositoryID, opts.Limit, opts.ImpactStatus)
	if err != nil {
		result.ReadinessState = "evidence_incomplete"
		RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishRepoAfterCleanup(deps, opts, result, scanResult.Truth, err)
	}
	if findings.Error != nil {
		result.ReadinessState = "evidence_incomplete"
		RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		failure := &Failure{Message: findings.Error.Message, Code: 4}
		return finishRepoAfterCleanup(deps, opts, result, findings.Truth, failure)
	}
	result.Findings = findings.Data.Findings
	result.Count = findings.Data.Count
	result.Limit = findings.Data.Limit
	result.Truncated = findings.Data.Truncated
	result.NextCursor = findings.Data.NextCursor
	result.Readiness = findings.Data.Readiness
	result.ReadinessState = ReadinessState(findings.Data.Readiness, result.Count)

	scopeFailure := ApplyScope(&result)
	RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
	if scopeFailure == nil {
		scopeFailure = ExitFailure(result)
	}
	return finishRepoAfterCleanup(deps, opts, result, findings.Truth, failureError(scopeFailure))
}

// failureError boxes a *Failure into error without producing a non-nil
// interface around a nil pointer. Every scanner-verdict site returns through
// here so a nil verdict stays a nil error.
func failureError(failure *Failure) error {
	if failure == nil {
		return nil
	}
	return failure
}

// resolveRepoID returns the canonical repository ID the scan reports against.
// An explicit --repo-id wins outright; otherwise the scanned root path is
// resolved as a repository selector, so `eshu vuln-scan repo .` names the
// repository the operator is standing in.
//
// The nil-client arm keeps the message the wrapper produced before this logic
// moved. It is defensive: the wrapper never hands RunRepo a nil client, and a
// nil client would already have failed the scan preflight.
func resolveRepoID(client RepoClient, opts RepoOptions) (string, error) {
	if opts.RepoID != "" {
		return opts.RepoID, nil
	}
	if client == nil {
		return "", fmt.Errorf("resolve scanned repository: resolve repo selector %q: missing API client",
			opts.Scan.Target.Root)
	}
	repositoryID, err := reposelector.Resolve(client, opts.Scan.Target.Root)
	if err != nil {
		return "", fmt.Errorf("resolve scanned repository: %w", err)
	}
	return repositoryID, nil
}
