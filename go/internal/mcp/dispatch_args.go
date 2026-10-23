// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"strings"
	"sync"

	routecontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

func stringSlice(args map[string]any, key string) []any {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if ok {
		return values
	}
	stringValues, ok := raw.([]string)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(stringValues))
	for _, value := range stringValues {
		result = append(result, value)
	}
	return result
}

func objectSlice(args map[string]any, key string) []any {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(values))
	for _, value := range values {
		if _, ok := value.(map[string]any); ok {
			result = append(result, value)
		}
	}
	return result
}

// dispatchStatusSucceeded reports whether an internal handler status counts as
// an answered tool call: only 2xx does. A 3xx is never success. No MCP-reachable
// route redirects on purpose (the only http.Redirect calls in internal/query
// are the OIDC, SAML, and GitHub browser login handlers, which no tool routes to), so a
// redirect means the request did not reach its handler (#8772).
func dispatchStatusSucceeded(status int) bool {
	return status >= 200 && status < 300
}

// rejectEmptyPathSegment refuses a resolved route whose path has an empty
// segment ("//"). That shape comes from a required id argument left empty in a
// path template; the mux would answer it with a 307 redirect instead of
// running the handler. The error names the tool's required arguments that are
// empty, in the same "<field> is required" form as the other route-level
// missing-argument errors.
func rejectEmptyPathSegment(toolName string, args map[string]any, route *routecontract.Request) error {
	if route == nil || !strings.Contains(route.Path, "//") {
		return nil
	}
	missing := emptyRequiredArguments(toolName, args)
	if len(missing) == 0 {
		return fmt.Errorf("%s: a required path argument is empty", toolName)
	}
	return fmt.Errorf("%s is required", strings.Join(missing, ", "))
}

// requiredArgumentsByTool maps each registered tool to its schema-required
// argument names. ReadOnlyTools rebuilds the whole catalog on every call
// (about 530 KB), so the map is built once, on first use.
var requiredArgumentsByTool = sync.OnceValue(func() map[string][]string {
	byTool := make(map[string][]string)
	for _, tool := range ReadOnlyTools() {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			continue
		}
		switch values := schema["required"].(type) {
		case []string:
			byTool[tool.Name] = values
		case []any:
			for _, value := range values {
				if name, ok := value.(string); ok {
					byTool[tool.Name] = append(byTool[tool.Name], name)
				}
			}
		}
	}
	return byTool
})

// emptyRequiredArguments returns the schema-required string arguments of a
// tool that are absent or empty in args, in schema order.
func emptyRequiredArguments(toolName string, args map[string]any) []string {
	var missing []string
	for _, name := range requiredArgumentsByTool()[toolName] {
		if str(args, name) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}
