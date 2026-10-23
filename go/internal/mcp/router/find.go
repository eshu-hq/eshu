// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// Request bounds for find_tool. The MCP transport has no body cap and a
// ranker may echo anchors into every candidate's example, so the handler is
// the boundary that keeps one request from inflating its response.
const (
	// MaxIntentBytes is the longest intent find_tool accepts, in bytes.
	MaxIntentBytes = 4096
	// MaxAnchorValueBytes is the longest anchor value find_tool accepts, in
	// bytes. Real repository, service, and workload ids are far shorter.
	MaxAnchorValueBytes = 256
)

// anchorKeys are the only anchor names find_tool accepts.
var anchorKeys = map[string]bool{"repo_id": true, "service_id": true, "workload_id": true, "environment": true}

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
// playbook plan. It never hides a tool the caller could call, and never
// offers a profile-hidden tool, find_tool, call_tool, ask, or a tool the API
// auth middleware always refuses for that credential. A tool the middleware
// admits only under deployment policy is offered, and the middleware may
// still refuse it at dispatch (see access.admits). Without an AuthContext it
// cannot filter and says so in the access block.
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
	if errs, truncated := ValidateArgumentsDetail(Tools()[0].InputSchema, args); len(errs) != 0 {
		return invalidArgumentsResult(FindToolName, errs, truncated), findOutcomeError
	}
	if errs, truncated := boundFindRequest(args); len(errs) != 0 {
		return invalidArgumentsResult(FindToolName, errs, truncated), findOutcomeError
	}
	q := parseQuery(args)
	if q.Intent == "" {
		return invalidArgumentsResult(FindToolName, []FieldError{{Path: "intent", Keyword: "minLength", Message: "must not be blank"}}, false), findOutcomeError
	}
	if e.ranker == nil {
		return errorResult("mcp_router_ranker_unavailable", "find_tool has no ranker configured on this server", nil), findOutcomeError
	}

	acc := accessFromContext(ctx)
	allowed := e.allowedFunc(acc, e.rankable(req.Visible))
	// Hand the ranker the caller's visible slice and the engine's entries
	// map unchanged on every call, so a ranker can key its index on them;
	// allowed carries the per-call filter.
	ranked, err := e.ranker.Rank(ctx, q, req.Visible, e.entries, allowed)
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
		example = e.examples[c.Name]
	}
	if example == nil {
		example = ExampleArguments(schema)
	}
	return candidateView{Name: c.Name, Why: c.Why, Cost: c.Cost, InputSchema: schema, Example: example, Score: c.Score}
}

// boundFindRequest enforces MaxIntentBytes, MaxAnchorValueBytes, and the
// closed anchor key set on a request that already passed schema validation.
func boundFindRequest(args map[string]any) ([]FieldError, bool) {
	errs := &errorSink{}
	if intent, _ := args["intent"].(string); len(intent) > MaxIntentBytes {
		errs.add(FieldError{Path: "intent", Keyword: "maxLength", Message: fmt.Sprintf("must be at most %d bytes", MaxIntentBytes)})
	}
	anchors, _ := asObject(args["anchors"])
	keys := make([]string, 0, len(anchors))
	for key := range anchors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if errs.full() {
			break
		}
		if !anchorKeys[key] {
			errs.add(FieldError{Path: "anchors." + key, Keyword: "additionalProperties", Message: "is not an anchor; use repo_id, service_id, workload_id, or environment"})
			continue
		}
		if value, _ := anchors[key].(string); len(value) > MaxAnchorValueBytes {
			errs.add(FieldError{Path: "anchors." + key, Keyword: "maxLength", Message: fmt.Sprintf("must be at most %d bytes", MaxAnchorValueBytes)})
		}
	}
	return errs.result()
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
func invalidArgumentsResult(tool string, errs []FieldError, truncated bool) ToolResult {
	return errorResult("mcp_router_invalid_arguments", "arguments for "+tool+" do not match its input schema",
		map[string]any{"tool": tool, "field_errors": errs, "field_errors_truncated": truncated})
}
