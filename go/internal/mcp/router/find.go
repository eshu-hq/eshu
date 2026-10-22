// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// candidateView is the wire form of one find_tool candidate.
type candidateView struct {
	Name        string            `json:"name"`
	Why         string            `json:"why"`
	Cost        catalog.CostClass `json:"cost"`
	InputSchema any               `json:"inputSchema"`
	Example     map[string]any    `json:"example"`
	Score       float64           `json:"score"`
}

// findResponse is the find_tool structured result.
type findResponse struct {
	Candidates []candidateView `json:"candidates"`
	Playbook   *playbookPlan   `json:"playbook,omitempty"`
	Access     accessView      `json:"access"`
}

// Find serves find_tool: it validates the request, ranks the tools the
// caller's credential and the query profile allow, and attaches a matching
// playbook plan. It never returns a tool the caller cannot call, a
// profile-hidden tool, find_tool, call_tool, or ask. Without an AuthContext
// it cannot filter and says so in the access block.
func (e *Engine) Find(ctx context.Context, req Request, args map[string]any) ToolResult {
	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, contract.SpanMCPRouterFind)
	defer span.End()

	result, outcome := e.find(ctx, req, args)
	span.SetAttributes(attribute.String("outcome", outcome))
	if outcome == findOutcomeError {
		span.SetStatus(codes.Error, "find_tool failed")
	}
	recordFind(ctx, outcome)
	return result
}

func (e *Engine) find(ctx context.Context, req Request, args map[string]any) (ToolResult, string) {
	if errs := ValidateArguments(Tools()[0].InputSchema, args); len(errs) != 0 {
		return invalidArgumentsResult(FindToolName, errs), findOutcomeError
	}
	q := parseQuery(args)
	if q.Intent == "" {
		return invalidArgumentsResult(FindToolName, []FieldError{{Path: "intent", Keyword: "minLength", Message: "must not be blank"}}), findOutcomeError
	}
	if e.ranker == nil {
		return errorResult("mcp_router_ranker_unavailable", "find_tool has no ranker configured on this server", nil), findOutcomeError
	}

	acc := accessFromContext(ctx)
	visible := e.rankable(req.Visible)
	allowed := e.allowedFunc(acc, visible)
	tools := make([]toolcontract.ToolDefinition, 0, len(visible))
	for _, tool := range req.Visible {
		if allowed(tool.Name) {
			tools = append(tools, tool)
		}
	}
	ranked, err := e.ranker.Rank(ctx, q, tools, e.entries, allowed)
	if err != nil {
		e.logger.Warn("mcp router: ranker failed", "error", err)
		return errorResult("mcp_router_rank_failed", "find_tool could not rank tools: "+err.Error(), nil), findOutcomeError
	}

	resp := findResponse{Candidates: make([]candidateView, 0, len(ranked)), Access: acc.view()}
	offered := make([]string, 0, len(ranked))
	for _, c := range ranked {
		// Re-check the ranker's output: a ranker that ignores allowed must
		// still not leak a tool, and the limit holds whatever it returns.
		if !allowed(c.Name) || len(resp.Candidates) >= q.Limit {
			continue
		}
		resp.Candidates = append(resp.Candidates, e.view(c))
		offered = append(offered, c.Name)
	}
	resp.Playbook = matchPlaybook(q, allowed)
	e.recent.remember(req.Session, offered)

	if len(resp.Candidates) == 0 {
		return jsonResult(resp, false), findOutcomeEmpty
	}
	return jsonResult(resp, false), findOutcomeCandidates
}

// rankable is the visible set as a name lookup.
func (e *Engine) rankable(visible []toolcontract.ToolDefinition) map[string]bool {
	names := make(map[string]bool, len(visible))
	for _, tool := range visible {
		names[tool.Name] = true
	}
	return names
}

// allowedFunc builds the per-call filter: visible under the query profile,
// registered, not a refused target, and admitted for the credential.
func (e *Engine) allowedFunc(acc access, visible map[string]bool) func(string) bool {
	return func(name string) bool {
		if refusedTargets[name] || !visible[name] {
			return false
		}
		class, registered := e.routes[name]
		return registered && acc.admits(class)
	}
}

func (e *Engine) view(c Candidate) candidateView {
	schema := any(c.InputSchema)
	if c.InputSchema == nil {
		schema = e.registry[c.Name].InputSchema
	}
	example := c.Example
	if example == nil {
		example = ExampleArguments(schema)
	}
	return candidateView{Name: c.Name, Why: c.Why, Cost: c.Cost, InputSchema: schema, Example: example, Score: c.Score}
}

// parseQuery reads a find_tool request that already passed validation.
func parseQuery(args map[string]any) Query {
	q := Query{Limit: DefaultLimit}
	q.Intent, _ = args["intent"].(string)
	q.Intent = strings.TrimSpace(q.Intent)
	if limit, ok := asNumber(args["limit"]); ok {
		q.Limit = int(limit)
	}
	if anchors, ok := asObject(args["anchors"]); ok {
		q.Anchors.RepoID, _ = anchors["repo_id"].(string)
		q.Anchors.ServiceID, _ = anchors["service_id"].(string)
		q.Anchors.WorkloadID, _ = anchors["workload_id"].(string)
		q.Anchors.Environment, _ = anchors["environment"].(string)
	}
	return q
}

// invalidArgumentsResult reports field errors for a router tool's own
// arguments or a call_tool target's arguments.
func invalidArgumentsResult(tool string, errs []FieldError) ToolResult {
	return errorResult("mcp_router_invalid_arguments", "arguments for "+tool+" do not match its input schema",
		map[string]any{"tool": tool, "field_errors": errs})
}
