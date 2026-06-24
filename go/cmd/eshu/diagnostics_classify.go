// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// onboardingSignal is the pure, structured input to the classifier. It carries
// the failing step, the detected runtime shape, the preserved underlying error,
// and the small set of boolean/structured signals that distinguish failure
// classes. Callers populate only the fields they have evidence for; an empty
// signal yields no diagnostic.
type onboardingSignal struct {
	// Step is the first-run step that produced the failure.
	Step onboardingStep
	// Shape is the detected runtime topology.
	Shape firstRunRuntimeShape
	// Underlying is the root-cause error, preserved verbatim in the diagnostic.
	Underlying error

	// RuntimeFailed marks that runtime verification failed (API unreachable).
	RuntimeFailed bool
	// RuntimeDetail is the human-readable verification detail, if any.
	RuntimeDetail string
	// ComposeDetected marks that a docker-compose file was found at the root.
	ComposeDetected bool
	// MissingBinaries lists required eshu-* binaries absent from PATH.
	MissingBinaries []string
	// RepoPathDenied marks that Docker could not see the repository paths.
	RepoPathDenied bool

	// MCPEndpoint is a configured MCP URL to validate against the API base.
	MCPEndpoint string
	// APIBaseURL is the resolved API base URL used for the MCP heuristic.
	APIBaseURL string

	// Readiness is the readiness verdict when the failure is readiness-related.
	Readiness scanReadinessVerdict
	// Queue is the queue snapshot used to distinguish stuck vs failed work.
	Queue scanQueue

	// EmptyRepoList marks that the repository list/selector returned nothing.
	EmptyRepoList bool
	// Selector is the repository selector that matched nothing, if any.
	Selector string

	// AssistantConfigured marks that an assistant MCP config exists on disk.
	AssistantConfigured bool
	// AssistantToolsVisible reports whether the assistant currently lists the
	// eshu tools. False with AssistantConfigured true is the hidden-tools class.
	AssistantToolsVisible bool
}

// classifyOnboardingFailure maps a structured signal to exactly one diagnostic.
// It is pure and table-driven: each predicate that fires returns the matching
// diagnostic with a stable summary fragment, concrete recovery steps, a docs
// link, and the preserved underlying error. When no predicate matches it
// returns ok=false so the caller surfaces the raw root-cause error unchanged.
//
// Ordering is significant: the most specific, evidence-backed signals are
// checked before broader runtime-shape fallbacks so a precise class is never
// masked by a generic one.
func classifyOnboardingFailure(signal onboardingSignal) (onboardingDiagnostic, bool) {
	for _, rule := range onboardingRules() {
		if rule.match(signal) {
			return rule.build(signal), true
		}
	}
	return onboardingDiagnostic{}, false
}

// onboardingRule pairs a predicate over the signal with a diagnostic builder.
type onboardingRule struct {
	match func(onboardingSignal) bool
	build func(onboardingSignal) onboardingDiagnostic
}

