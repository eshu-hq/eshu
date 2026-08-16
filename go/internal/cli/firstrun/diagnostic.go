// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// FailureClass names a recognized first-run failure category. Each
// class maps to one concrete recovery diagnostic with a stable summary
// fragment, so operators and tests can rely on the wording.
type FailureClass string

const (
	// ClassDockerRepoPaths means a Docker/Compose runtime cannot see
	// the repository paths it was asked to index (host paths not mounted into
	// the container).
	ClassDockerRepoPaths FailureClass = "docker_repo_paths"
	// ClassComposeUnhealthy means the Compose stack is not running or
	// is unhealthy, so the API the first-run depends on is unreachable.
	ClassComposeUnhealthy FailureClass = "compose_unhealthy"
	// ClassBinariesMissing means required eshu-* helper binaries are
	// absent from PATH for the local-binaries runtime shape.
	ClassBinariesMissing FailureClass = "binaries_missing"
	// ClassAuthMismatch means the API rejected the request with an
	// authentication or authorization error (token missing or wrong).
	ClassAuthMismatch FailureClass = "auth_mismatch"
	// ClassMCPEndpointIsAPI means a configured MCP endpoint points at
	// the HTTP API instead of the MCP service path.
	ClassMCPEndpointIsAPI FailureClass = "mcp_endpoint_is_api"
	// ClassIndexingNotReady means health is green but indexing is
	// still building or stale, so a query cannot yet be trusted.
	ClassIndexingNotReady FailureClass = "indexing_not_ready"
	// ClassQueueFailedWork means the reducer queue has failed,
	// retrying, or dead-letter work that blocks readiness.
	ClassQueueFailedWork FailureClass = "queue_failed_work"
	// ClassNoRepositories means no repositories match the configured
	// selector, so the bounded query has nothing to answer over.
	ClassNoRepositories FailureClass = "no_repositories"
	// ClassAssistantToolsHidden means an assistant MCP config exists
	// but the eshu tools are not visible in the client.
	ClassAssistantToolsHidden FailureClass = "assistant_tools_hidden"
)

// onboardingStep names the first-run step that produced a failure signal. It
// lets the classifier disambiguate otherwise similar errors by stage.
type onboardingStep string

const (
	// onboardingStepVerify is the runtime-verification step.
	onboardingStepVerify onboardingStep = "verify"
	// onboardingStepIndex is the index/scan step.
	onboardingStepIndex onboardingStep = "index"
	// onboardingStepReadiness is the wait-for-readiness step.
	onboardingStepReadiness onboardingStep = "readiness"
	// onboardingStepQuery is the bounded first-query step.
	onboardingStepQuery onboardingStep = "query"
)

// Diagnostic is the structured, operator-facing classification of a
// first-run failure. It always carries the preserved underlying error so the
// root cause is surfaced alongside the recovery guidance, never instead of it.
type Diagnostic struct {
	// Class is the recognized failure category.
	Class FailureClass `json:"class"`
	// Summary is a stable, human-readable one-line description of the failure.
	Summary string `json:"summary"`
	// RecoverySteps are concrete, copy-pasteable actions to resolve the failure.
	RecoverySteps []string `json:"recovery_steps"`
	// DocsLink is a repo-relative docs path the operator can open for context.
	DocsLink string `json:"docs_link"`
	// Underlying is the preserved root-cause error. It is never discarded so the
	// transport, process, or queue evidence remains visible.
	Underlying error `json:"-"`
}

// rootCause returns the preserved underlying error text, or an empty string
// when no underlying error was attached.
func (d Diagnostic) rootCause() string {
	if d.Underlying == nil {
		return ""
	}
	return d.Underlying.Error()
}

// String renders the diagnostic as a single multi-line block: summary, recovery
// steps, docs link, and the preserved underlying error. The underlying error is
// always included when present so the root cause is never swallowed.
func (d Diagnostic) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s", d.Summary)
	for _, step := range d.RecoverySteps {
		fmt.Fprintf(&b, "\n  - %s", step)
	}
	if strings.TrimSpace(d.DocsLink) != "" {
		fmt.Fprintf(&b, "\n  docs: %s", d.DocsLink)
	}
	if cause := d.rootCause(); cause != "" {
		fmt.Fprintf(&b, "\n  cause: %s", cause)
	}
	return b.String()
}

// MarshalJSON renders the diagnostic for the JSON envelope. The preserved
// underlying error is emitted as a string under "cause" so machine consumers
// also see the root cause instead of losing it to the unexported error field.
//
//nolint:wrapcheck // the marshal error reaches the operator through encoding/json's own error text inside the envelope write; wrapping would change CLI output parity.
func (d Diagnostic) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"class":          string(d.Class),
		"summary":        d.Summary,
		"recovery_steps": d.RecoverySteps,
		"docs_link":      d.DocsLink,
		"cause":          d.rootCause(),
	})
}

// UnmarshalJSON restores a diagnostic from the envelope, including the root
// cause. It exists because MarshalJSON emits "cause" from Underlying, which is
// unexported and tagged `json:"-"`: the default decode reads every other field
// and silently drops that one, leaving this type in breach of its own contract
// that the underlying error is never discarded.
//
// The consumer is `eshu first-run report --from <saved envelope>`, which
// decodes through ParseEnvelope and prints the cause line only when rootCause()
// is non-empty. Without this the operator gets a report with the root cause
// blank and no indication anything was lost.
//
// Decoding into a local wire struct rather than an alias of Diagnostic keeps
// this from recursing back into itself, and keeps the decode exactly as strict
// as before: a mistyped field still fails the parse.
func (d *Diagnostic) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Class         FailureClass `json:"class"`
		Summary       string       `json:"summary"`
		RecoverySteps []string     `json:"recovery_steps"`
		DocsLink      string       `json:"docs_link"`
		Cause         string       `json:"cause"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return fmt.Errorf("decode first-run diagnostic: %w", err)
	}

	*d = Diagnostic{
		Class:         wire.Class,
		Summary:       wire.Summary,
		RecoverySteps: wire.RecoverySteps,
		DocsLink:      wire.DocsLink,
	}
	// An absent or empty cause leaves Underlying nil, which is what an envelope
	// written before this key existed decodes to, and what a diagnostic with no
	// underlying error emits.
	if wire.Cause != "" {
		d.Underlying = errors.New(wire.Cause)
	}
	return nil
}
