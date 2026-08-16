// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"fmt"
	"io"
	"strings"
)

// StageName identifies one ordered, independently-reported hosted-setup
// verification stage.
type StageName string

const (
	// StageEndpoint resolves and reports the deployed endpoint and whether
	// a bearer token was supplied (the value is never recorded).
	StageEndpoint StageName = "endpoint and auth resolved"
	// StageHealthz proves the deployed service answers /healthz.
	StageHealthz StageName = "healthz reachable"
	// StageReadyz proves the deployed service answers /readyz and that the
	// caller is authenticated.
	StageReadyz StageName = "readyz reachable"
	// StageIndexReadiness classifies the indexed-state of the service from
	// the status/pipeline surface (empty, partial, stale, or ready).
	StageIndexReadiness StageName = "index readiness"
	// StageMCPTools proves the MCP tool surface is visible and non-empty.
	StageMCPTools StageName = "mcp tools visible"
	// StageQuery runs one bounded query and is the only stage whose success
	// proves a useful answer is reachable.
	StageQuery StageName = "first bounded query"
)

// StageStatus is the truthful outcome of a single hosted-setup stage.
type StageStatus string

const (
	// StageOK marks a stage that completed and was proven correct.
	StageOK StageStatus = "ok"
	// StageWarn marks a stage that ran but surfaced a non-fatal concern,
	// such as a partial or building index, so the operator sees the nuance.
	StageWarn StageStatus = "warn"
	// StageFailed marks a stage that did not complete. The detail preserves
	// the underlying cause so the root failure is never hidden.
	StageFailed StageStatus = "failed"
)

// FailCategory is the specific, separately-reported reason a hosted-setup
// stage did not reach a fully-connected state. The acceptance criteria require
// these categories never collapse into a single generic failure.
type FailCategory string

const (
	// FailNone means the stage did not record a failure category.
	FailNone FailCategory = ""
	// FailAuthUnavailable means authentication was rejected (401/403) or no
	// token resolved where one is required.
	FailAuthUnavailable FailCategory = "auth-unavailable"
	// FailUnreachable means the endpoint did not answer a health probe.
	FailUnreachable FailCategory = "unreachable"
	// FailEmptyIndex means the service is reachable but no repository has
	// been indexed yet.
	FailEmptyIndex FailCategory = "empty-index"
	// FailStaleReadiness means the pipeline is degraded, stalled, or carries
	// failed/dead-letter work; its indexed truth is not trustworthy.
	FailStaleReadiness FailCategory = "stale-readiness"
	// FailPartialReadiness means the pipeline is still draining or has no
	// completed generation yet; the index is building, not ready.
	FailPartialReadiness FailCategory = "partial-readiness"
	// FailMissingRepoScope means a requested repository was not present in
	// the indexed set the token can see.
	FailMissingRepoScope FailCategory = "missing-repo-scope"
	// FailMCPUnavailable means the MCP tool surface is empty or unavailable.
	FailMCPUnavailable FailCategory = "mcp-unavailable"
	// FailQueryFailed means the bounded query did not return an answer.
	FailQueryFailed FailCategory = "query-failed"
	// FailUnresolvedEndpoint means no deployed endpoint could be resolved.
	FailUnresolvedEndpoint FailCategory = "unresolved-endpoint"
)

