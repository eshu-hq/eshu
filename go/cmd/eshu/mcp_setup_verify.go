// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// verifyStage names one independently-reported verification step.
type verifyStage string

const (
	// stageConfigGenerated proves a valid platform snippet was produced.
	stageConfigGenerated verifyStage = "config generated"
	// stageClientReachable proves the configured endpoint answers a health probe.
	stageClientReachable verifyStage = "client reachable"
	// stageToolsVisible proves the MCP tool surface is non-empty.
	stageToolsVisible verifyStage = "tools visible"
	// stageFirstQuery proves one bounded read query succeeds end to end.
	stageFirstQuery verifyStage = "first query successful"
)

// stageResult is the outcome of one verification stage.
type stageResult struct {
	// Stage is the stage name.
	Stage verifyStage
	// OK reports success.
	OK bool
	// Detail is a short human explanation (endpoint, count, or failure cause).
	Detail string
	// Skipped marks a stage that did not run (for example, no endpoint to probe).
	Skipped bool
}

// verifyReport is the ordered set of stage results.
type verifyReport struct {
	// Stages holds each stage outcome in execution order.
	Stages []stageResult
}

// allOK reports whether every non-skipped stage passed.
func (r verifyReport) allOK() bool {
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

// healthProber probes endpoint reachability. It is an interface so tests can
// inject deterministic reachability without a live server.
type healthProber interface {
	// Reachable returns nil when the endpoint answers a health probe.
	Reachable() error
}

// queryProber runs one bounded smoke query. It is an interface so tests can
// inject success or failure without a live backend.
type queryProber interface {
	// Smoke runs one bounded read and returns nil on success.
	Smoke() error
}

// apiHealthProber probes the hosted API /health endpoint via the shared client.
type apiHealthProber struct {
	client *APIClient
}

// Reachable issues a GET /health and treats any non-error response as reachable.
func (p apiHealthProber) Reachable() error {
	if err := p.client.Get("/health", nil); err != nil {
		return fmt.Errorf("health probe %s failed: %w", p.client.BaseURL, err)
	}
	return nil
}

// apiQueryProber runs one bounded index-status read as the first-query smoke.
type apiQueryProber struct {
	client *APIClient
}

// Smoke fetches index status, a cheap bounded read that proves the query surface
// answers without fetching a large payload.
func (p apiQueryProber) Smoke() error {
	var result map[string]any
	if err := p.client.Get("/api/v0/index-status", &result); err != nil {
		return fmt.Errorf("index-status query failed: %w", err)
	}
	return nil
}

// toolLister returns the visible MCP tool names. The local binary embeds the
// read-only tool surface, so verification reuses it directly.
type toolLister func() []mcp.ToolDefinition

// runVerification executes the staged checks. snippet is the generated config
// (empty means generation failed). health and query may be nil when there is no
// endpoint to probe (those stages are then skipped, not failed). tools must be
// non-nil; it returns the visible tool surface.
func runVerification(snippet string, tools toolLister, health healthProber, query queryProber) verifyReport {
	var report verifyReport

	// Stage 1: config generated.
	configOK := strings.TrimSpace(snippet) != ""
	report.Stages = append(report.Stages, stageResult{
		Stage:  stageConfigGenerated,
		OK:     configOK,
		Detail: configGeneratedDetail(configOK),
	})

	// Stage 2: client reachable.
	report.Stages = append(report.Stages, reachableStage(health))

	// Stage 3: tools visible.
	report.Stages = append(report.Stages, toolsVisibleStage(tools))

	// Stage 4: first query successful.
	report.Stages = append(report.Stages, firstQueryStage(query))

	return report
}

func configGeneratedDetail(ok bool) string {
	if ok {
		return "platform snippet produced"
	}
	return "no snippet produced"
}

func reachableStage(health healthProber) stageResult {
	if health == nil {
		return stageResult{Stage: stageClientReachable, Skipped: true, Detail: "no endpoint to probe (local stdio)"}
	}
	if err := health.Reachable(); err != nil {
		return stageResult{Stage: stageClientReachable, OK: false, Detail: err.Error()}
	}
	return stageResult{Stage: stageClientReachable, OK: true, Detail: "health probe responded"}
}

func toolsVisibleStage(tools toolLister) stageResult {
	if tools == nil {
		return stageResult{Stage: stageToolsVisible, OK: false, Detail: "no tool surface available"}
	}
	defs := tools()
	if len(defs) == 0 {
		return stageResult{Stage: stageToolsVisible, OK: false, Detail: "tool surface is empty"}
	}
	return stageResult{Stage: stageToolsVisible, OK: true, Detail: fmt.Sprintf("%d tools visible", len(defs))}
}

func firstQueryStage(query queryProber) stageResult {
	if query == nil {
		return stageResult{Stage: stageFirstQuery, Skipped: true, Detail: "no endpoint to query (local stdio)"}
	}
	if err := query.Smoke(); err != nil {
		return stageResult{Stage: stageFirstQuery, OK: false, Detail: err.Error()}
	}
	return stageResult{Stage: stageFirstQuery, OK: true, Detail: "bounded read succeeded"}
}

// renderVerifyReport formats the staged report for the terminal.
func renderVerifyReport(report verifyReport) string {
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
