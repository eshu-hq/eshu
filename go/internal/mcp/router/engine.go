// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"encoding/json"
	"log/slog"
	"maps"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// refusedTargets are the names call_tool never dispatches and find_tool never
// offers: the router tools themselves (recursion) and ask, which re-enters the
// Ask engine and could call back into the router.
var refusedTargets = map[string]bool{FindToolName: true, CallToolName: true, "ask": true}

// Config wires an Engine to one MCP server.
type Config struct {
	// Registry is every registered tool (mcp.ReadOnlyTools). call_tool looks
	// targets up here and validates against their schemas, so a tool the
	// query profile hides from tools/list stays callable, exactly like a
	// direct tools/call.
	Registry []toolcontract.ToolDefinition
	// Resolve maps a tool call to the HTTP route dispatch would send. The
	// engine classifies that route to decide what a credential may call.
	Resolve RouteResolver
	// Ranker orders candidates for find_tool. Nil makes find_tool answer
	// mcp_router_ranker_unavailable instead of inventing an order.
	Ranker Ranker
	// Entries maps tool names to ask-catalog entries for the ranker. Nil loads
	// them from the embedded surface inventory.
	Entries map[string]catalog.Entry
	// Logger receives router decisions. Nil uses slog.Default.
	Logger *slog.Logger
}

// Engine serves find_tool and call_tool for one MCP server. It is safe for
// concurrent use; per-request state travels in Request.
type Engine struct {
	registry map[string]toolcontract.ToolDefinition
	names    []string
	routes   map[string]routeClass
	examples map[string]map[string]any
	resolve  RouteResolver
	ranker   Ranker
	entries  map[string]catalog.Entry
	logger   *slog.Logger
	recent   *recentFinds
}

// Request carries the per-call inputs the MCP root owns.
type Request struct {
	// Visible is the tool list the server's query profile exposes. find_tool
	// ranks only these tools.
	Visible []toolcontract.ToolDefinition
	// Session keys the caller's last find_tool result for the target-rank
	// histogram. The root passes the credential's principal key; an empty
	// key (stdio or an unauthenticated transport) is one shared session.
	Session string
	// Dispatch runs a registered tool through the same path as tools/call.
	Dispatch Dispatcher
}

// NewEngine builds an Engine and classifies every registered tool's route
// once, so per-call access checks are map lookups.
func NewEngine(cfg Config) *Engine {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	e := &Engine{
		registry: make(map[string]toolcontract.ToolDefinition, len(cfg.Registry)),
		routes:   make(map[string]routeClass, len(cfg.Registry)),
		examples: make(map[string]map[string]any, len(cfg.Registry)),
		resolve:  cfg.Resolve,
		ranker:   cfg.Ranker,
		entries:  cfg.Entries,
		logger:   logger,
		recent:   newRecentFinds(recentFindsCapacity),
	}
	for _, tool := range cfg.Registry {
		e.registry[tool.Name] = tool
		e.names = append(e.names, tool.Name)
		e.routes[tool.Name] = classifyTool(cfg.Resolve, tool)
		e.examples[tool.Name] = callableExample(cfg.Resolve, tool)
	}
	if e.entries == nil {
		e.entries = loadCatalogEntries(logger)
	}
	return e
}

// Example returns a copy of the example arguments the engine hands back for
// a registered tool: the required-only ExampleArguments, plus the first
// optional property (in name order) that lets the route resolve when the
// tool needs one of several selectors its schema cannot mark required.
func (e *Engine) Example(name string) (map[string]any, bool) {
	example, ok := e.examples[name]
	if !ok {
		return nil, false
	}
	return maps.Clone(example), true
}

// callableExample builds a tool's example and, when the required-only one
// does not resolve to a route, adds one optional placeholder at a time.
func callableExample(resolve RouteResolver, tool toolcontract.ToolDefinition) map[string]any {
	base := ExampleArguments(tool.InputSchema)
	if resolve == nil || resolves(resolve, tool.Name, base) {
		return base
	}
	all := placeholderArguments(tool.InputSchema, true)
	names := make([]string, 0, len(all))
	for name := range all {
		if _, ok := base[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		candidate := maps.Clone(base)
		candidate[name] = all[name]
		if resolves(resolve, tool.Name, candidate) {
			return candidate
		}
	}
	return base
}

func resolves(resolve RouteResolver, name string, args map[string]any) bool {
	route, err := resolve(name, args)
	return err == nil && route != nil && route.Path != ""
}

// loadCatalogEntries annotates the embedded surface inventory and keeps the
// MCP tool entries. A failure leaves the map empty: the ranker still sees
// every tool, only without curated intents or cost classes.
func loadCatalogEntries(logger *slog.Logger) map[string]catalog.Entry {
	entries := map[string]catalog.Entry{}
	cat, err := catalog.Parse(capabilitycatalog.RawSurfaceArtifact())
	if err != nil {
		logger.Warn("mcp router: surface inventory unreadable, ranking without catalog entries", "error", err)
		return entries
	}
	cat.Annotate()
	for _, entry := range cat.Entries() {
		if entry.Kind == catalog.KindMCPTool {
			entries[entry.Name] = entry
		}
	}
	return entries
}

// ToolResult is the MCP tools/call result the router builds itself. Its JSON
// matches the root's tool-result shape field for field.
type ToolResult struct {
	// Content holds the text rendering of the result.
	Content []Content `json:"content"`
	// StructuredContent holds the machine-readable result.
	StructuredContent any `json:"structuredContent,omitempty"`
	// IsError marks a tool-level failure.
	IsError bool `json:"isError,omitempty"`
}

// Content is one MCP content block.
type Content struct {
	// Type is the MCP content type; the router only emits "text".
	Type string `json:"type"`
	// Text is the rendered content.
	Text string `json:"text,omitempty"`
}

// CallOutcome is the result of one call_tool request. When Dispatched is
// true the root renders Value or Err exactly as it renders a direct
// tools/call to Target, so the two are byte-identical. Otherwise Result holds
// the router's own error result.
type CallOutcome struct {
	// Target is the requested tool name, empty when the request had none.
	Target string
	// Dispatched reports whether the Dispatcher ran.
	Dispatched bool
	// Value is the Dispatcher's result, untouched.
	Value any
	// Err is the Dispatcher's error, untouched.
	Err error
	// Result is the router-built error result when Dispatched is false.
	Result ToolResult
}

// jsonResult renders body as a tool result whose text is the compact JSON of
// the structured content, for clients that read only text.
func jsonResult(body any, isError bool) ToolResult {
	text, err := json.Marshal(body)
	if err != nil {
		text = []byte(`{"error":"unrenderable router result"}`)
	}
	return ToolResult{
		Content:           []Content{{Type: "text", Text: string(text)}},
		StructuredContent: body,
		IsError:           isError,
	}
}

// errorResult builds the router's {data, truth, error} error envelope, the
// same shape the dispatch timeout error uses.
func errorResult(code, message string, details map[string]any) ToolResult {
	return jsonResult(map[string]any{
		"data":  nil,
		"truth": nil,
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"capability": "mcp.router",
			"details":    details,
		},
	}, true)
}