// Stage records the outcome and evidence of one hosted-setup stage.
// Category is empty unless the stage recorded a specific failure reason.
type Stage struct {
	Name     StageName    `json:"name"`
	Status   StageStatus  `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Category FailCategory `json:"category,omitempty"`
}

// Result is the canonical, machine-readable outcome of a hosted
// connection attempt. It never reports connected unless the bounded query
// actually returned. It records only a redacted token reference, never the raw
// secret value.
type Result struct {
	Command       string   `json:"command"`
	ServiceURL    string   `json:"service_url"`
	TokenRef      string   `json:"token_ref"`
	Platform      string   `json:"platform,omitempty"`
	Repository    string   `json:"repository,omitempty"`
	IndexState    string   `json:"index_state"`
	ToolCount     int      `json:"tool_count"`
	QueryAnswered bool     `json:"query_answered"`
	QuerySummary  string   `json:"query_summary,omitempty"`
	SetupHint     string   `json:"setup_hint,omitempty"`
	Stages        []Stage  `json:"stages"`
	NextSteps     []string `json:"next_steps,omitempty"`
}

// newResult builds an initial result whose index state and query
// outcome are pessimistic until a later stage proves otherwise.
func newResult(serviceURL, tokenRef, platform, repository string) Result {
	return Result{
		Command:    "hosted-setup",
		ServiceURL: serviceURL,
		TokenRef:   tokenRef,
		Platform:   platform,
		Repository: repository,
		IndexState: "unknown",
	}
}

// Connected reports whether the flow reached its truthful end state: a bounded
// query actually returned an answer over the deployed endpoint. Health or
// readiness alone is never treated as connected.
func (r Result) Connected() bool {
	return r.QueryAnswered
}

// addStage appends a stage outcome and returns the modified result so callers
// can chain stage bookkeeping inline.
func (r Result) addStage(stage Stage) Result {
	r.Stages = append(r.Stages, stage)
	return r
}

// firstFailure returns the first stage that recorded a failure category, or an
// empty stage and false when every stage was clean. It matches on the category,
// not on StageFailed, because the not-ready index path records its stage as
// StageWarn with a category — matching status alone left the empty/partial/
// stale next-step branches unreachable and sent those operators the generic
// re-run step.
func (r Result) firstFailure() (Stage, bool) {
	for _, s := range r.Stages {
		if s.Category != FailNone {
			return s, true
		}
	}
	return Stage{}, false
}

// RenderSetupHuman writes a concise operator-facing summary. It prints the
// redacted token reference only, never a raw secret, and surfaces the specific
// failure category so the operator can act without reading every deployment
// page.
func RenderSetupHuman(w io.Writer, result Result, runErr error) {
	header := "Hosted connection ready"
	if !result.Connected() {
		header = "Hosted connection incomplete"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 40))
	_, _ = fmt.Fprintf(w, "  service url   : %s\n", result.ServiceURL)
	_, _ = fmt.Fprintf(w, "  auth token    : %s\n", tokenLine(result.TokenRef))
	if result.Repository != "" {
		_, _ = fmt.Fprintf(w, "  repo scope    : %s\n", result.Repository)
	}
	_, _ = fmt.Fprintf(w, "  index state   : %s\n", result.IndexState)
	_, _ = fmt.Fprintf(w, "  mcp tools     : %d visible\n", result.ToolCount)
	_, _ = fmt.Fprintf(w, "  first query   : %s\n", queryLine(result))

	for _, stage := range result.Stages {
		marker := stageMarker(stage.Status)
		line := fmt.Sprintf("  %s %s", marker, stage.Name)
		if stage.Category != FailNone {
			line += fmt.Sprintf(" [%s]", stage.Category)
		}
		if stage.Detail != "" {
			line += ": " + stage.Detail
		}
		_, _ = fmt.Fprintln(w, line)
	}

	if runErr != nil {
		_, _ = fmt.Fprintf(w, "  cause         : %s\n", runErr.Error())
	}
	if strings.TrimSpace(result.SetupHint) != "" {
		_, _ = fmt.Fprintln(w, "")
		_, _ = fmt.Fprint(w, result.SetupHint)
	}
	if len(result.NextSteps) > 0 {
		_, _ = fmt.Fprintln(w, "Next steps:")
		for _, step := range result.NextSteps {
			_, _ = fmt.Fprintf(w, "  - %s\n", step)
		}
	}
}

// SetupEnvelope wraps a result and the truthful run outcome in the canonical
// {data, truth, error} response envelope the `--json` output writes. The caller
// keeps the run error as its own return value so the process exit code still
// reflects whether the bounded query returned.
func SetupEnvelope(result Result, runErr error) map[string]any {
	envelope := map[string]any{
		"data":  result,
		"truth": nil,
		"error": nil,
	}
	if runErr != nil {
		envelope["error"] = map[string]any{"message": runErr.Error()}
	}
	return envelope
}

// tokenLine renders the token reference for display, never the raw value.
func tokenLine(tokenRef string) string {
	if strings.TrimSpace(tokenRef) == "" {
		return "none (set ESHU_API_KEY)"
	}
	return tokenRef
}

// queryLine describes the first-query outcome for the human summary.
func queryLine(result Result) string {
	if !result.QueryAnswered {
		return "no bounded query returned"
	}
	if result.QuerySummary != "" {
		return result.QuerySummary
	}
	return "answered"
}

// stageMarker maps a stage status to a stable ASCII marker.
func stageMarker(status StageStatus) string {
	switch status {
	case StageOK:
		return "[ok]"
	case StageWarn:
		return "[~~]"
	default:
		return "[!!]"
	}
}
