// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"strings"
)

// hostedStageName identifies one ordered, independently-reported hosted-setup
// verification stage.
type hostedStageName string

const (
	// hostedStageEndpoint resolves and reports the deployed endpoint and whether
	// a bearer token was supplied (the value is never recorded).
	hostedStageEndpoint hostedStageName = "endpoint and auth resolved"
	// hostedStageHealthz proves the deployed service answers /healthz.
	hostedStageHealthz hostedStageName = "healthz reachable"
	// hostedStageReadyz proves the deployed service answers /readyz and that the
	// caller is authenticated.
	hostedStageReadyz hostedStageName = "readyz reachable"
	// hostedStageIndexReadiness classifies the indexed-state of the service from
	// the status/pipeline surface (empty, partial, stale, or ready).
	hostedStageIndexReadiness hostedStageName = "index readiness"
	// hostedStageMCPTools proves the MCP tool surface is visible and non-empty.
	hostedStageMCPTools hostedStageName = "mcp tools visible"
	// hostedStageQuery runs one bounded query and is the only stage whose success
	// proves a useful answer is reachable.
	hostedStageQuery hostedStageName = "first bounded query"
)

// hostedStageStatus is the truthful outcome of a single hosted-setup stage.
type hostedStageStatus string

const (
	// hostedStageOK marks a stage that completed and was proven correct.
	hostedStageOK hostedStageStatus = "ok"
	// hostedStageWarn marks a stage that ran but surfaced a non-fatal concern,
	// such as a partial or building index, so the operator sees the nuance.
	hostedStageWarn hostedStageStatus = "warn"
	// hostedStageFailed marks a stage that did not complete. The detail preserves
	// the underlying cause so the root failure is never hidden.
	hostedStageFailed hostedStageStatus = "failed"
)

// hostedFailCategory is the specific, separately-reported reason a hosted-setup
// stage did not reach a fully-connected state. The acceptance criteria require
// these categories never collapse into a single generic failure.
type hostedFailCategory string

const (
	// hostedFailNone means the stage did not record a failure category.
	hostedFailNone hostedFailCategory = ""
	// hostedFailAuthUnavailable means authentication was rejected (401/403) or no
	// token resolved where one is required.
	hostedFailAuthUnavailable hostedFailCategory = "auth-unavailable"
	// hostedFailUnreachable means the endpoint did not answer a health probe.
	hostedFailUnreachable hostedFailCategory = "unreachable"
	// hostedFailEmptyIndex means the service is reachable but no repository has
	// been indexed yet.
	hostedFailEmptyIndex hostedFailCategory = "empty-index"
	// hostedFailStaleReadiness means the pipeline is degraded, stalled, or carries
	// failed/dead-letter work; its indexed truth is not trustworthy.
	hostedFailStaleReadiness hostedFailCategory = "stale-readiness"
	// hostedFailPartialReadiness means the pipeline is still draining or has no
	// completed generation yet; the index is building, not ready.
	hostedFailPartialReadiness hostedFailCategory = "partial-readiness"
	// hostedFailMissingRepoScope means a requested repository was not present in
	// the indexed set the token can see.
	hostedFailMissingRepoScope hostedFailCategory = "missing-repo-scope"
	// hostedFailMCPUnavailable means the MCP tool surface is empty or unavailable.
	hostedFailMCPUnavailable hostedFailCategory = "mcp-unavailable"
	// hostedFailQueryFailed means the bounded query did not return an answer.
	hostedFailQueryFailed hostedFailCategory = "query-failed"
	// hostedFailUnresolvedEndpoint means no deployed endpoint could be resolved.
	hostedFailUnresolvedEndpoint hostedFailCategory = "unresolved-endpoint"
)

// hostedSetupStage records the outcome and evidence of one hosted-setup stage.
// Category is empty unless the stage recorded a specific failure reason.
type hostedSetupStage struct {
	Name     hostedStageName    `json:"name"`
	Status   hostedStageStatus  `json:"status"`
	Detail   string             `json:"detail,omitempty"`
	Category hostedFailCategory `json:"category,omitempty"`
}

