// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"fmt"
	"io"
	"strings"
)

// StepStatus is the truthful outcome of a single first-run step.
type StepStatus string

const (
	// StepOK marks a step that completed and was proven correct.
	StepOK StepStatus = "ok"
	// StepSkipped marks a step that was intentionally not run, with a
	// human-readable reason recorded on the step.
	StepSkipped StepStatus = "skipped"
	// StepFailed marks a step that did not complete. The step detail
	// preserves the underlying error so the root cause is never hidden.
	StepFailed StepStatus = "failed"
)

// RuntimeShape names the runtime topology the command walked.
type RuntimeShape string

const (
	// ShapeExistingAPI means a reachable Eshu API already answers
	// readiness and query calls, so no local runtime is started.
	ShapeExistingAPI RuntimeShape = "existing_api"
	// ShapeLocalBinaries means the eshu-* binaries are on PATH and the
	// local single-host runtime is the chosen path.
	ShapeLocalBinaries RuntimeShape = "local_binaries"
	// ShapeDockerCompose means a docker-compose.yaml is present and the
	// Compose stack is the chosen path.
	ShapeDockerCompose RuntimeShape = "docker_compose"
	// ShapeUnknown means no usable runtime shape was detected.
	ShapeUnknown RuntimeShape = "unknown"
)

// Step records the outcome and evidence of one orchestration step.
type Step struct {
	Name   string     `json:"name"`
	Status StepStatus `json:"status"`
	Detail string     `json:"detail,omitempty"`
}

// Result is the canonical, machine-readable outcome of a first-run.
// It never reports overall success unless a bounded query actually returned.
type Result struct {
	Command       string       `json:"command"`
	RuntimeShape  RuntimeShape `json:"runtime_shape"`
	ServiceURL    string       `json:"service_url"`
	RepoIndexed   string       `json:"repo_indexed"`
	RepoTarget    string       `json:"repo_target,omitempty"`
	Readiness     string       `json:"readiness"`
	QueryAnswered bool         `json:"query_answered"`
	QuerySummary  string       `json:"query_summary,omitempty"`
	Steps         []Step       `json:"steps"`
	NextSteps     []string     `json:"next_steps,omitempty"`
	// Diagnostic is the classified onboarding failure (or empty-index advisory)
	// attached to the result. It is nil when no failure class matched, so the raw
	// step detail and runErr remain the sole evidence. When present it always
	// carries the preserved underlying error so the root cause is never hidden.
	Diagnostic *Diagnostic    `json:"diagnosis,omitempty"`
	Truth      map[string]any `json:"-"`
}

// succeeded reports whether the first-run reached its truthful end state: a
// bounded query actually returned an answer. Process or readiness state alone
// is never treated as success.
func (r Result) succeeded() bool {
	return r.QueryAnswered
}

// addStep appends a step outcome to the result and returns the modified result
// so callers can chain step bookkeeping inline.
func (r Result) addStep(name string, status StepStatus, detail string) Result {
	r.Steps = append(r.Steps, Step{Name: name, Status: status, Detail: detail})
	return r
}

// NewResult builds an initial failed result so any early return is
// truthful until a later step proves otherwise.
func NewResult(serviceURL string) Result {
	return Result{
		Command:      "first-run",
		RuntimeShape: ShapeUnknown,
		ServiceURL:   serviceURL,
		RepoIndexed:  "unknown",
		Readiness:    "unknown",
	}
}

// RenderHuman writes a concise operator-facing summary of the result.
// On failure it prints actionable next steps and preserves the failing detail.
func RenderHuman(w io.Writer, result Result, runErr error) {
	header := "First-run succeeded"
	if !result.succeeded() {
		header = "First-run incomplete"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 40))
	_, _ = fmt.Fprintf(w, "  runtime shape : %s\n", result.RuntimeShape)
	_, _ = fmt.Fprintf(w, "  service url   : %s\n", result.ServiceURL)
	if result.RepoTarget != "" {
		_, _ = fmt.Fprintf(w, "  repo target   : %s\n", result.RepoTarget)
	}
	_, _ = fmt.Fprintf(w, "  repo indexed  : %s\n", result.RepoIndexed)
	_, _ = fmt.Fprintf(w, "  readiness     : %s\n", result.Readiness)
	_, _ = fmt.Fprintf(w, "  first query   : %s\n", firstRunQueryLine(result))

	for _, step := range result.Steps {
		marker := firstRunStepMarker(step.Status)
		line := fmt.Sprintf("  %s %s", marker, step.Name)
		if step.Detail != "" {
			line += ": " + step.Detail
		}
		_, _ = fmt.Fprintln(w, line)
	}

	if runErr != nil {
		_, _ = fmt.Fprintf(w, "  cause         : %s\n", runErr.Error())
	}
	if result.Diagnostic != nil {
		_, _ = fmt.Fprintln(w, "Diagnosis:")
		_, _ = fmt.Fprintf(w, "  %s\n", indentDiagnostic(result.Diagnostic.String()))
	}
	if len(result.NextSteps) > 0 {
		_, _ = fmt.Fprintln(w, "Next steps:")
		for _, step := range result.NextSteps {
			_, _ = fmt.Fprintf(w, "  - %s\n", step)
		}
	}
}

// indentDiagnostic re-indents the multi-line diagnostic block so it nests under
// the "Diagnosis:" header while keeping the summary, recovery steps, docs link,
// and preserved root cause readable.
func indentDiagnostic(s string) string {
	return strings.ReplaceAll(s, "\n", "\n  ")
}

// firstRunQueryLine describes the first-query outcome for the human summary.
func firstRunQueryLine(result Result) string {
	if !result.QueryAnswered {
		return "no bounded query returned"
	}
	if result.QuerySummary != "" {
		return result.QuerySummary
	}
	return "answered"
}

// firstRunStepMarker maps a step status to a stable ASCII marker.
func firstRunStepMarker(status StepStatus) string {
	switch status {
	case StepOK:
		return "[ok]"
	case StepSkipped:
		return "[--]"
	default:
		return "[!!]"
	}
}