// onboardingRules is the ordered classification table. Most specific first.
func onboardingRules() []onboardingRule {
	return []onboardingRule{
		{
			match: func(s onboardingSignal) bool { return s.RepoPathDenied },
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassDockerRepoPaths,
					Summary: "Docker cannot see the repository paths it was asked to index",
					RecoverySteps: []string{
						"Mount the repo into the container: add the host path under volumes in docker-compose.yaml",
						"Confirm the bootstrap target resolves to a path visible inside the container",
						"Re-run after the mount is in place: eshu first-run",
					},
					DocsLink:   "docs/public/run-locally/docker-compose.md",
					Underlying: s.Underlying,
				}
			},
		},
		{
			match: func(s onboardingSignal) bool { return len(s.MissingBinaries) > 0 },
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassBinariesMissing,
					Summary: fmt.Sprintf("CLI helper binaries are missing from PATH: %s", strings.Join(s.MissingBinaries, ", ")),
					RecoverySteps: []string{
						"Build the binaries: cd go && make build",
						"Add them to PATH: export PATH=$PATH:$(pwd)/bin",
						"Re-run: eshu first-run",
					},
					DocsLink:   "docs/public/reference/local-testing.md",
					Underlying: s.Underlying,
				}
			},
		},
		{
			match: mcpEndpointSignalMatches,
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassMCPEndpointIsAPI,
					Summary: fmt.Sprintf("MCP endpoint points at the API instead of the MCP service: %s", s.MCPEndpoint),
					RecoverySteps: []string{
						"Point the MCP client at the MCP service path, e.g. http://<mcp-host>:<mcp-port>/mcp/message",
						"For local stdio, use command \"eshu\" with args [\"mcp\", \"start\"] instead of an HTTP URL",
						"Re-run setup: eshu mcp setup",
					},
					DocsLink:   "docs/public/guides/mcp-guide.md",
					Underlying: s.Underlying,
				}
			},
		},
		{
			match: func(s onboardingSignal) bool { return isAuthError(s.Underlying) },
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassAuthMismatch,
					Summary: "API auth/token mismatch: the API rejected the request as unauthorized",
					RecoverySteps: []string{
						"Set a matching token: export ESHU_API_KEY=<server token>",
						"Confirm the API's configured key matches the client key",
						"Re-run: eshu first-run",
					},
					DocsLink:   "docs/public/reference/http-api.md",
					Underlying: s.Underlying,
				}
			},
		},
		{
			match: func(s onboardingSignal) bool {
				return s.AssistantConfigured && !s.AssistantToolsVisible
			},
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassAssistantToolsHidden,
					Summary: "Assistant config exists but the eshu tools are not visible in the client",
					RecoverySteps: []string{
						"Fully restart the assistant so it reloads the MCP server list",
						"Verify the config file is in the path the client actually reads",
						"Re-run verification: eshu mcp setup --verify",
					},
					DocsLink:   "docs/public/guides/mcp-guide.md",
					Underlying: s.Underlying,
				}
			},
		},
		{
			match: func(s onboardingSignal) bool { return queueHasFailedWork(s.Queue) },
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassQueueFailedWork,
					Summary: fmt.Sprintf("Queue has blocked work (%s)", queueFailureDetail(s.Queue)),
					RecoverySteps: []string{
						"Inspect the failing work: eshu admin facts dead-letter",
						"Resolve or retry the failed/dead-letter items before re-querying",
						"Re-run once the queue drains: eshu first-run",
					},
					DocsLink:   "docs/public/operate/troubleshooting.md",
					Underlying: s.Underlying,
				}
			},
		},
		{
			match: func(s onboardingSignal) bool { return s.EmptyRepoList },
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassNoRepositories,
					Summary: noRepositoriesSummary(s.Selector),
					RecoverySteps: []string{
						"Index a repository first: eshu scan <path>",
						"Widen or correct the selector so it matches an indexed repo",
						"List what is indexed: eshu list",
					},
					DocsLink:   "docs/public/getting-started/first-successful-run.md",
					Underlying: s.Underlying,
				}
			},
		},
		{
			match: func(s onboardingSignal) bool {
				return s.Step == onboardingStepReadiness && readinessStillBuilding(s.Readiness, s.Queue)
			},
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassIndexingNotReady,
					Summary: "Health is green but indexing is still building or stale",
					RecoverySteps: []string{
						"Wait for the queue to drain, then re-check: eshu index-status",
						"Increase the readiness budget: eshu first-run --timeout 30m",
						"Re-run once indexing completes: eshu first-run",
					},
					DocsLink:   "docs/public/operate/health-checks.md",
					Underlying: s.Underlying,
				}
			},
		},
		{
			match: func(s onboardingSignal) bool {
				return s.RuntimeFailed && (s.Shape == firstRunShapeDockerCompose || s.ComposeDetected)
			},
			build: func(s onboardingSignal) onboardingDiagnostic {
				return onboardingDiagnostic{
					Class:   onboardingClassComposeUnhealthy,
					Summary: "Compose services are not running or are unhealthy",
					RecoverySteps: []string{
						"Start the stack: docker compose up -d",
						"Check service health: docker compose ps",
						"Re-run once the API reports healthy: eshu first-run",
					},
					DocsLink:   "docs/public/run-locally/docker-compose.md",
					Underlying: s.Underlying,
				}
			},
		},
	}
}

