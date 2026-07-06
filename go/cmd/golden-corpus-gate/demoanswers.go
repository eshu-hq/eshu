// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/demospec"
)

// checkDemoAnswers executes every specs/demo-first-answers.v1.yaml question
// live against the running API/MCP stack and asserts the answer is populated.
// It closes the value-level gap the generic query shapes leave open (#4776):
// checkQuery/checkMCPQuery assert each tool/route with the snapshot's generic
// example arguments, but the demo oracle pins SPECIFIC arguments (for example
// package_id github.com/acme/lib-common, or workload:api-svc) whose answers
// nothing else asserts. Without this phase a demo answer could silently
// regress to empty while the referential-integrity test and the generic query
// shapes both stay green. Each question is a required finding "demo:<id>".
func checkDemoAnswers(ctx context.Context, qc *queryClient, mc *mcpClient, manifestPath string, r *Report) error {
	m, err := demospec.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load demo-first-answers manifest %s: %w", manifestPath, err)
	}
	assertDemoAnswers(ctx, qc, mc, m.Questions, r)
	return nil
}

// assertDemoAnswers executes each question and records a required "demo:<id>"
// finding. It is split from checkDemoAnswers (which owns manifest loading) so
// the execute-and-assert loop is unit-testable with in-code questions.
func assertDemoAnswers(ctx context.Context, qc *queryClient, mc *mcpClient, questions []demospec.Question, r *Report) {
	for _, q := range questions {
		body, err := executeDemoQuestion(ctx, qc, mc, q)
		if err != nil {
			r.AddCheck("query", "demo:"+q.ID, false, true, err.Error())
			continue
		}
		r.Add(EvaluateQueryShape("demo:"+q.ID, demoQueryShape(q), body))
	}
}

// demoQueryShape builds the assertion shape for a question from its
// expected_answer: the required top-level fields, the dotted JSON paths, and
// the minimum result count. It reuses the same EvaluateQueryShape contract the
// snapshot query shapes use, so a demo answer is held to the same bar.
func demoQueryShape(q demospec.Question) QueryShape {
	return QueryShape{
		RequiredResponseFields: q.ExpectedAnswer.RequiredResponseFields,
		RequiredJSONPaths:      q.ExpectedAnswer.RequiredJSONPaths,
		MinimumResults:         q.ExpectedAnswer.MinimumResults,
	}
}

// executeDemoQuestion fetches the live answer for a question. It calls the
// surface.execute target when set (a playbook id is not directly callable, so
// the manifest names the underlying mcp tool or http route) and otherwise the
// surface itself, which must be a directly-callable mcp tool or http route.
func executeDemoQuestion(ctx context.Context, qc *queryClient, mc *mcpClient, q demospec.Question) ([]byte, error) {
	kind, ref, args := q.Surface.Kind, q.Surface.Ref, q.Surface.Arguments
	if ex := q.Surface.Execute; ex != nil {
		kind, ref, args = ex.Kind, ex.Ref, ex.Arguments
	}
	switch kind {
	case demospec.SurfaceKindMCP:
		body, err := mc.callTool(ctx, ref, args, false)
		if err != nil {
			return nil, fmt.Errorf("question %s: mcp %s: %w", q.ID, ref, err)
		}
		return body, nil
	case demospec.SurfaceKindHTTP:
		method, path, err := parseHTTPShapeKey(ref)
		if err != nil {
			return nil, fmt.Errorf("question %s: %w", q.ID, err)
		}
		status, body, err := qc.request(ctx, method, path, QueryShape{})
		if err != nil {
			return nil, fmt.Errorf("question %s: %s %s: %w", q.ID, method, path, err)
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("question %s: HTTP %d from %s %s", q.ID, status, method, path)
		}
		return body, nil
	default:
		return nil, fmt.Errorf(
			"question %s: surface kind %q is not gate-executable; a playbook or cli question needs a surface.execute mcp/http target",
			q.ID, kind,
		)
	}
}
