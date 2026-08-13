// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpsetup

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// VerifyStage names one independently-reported verification step.
type VerifyStage string

const (
	// StageConfigGenerated proves a valid platform snippet was produced.
	StageConfigGenerated VerifyStage = "config generated"
	// StageClientReachable proves the configured endpoint answers a health probe.
	StageClientReachable VerifyStage = "client reachable"
	// StageToolsVisible proves the MCP tool surface is non-empty.
	StageToolsVisible VerifyStage = "tools visible"
	// StageFirstQuery proves one bounded read query succeeds end to end.
	StageFirstQuery VerifyStage = "first query successful"
)

// StageResult is the outcome of one verification stage.
type StageResult struct {
	// Stage is the stage name.
	Stage VerifyStage
	// OK reports success.
	OK bool
	// Detail is a short human explanation (endpoint, count, or failure cause).
	Detail string
	// Skipped marks a stage that did not run (for example, no endpoint to probe).
	Skipped bool
}

// VerifyReport is the ordered set of stage results.
type VerifyReport struct {
	// Stages holds each stage outcome in execution order.
	Stages []StageResult
}

// AllOK reports whether every non-skipped stage passed.
func (r VerifyReport) AllOK() bool {
	for _, s := range r.Stages {
		if s.Skipped {
			continue
		}
		if !s.OK {
			return false
		}
	}
	return true
}

// HealthProber probes endpoint reachability. It is an interface so tests can
// inject deterministic reachability without a live server, and so go/cmd/eshu
// can implement it over *APIClient without this package depending on the CLI's
// process-level client type.
type HealthProber interface {
	// Reachable returns nil when the endpoint answers a health probe.
	Reachable() error
}

// QueryProber runs one bounded smoke query. It is an interface so tests can
// inject success or failure without a live backend, and so go/cmd/eshu can
// implement it over *APIClient without this package depending on the CLI's
// process-level client type.
type QueryProber interface {
	// Smoke runs one bounded read and returns nil on success.
	Smoke() error
}

// toolLister returns the visible MCP tool names. The local binary embeds the
// read-only tool surface, so verification reuses it directly.
type toolLister func() []mcp.ToolDefinition

// RunVerification executes the staged checks. snippet is the generated config
// (empty means generation failed). health and query may be nil when there is no
// endpoint to probe (those stages are then skipped, not failed). tools must be
// non-nil; it returns the visible tool surface. querySkipReason overrides the
// skipped first-query stage's detail: the caller passes a posture-specific
// reason (for example "OAuth is interactive" for SSO, or "set ESHU_MCP_TOKEN"
// for token posture without a personal token) so a hosted skip does not
// mislabel itself with the local-stdio default. An empty querySkipReason keeps
// that default.
func RunVerification(snippet string, tools func() []mcp.ToolDefinition, health HealthProber, query QueryProber, querySkipReason string) VerifyReport {
	var report VerifyReport

	// Stage 1: config generated.
	configOK := strings.TrimSpace(snippet) != ""
	report.Stages = append(report.Stages, StageResult{
		Stage:  StageConfigGenerated,
		OK:     configOK,
		Detail: configGeneratedDetail(configOK),
	})

	// Stage 2: client reachable.
	report.Stages = append(report.Stages, reachableStage(health))

	// Stage 3: tools visible.
	report.Stages = append(report.Stages, toolsVisibleStage(toolLister(tools)))

	// Stage 4: first query successful.
	report.Stages = append(report.Stages, firstQueryStage(query, querySkipReason))

	return report
}

func configGeneratedDetail(ok bool) string {
	if ok {
		return "platform snippet produced"
	}
	return "no snippet produced"
}

func reachableStage(health HealthProber) StageResult {
	if health == nil {
		return StageResult{Stage: StageClientReachable, Skipped: true, Detail: "no endpoint to probe (local stdio)"}
	}
	if err := health.Reachable(); err != nil {
		return StageResult{Stage: StageClientReachable, OK: false, Detail: err.Error()}
	}
	return StageResult{Stage: StageClientReachable, OK: true, Detail: "health probe responded"}
}

func toolsVisibleStage(tools toolLister) StageResult {
	if tools == nil {
		return StageResult{Stage: StageToolsVisible, OK: false, Detail: "no tool surface available"}
	}
	defs := tools()
	if len(defs) == 0 {
		return StageResult{Stage: StageToolsVisible, OK: false, Detail: "tool surface is empty"}
	}
	return StageResult{Stage: StageToolsVisible, OK: true, Detail: fmt.Sprintf("%d tools visible", len(defs))}
}

func firstQueryStage(query QueryProber, skipReason string) StageResult {
	if query == nil {
		detail := strings.TrimSpace(skipReason)
		if detail == "" {
			detail = "no endpoint to query (local stdio)"
		}
		return StageResult{Stage: StageFirstQuery, Skipped: true, Detail: detail}
	}
	if err := query.Smoke(); err != nil {
		return StageResult{Stage: StageFirstQuery, OK: false, Detail: err.Error()}
	}
	return StageResult{Stage: StageFirstQuery, OK: true, Detail: "bounded read succeeded"}
}

// RenderVerifyReport formats the staged report for the terminal.
func RenderVerifyReport(report VerifyReport) string {
	var b strings.Builder
	b.WriteString("MCP setup verification\n")
	for _, s := range report.Stages {
		marker := "[ok]"
		switch {
		case s.Skipped:
			marker = "[--]"
		case !s.OK:
			marker = "[!!]"
		}
		fmt.Fprintf(&b, "  %s %s: %s\n", marker, s.Stage, s.Detail)
	}
	return b.String()
}