// mcpEndpointSignalMatches reports whether the signal carries an MCP endpoint
// that resolves to the API instead of the MCP service.
func mcpEndpointSignalMatches(s onboardingSignal) bool {
	return mcpEndpointLooksLikeAPI(s.MCPEndpoint, s.APIBaseURL)
}

// mcpEndpointLooksLikeAPI is the heuristic that flags a configured MCP URL that
// actually targets the HTTP API. An endpoint is API-shaped when it carries an
// /api path segment, or when it is the bare API base (same host:port, no MCP
// path). A genuine MCP endpoint carries an /mcp path and is not flagged even on
// the same host:port as the API. An empty endpoint is never flagged.
func mcpEndpointLooksLikeAPI(endpoint, apiBase string) bool {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return false
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	path := strings.Trim(parsed.Path, "/")
	// A genuine MCP endpoint advertises an /mcp path; never flag it.
	if strings.HasPrefix(path, "mcp") {
		return false
	}
	// Any /api path is API-shaped.
	if path == "api" || strings.HasPrefix(path, "api/") {
		return true
	}
	// A bare endpoint (no MCP path) that matches the API base host:port is the
	// API itself being used as an MCP endpoint.
	if path == "" {
		base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
		if base == "" {
			return false
		}
		baseParsed, err := url.Parse(base)
		if err != nil {
			return false
		}
		return parsed.Host == baseParsed.Host
	}
	return false
}

// isAuthError reports whether the error is an HTTP 401/403 from the API client.
func isAuthError(err error) bool {
	var apiErr *apiHTTPError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 401 || apiErr.StatusCode == 403
	}
	return false
}

// queueHasFailedWork reports whether the queue snapshot carries failed,
// retrying, or dead-letter work that blocks readiness.
func queueHasFailedWork(q scanQueue) bool {
	return q.DeadLetter > 0 || q.Failed > 0 || q.Retrying > 0
}

// queueFailureDetail renders a stable, specific description of the blocked work.
func queueFailureDetail(q scanQueue) string {
	parts := make([]string, 0, 3)
	if q.DeadLetter > 0 {
		parts = append(parts, fmt.Sprintf("dead-letter=%d", q.DeadLetter))
	}
	if q.Failed > 0 {
		parts = append(parts, fmt.Sprintf("failed=%d", q.Failed))
	}
	if q.Retrying > 0 {
		parts = append(parts, fmt.Sprintf("retrying=%d", q.Retrying))
	}
	if len(parts) == 0 {
		return "blocked work present"
	}
	return strings.Join(parts, ", ")
}

// readinessStillBuilding reports whether the pipeline is healthy-ish but
// indexing has not finished: a non-terminal readiness verdict with outstanding
// queue work and no failed/dead-letter items.
func readinessStillBuilding(verdict scanReadinessVerdict, q scanQueue) bool {
	if verdict.Ready || verdict.Terminal || queueHasFailedWork(q) {
		return false
	}
	if q.Outstanding > 0 || q.Pending > 0 || q.InFlight > 0 {
		return true
	}
	// Fall back to the verdict reason when the queue snapshot is empty.
	return strings.Contains(strings.ToLower(verdict.Reason), "outstanding") ||
		strings.Contains(strings.ToLower(verdict.Reason), "pending") ||
		strings.Contains(strings.ToLower(verdict.Reason), "not healthy")
}

// noRepositoriesSummary renders the empty-selector summary, including the
// selector when one was provided.
func noRepositoriesSummary(selector string) string {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "No repositories match: the index is empty"
	}
	return fmt.Sprintf("No repositories match the selector %q", selector)
}
