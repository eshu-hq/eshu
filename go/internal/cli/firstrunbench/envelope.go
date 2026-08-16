// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrunbench

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Envelope is the benchmark's view of the canonical `{data, truth, error}`
// envelope emitted by `eshu first-run --json`. The evaluator consumes this
// structured result rather than scraping human text, so the first-answer
// verdict is derived from the same fields the command already proves.
//
// The field set mirrors the wire shape the first-run command persists
// (declared in go/cmd/eshu, package main, which this package cannot import).
// Every field keeps the emitter's JSON tag and an equivalent type so decoding
// stays exactly as strict as the canonical envelope: a corrupt artifact fails
// the parse instead of being silently scored. The wire-parity test in
// go/cmd/eshu pins the two shapes together.
type Envelope struct {
	// Data is the machine-readable first-run result.
	Data Result `json:"data"`
	// Truth carries the freshness/completeness/backend labels for the answer.
	// A missing or empty Truth map means the answer lacks provenance and the
	// benchmark must not trust it.
	Truth map[string]any `json:"truth"`
	// Error is non-nil when the run failed; any error fails the benchmark.
	Error *EnvelopeError `json:"error"`
}

// EnvelopeError mirrors the error object inside the JSON envelope.
type EnvelopeError struct {
	// Message is the human-readable failure cause preserved by first-run.
	Message string `json:"message"`
}

// Result is the machine-readable first-run outcome inside the envelope. Only
// QueryAnswered, QuerySummary, RepoTarget, RepoIndexed, Steps, and NextSteps
// feed criteria; the remaining fields exist so decode strictness matches the
// canonical envelope shape.
type Result struct {
	Command       string      `json:"command"`
	RuntimeShape  string      `json:"runtime_shape"`
	ServiceURL    string      `json:"service_url"`
	RepoIndexed   string      `json:"repo_indexed"`
	RepoTarget    string      `json:"repo_target,omitempty"`
	Readiness     string      `json:"readiness"`
	QueryAnswered bool        `json:"query_answered"`
	QuerySummary  string      `json:"query_summary,omitempty"`
	Steps         []Step      `json:"steps"`
	NextSteps     []string    `json:"next_steps,omitempty"`
	Diagnostic    *Diagnostic `json:"diagnosis,omitempty"`
}

// Step records the outcome and evidence of one first-run orchestration step.
type Step struct {
	Name   string     `json:"name"`
	Status StepStatus `json:"status"`
	Detail string     `json:"detail,omitempty"`
}

// StepStatus is the truthful outcome of a single first-run step. The wire also
// carries "skipped"; only the values the benchmark reads are named here.
type StepStatus string

const (
	// StepOK marks a step that completed and was proven correct.
	StepOK StepStatus = "ok"
	// StepFailed marks a step that did not complete. The step detail preserves
	// the underlying error, which is what makes a failure explainable.
	StepFailed StepStatus = "failed"
)

// Diagnostic mirrors the classified onboarding failure attached to a first-run
// result. The benchmark reads none of these fields; the struct exists so a
// mistyped diagnosis block fails the decode exactly as it does for the
// canonical envelope.
type Diagnostic struct {
	Class         string   `json:"class"`
	Summary       string   `json:"summary"`
	RecoverySteps []string `json:"recovery_steps"`
	DocsLink      string   `json:"docs_link"`
}

// ParseEnvelope decodes the canonical `eshu first-run --json` output. It
// returns a descriptive error when the payload is not the expected envelope so
// the harness fails loudly rather than silently scoring malformed input.
func ParseEnvelope(raw []byte) (Envelope, error) {
	var env Envelope
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&env); err != nil {
		return Envelope{}, fmt.Errorf("decode first-run envelope: %w", err)
	}
	return env, nil
}

// ReadEnvelope reads the envelope bytes from a file path or, when the path is
// empty, from the provided reader (stdin).
func ReadEnvelope(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read envelope from stdin: %w", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local benchmark artifact path, not an HTTP request param //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("read envelope file %q: %w", path, err)
	}
	return raw, nil
}
