// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// HealthzPath is the deployed liveness probe path served by the runtime
// admin mux.
const HealthzPath = "/healthz"

// ReadyzPath is the deployed readiness probe path served by the runtime
// admin mux. A 401/403 here is treated as an authentication failure.
const ReadyzPath = "/readyz"

// StatusPath is the bounded pipeline status read used to classify the
// indexed state of the deployed service.
const StatusPath = "/api/v0/status/pipeline"

// ReposPath is the bounded repositories query used both to enumerate the
// indexed scope and as the final useful-answer proof.
const ReposPath = "/api/v0/repositories?limit=25"

// Repository is one indexed repository as the hosted checks see it.
type Repository struct {
	// ID is the canonical repository identifier.
	ID string
	// Name is the operator-facing repository name.
	Name string
	// ScopeMatch reports whether an operator-supplied repository selector names
	// this repository. The caller supplies the predicate because selector
	// resolution (path cleaning and symlink evaluation) belongs to the CLI's
	// repository-selector surface, not to the hosted flow. A nil predicate never
	// matches, so a requested scope is reported missing rather than assumed
	// present.
	ScopeMatch func(selector string) bool
}

// RepositoryList is the bounded repositories answer. It is both the
// indexed-scope enumeration the readiness classifier counts and the final
// useful-answer proof.
type RepositoryList struct {
	// Repositories are the repositories the bounded query returned, in server
	// order.
	Repositories []Repository
}

// Deps groups the injectable seams the staged checks call so each stage is
// unit-testable with fakes. Every seam is transport or CLI adaptation the
// caller owns; no seam is optional except ListTools, which tolerates a nil
// value and reports an empty tool surface.
type Deps struct {
	// Health probes the deployed HealthzPath liveness endpoint.
	Health func() error
	// Ready probes the deployed ReadyzPath readiness endpoint; a 401/403 here is
	// classified as an authentication failure.
	Ready func() error
	// Readiness reads the bounded pipeline status at StatusPath and returns the
	// drain verdict used for index classification. The verdict is scan's own
	// type: the caller evaluates the status with scan.EvaluateReadiness, so the
	// hosted flow consumes the same readiness contract `eshu scan` enforces and
	// cannot drift from it, without this package owning the status wire shape.
	Readiness func() (scan.ReadinessVerdict, error)
	// ListTools returns the visible MCP tool surface. It is resolved in-process
	// from the CLI's own tool registry, not over the network.
	ListTools func() []mcp.ToolDefinition
	// ListRepos runs the bounded repositories query at ReposPath, used for scope
	// enumeration and as the final useful-answer proof.
	ListRepos func() (RepositoryList, error)
}

// classifyProbeError maps a probe error to a hosted failure category. A 401/403
// is auth-unavailable; anything else is unreachable. This keeps the
// authentication failure distinct from a transport or service-down failure so
// the operator sees the right remediation. The status is read through
// apierr.StatusCode, so any transport error implementing
// apierr.HTTPStatusError classifies without this package naming the CLI's
// concrete error type.
func classifyProbeError(err error) FailCategory {
	if err == nil {
		return FailNone
	}
	if code, ok := apierr.StatusCode(err); ok && (code == 401 || code == 403) {
		return FailAuthUnavailable
	}
	return FailUnreachable
}

// classifyReadError maps a bounded-read error to a hosted failure category. A
// 401/403 stays auth-unavailable; any other error is query-failed, because the
// failing call is a query against the deployed read surface rather than a bare
// reachability probe.
func classifyReadError(err error) FailCategory {
	if cat := classifyProbeError(err); cat == FailAuthUnavailable {
		return cat
	}
	if err == nil {
		return FailNone
	}
	return FailQueryFailed
}

// ClassifyIndexReadiness inspects the deployed pipeline's drain verdict and the
// indexed repository count, returning the specific readiness category, a human
// detail, and whether the index is fully ready. The categories never collapse:
// an empty index, a building/partial pipeline, and a stale/degraded pipeline are
// each reported distinctly.
func ClassifyIndexReadiness(verdict scan.ReadinessVerdict, repoCount int) (FailCategory, string, bool) {
	// A terminal verdict means failed, dead-letter, degraded, or stalled work:
	// the indexed truth is not trustworthy, so report stale-readiness.
	if verdict.Terminal {
		reason := strings.TrimSpace(verdict.Reason)
		if reason == "" {
			reason = "pipeline reported a terminal unhealthy state"
		}
		return FailStaleReadiness, reason, false
	}

	if verdict.Ready {
		if repoCount == 0 {
			return FailEmptyIndex, "pipeline healthy but no repository is indexed", false
		}
		return FailNone, fmt.Sprintf("pipeline healthy and drained; %d repositories indexed", repoCount), true
	}

	// Not ready and not terminal: the index is still building or draining.
	reason := strings.TrimSpace(verdict.Reason)
	if reason == "" {
		reason = "pipeline is still building the index"
	}
	if repoCount == 0 {
		return FailEmptyIndex, "no repository indexed yet; " + reason, false
	}
	return FailPartialReadiness, reason, false
}

// scopePresent reports whether the requested repository selector matches any
// indexed repository the token can see. An empty selector means no scope was
// requested, which is always satisfied.
func scopePresent(repos RepositoryList, selector string) bool {
	target := strings.TrimSpace(selector)
	if target == "" {
		return true
	}
	for _, repo := range repos.Repositories {
		if repo.ScopeMatch != nil && repo.ScopeMatch(target) {
			return true
		}
	}
	return false
}
