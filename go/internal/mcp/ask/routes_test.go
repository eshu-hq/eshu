// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package asktools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

func TestRoutePreservesAskRequestContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args routecontract.Arguments
		body map[string]any
	}{
		{
			name: "provided strings",
			args: routecontract.Arguments{
				"question": "what is the deployment story for service X?",
				"format":   "markdown",
			},
			body: map[string]any{
				"question": "what is the deployment story for service X?",
				"format":   "markdown",
			},
		},
		{
			name: "missing arguments",
			args: routecontract.Arguments{},
			body: map[string]any{"question": "", "format": ""},
		},
		{
			name: "wrong argument types",
			args: routecontract.Arguments{"question": 42, "format": true},
			body: map[string]any{"question": "", "format": ""},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request, handled := Route("ask", test.args)
			if !handled {
				t.Fatal("Route(ask) handled = false, want true")
			}
			want := routecontract.Request{
				Method: "POST",
				Path:   "/api/v0/ask",
				Body:   test.body,
			}
			if !reflect.DeepEqual(request, want) {
				t.Fatalf("Route(ask) request = %#v, want %#v", request, want)
			}
		})
	}
}

func TestRouteRejectsUnrelatedTool(t *testing.T) {
	t.Parallel()

	request, handled := Route("search", routecontract.Arguments{
		"question": "must not leak into another route",
	})
	if handled {
		t.Fatal("Route(search) handled = true, want false")
	}
	if !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("Route(search) request = %#v, want zero value", request)
	}
}