// hostedSetupResult is the canonical, machine-readable outcome of a hosted
// connection attempt. It never reports connected unless the bounded query
// actually returned. It records only a redacted token reference, never the raw
// secret value.
type hostedSetupResult struct {
	Command       string             `json:"command"`
	ServiceURL    string             `json:"service_url"`
	TokenRef      string             `json:"token_ref"`
	Platform      string             `json:"platform,omitempty"`
	Repository    string             `json:"repository,omitempty"`
	IndexState    string             `json:"index_state"`
	ToolCount     int                `json:"tool_count"`
	QueryAnswered bool               `json:"query_answered"`
	QuerySummary  string             `json:"query_summary,omitempty"`
	SetupHint     string             `json:"setup_hint,omitempty"`
	Stages        []hostedSetupStage `json:"stages"`
	NextSteps     []string           `json:"next_steps,omitempty"`
}

// newHostedSetupResult builds an initial result whose index state and query
// outcome are pessimistic until a later stage proves otherwise.
func newHostedSetupResult(serviceURL, tokenRef, platform, repository string) hostedSetupResult {
	return hostedSetupResult{
		Command:    "hosted-setup",
		ServiceURL: serviceURL,
		TokenRef:   tokenRef,
		Platform:   platform,
		Repository: repository,
		IndexState: "unknown",
	}
}

// connected reports whether the flow reached its truthful end state: a bounded
// query actually returned an answer over the deployed endpoint. Health or
// readiness alone is never treated as connected.
func (r hostedSetupResult) connected() bool {
	return r.QueryAnswered
}

// addStage appends a stage outcome and returns the modified result so callers
// can chain stage bookkeeping inline.
func (r hostedSetupResult) addStage(stage hostedSetupStage) hostedSetupResult {
	r.Stages = append(r.Stages, stage)
	return r
}

// firstFailure returns the first stage that recorded a failure category, or an
// empty stage and false when every stage was clean.
func (r hostedSetupResult) firstFailure() (hostedSetupStage, bool) {
	for _, s := range r.Stages {
		if s.Status == hostedStageFailed {
			return s, true
		}
	}
	return hostedSetupStage{}, false
}

// renderHostedSetupHuman writes a concise operator-facing summary. It prints the
// redacted token reference only, never a raw secret, and surfaces the specific
// failure category so the operator can act without reading every deployment
// page.
func renderHostedSetupHuman(w io.Writer, result hostedSetupResult, runErr error) {
	header := "Hosted connection ready"
	if !result.connected() {
		header = "Hosted connection incomplete"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 40))
	_, _ = fmt.Fprintf(w, "  service url   : %s\n", result.ServiceURL)
	_, _ = fmt.Fprintf(w, "  auth token    : %s\n", hostedTokenLine(result.TokenRef))
	if result.Repository != "" {
		_, _ = fmt.Fprintf(w, "  repo scope    : %s\n", result.Repository)
	}
	_, _ = fmt.Fprintf(w, "  index state   : %s\n", result.IndexState)
	_, _ = fmt.Fprintf(w, "  mcp tools     : %d visible\n", result.ToolCount)
	_, _ = fmt.Fprintf(w, "  first query   : %s\n", hostedQueryLine(result))

	for _, stage := range result.Stages {
		marker := hostedStageMarker(stage.Status)
		line := fmt.Sprintf("  %s %s", marker, stage.Name)
		if stage.Category != hostedFailNone {
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

// hostedTokenLine renders the token reference for display, never the raw value.
func hostedTokenLine(tokenRef string) string {
	if strings.TrimSpace(tokenRef) == "" {
		return "none (set ESHU_API_KEY)"
	}
	return tokenRef
}

// hostedQueryLine describes the first-query outcome for the human summary.
func hostedQueryLine(result hostedSetupResult) string {
	if !result.QueryAnswered {
		return "no bounded query returned"
	}
	if result.QuerySummary != "" {
		return result.QuerySummary
	}
	return "answered"
}

// hostedStageMarker maps a stage status to a stable ASCII marker.
func hostedStageMarker(status hostedStageStatus) string {
	switch status {
	case hostedStageOK:
		return "[ok]"
	case hostedStageWarn:
		return "[~~]"
	default:
		return "[!!]"
	}
}
