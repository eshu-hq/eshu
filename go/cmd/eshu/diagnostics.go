// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// onboardingFailureClass names a recognized first-run failure category. Each
// class maps to one concrete recovery diagnostic with a stable summary
// fragment, so operators and tests can rely on the wording.
type onboardingFailureClass string

const (
	// onboardingClassDockerRepoPaths means a Docker/Compose runtime cannot see
	// the repository paths it was asked to index (host paths not mounted into
	// the container).
	onboardingClassDockerRepoPaths onboardingFailureClass = "docker_repo_paths"
	// onboardingClassComposeUnhealthy means the Compose stack is not running or
	// is unhealthy, so the API the first-run depends on is unreachable.
	onboardingClassComposeUnhealthy onboardingFailureClass = "compose_unhealthy"
	// onboardingClassBinariesMissing means required eshu-* helper binaries are
	// absent from PATH for the local-binaries runtime shape.
	onboardingClassBinariesMissing onboardingFailureClass = "binaries_missing"
	// onboardingClassAuthMismatch means the API rejected the request with an
	// authentication or authorization error (token missing or wrong).
	onboardingClassAuthMismatch onboardingFailureClass = "auth_mismatch"
	// onboardingClassMCPEndpointIsAPI means a configured MCP endpoint points at
	// the HTTP API instead of the MCP service path.
	onboardingClassMCPEndpointIsAPI onboardingFailureClass = "mcp_endpoint_is_api"
	// onboardingClassIndexingNotReady means health is green but indexing is
	// still building or stale, so a query cannot yet be trusted.
	onboardingClassIndexingNotReady onboardingFailureClass = "indexing_not_ready"
	// onboardingClassQueueFailedWork means the reducer queue has failed,
	// retrying, or dead-letter work that blocks readiness.
	onboardingClassQueueFailedWork onboardingFailureClass = "queue_failed_work"
	// onboardingClassNoRepositories means no repositories match the configured
	// selector, so the bounded query has nothing to answer over.
	onboardingClassNoRepositories onboardingFailureClass = "no_repositories"
	// onboardingClassAssistantToolsHidden means an assistant MCP config exists
	// but the eshu tools are not visible in the client.
	onboardingClassAssistantToolsHidden onboardingFailureClass = "assistant_tools_hidden"
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

// onboardingDiagnostic is the structured, operator-facing classification of a
// first-run failure. It always carries the preserved underlying error so the
// root cause is surfaced alongside the recovery guidance, never instead of it.
type onboardingDiagnostic struct {
	// Class is the recognized failure category.
	Class onboardingFailureClass `json:"class"`
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
func (d onboardingDiagnostic) rootCause() string {
	if d.Underlying == nil {
		return ""
	}
	return d.Underlying.Error()
}

// String renders the diagnostic as a single multi-line block: summary, recovery
// steps, docs link, and the preserved underlying error. The underlying error is
// always included when present so the root cause is never swallowed.
func (d onboardingDiagnostic) String() string {
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
func (d onboardingDiagnostic) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"class":          string(d.Class),
		"summary":        d.Summary,
		"recovery_steps": d.RecoverySteps,
		"docs_link":      d.DocsLink,
		"cause":          d.rootCause(),
	})
}
