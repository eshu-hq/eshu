// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// TestServiceIDAloneSatisfiesRegisteredSchemas guards the follow-up an
// ambiguous service answer advises: calling the same tool again with only
// service_id set to a candidate's next_selector value. core_router call
// validates the target's registered input schema before dispatch, so a schema
// that still marks service_name required rejects that follow-up even though
// the route accepts it. The test validates the real registered schemas, not a
// copy, with the same validator router/call.go uses.
func TestServiceIDAloneSatisfiesRegisteredSchemas(t *testing.T) {
	t.Parallel()

	const serviceID = "workload:repository:r_sample:checkout-service"
	registered := make(map[string]ToolDefinition)
	for _, tool := range ReadOnlyTools() {
		registered[tool.Name] = tool
	}
	for _, name := range []string{"get_service_story", "get_service_intelligence_report", "investigate_service"} {
		tool, ok := registered[name]
		if !ok {
			t.Fatalf("%s is not a registered tool", name)
		}
		args := map[string]any{"service_id": serviceID}
		if errs := router.ValidateArguments(tool.InputSchema, args); len(errs) != 0 {
			t.Errorf("%s rejects %v on the router path: %v", name, args, errs)
		}
		if _, err := resolveRoute(name, args); err != nil {
			t.Errorf("%s builds no route for %v: %v", name, args, err)
		}
	}
}

// TestInvestigateServiceRouteStillRequiresASelector keeps the selector promise
// the schema can no longer express: with neither service_id nor service_name
// the route refuses the call before any HTTP request is built.
func TestInvestigateServiceRouteStillRequiresASelector(t *testing.T) {
	t.Parallel()

	if _, err := resolveRoute("investigate_service", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(investigate_service, {}) error = nil, want a missing-selector error")
	}
}
