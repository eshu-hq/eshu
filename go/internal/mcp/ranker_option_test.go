// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestWithRouterRankerBuildsOverTheFinalToolListInEitherOptionOrder proves
// NewServer builds the ranker after every option, so a WithRouterRanker given
// before WithQueryProfile still indexes the profile-filtered s.tools, the
// slice find_tool passes as Request.Visible. A ranker built over the
// unfiltered list would rebuild its index on every call (#8771).
func TestWithRouterRankerBuildsOverTheFinalToolListInEitherOptionOrder(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, rankerFirst := range map[string]bool{"ranker first": true, "ranker last": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var builds int
			var built []toolcontract.ToolDefinition
			opt := WithRouterRanker(func(tools []toolcontract.ToolDefinition, _ map[string]catalog.Entry) router.Ranker {
				builds++
				built = tools
				return routerOrderRanker{}
			})
			profile := WithQueryProfile(querycontract.ProfileProduction)
			opts := []ServerOption{profile, opt}
			if rankerFirst {
				opts = []ServerOption{opt, profile}
			}
			s := NewServer(http.NewServeMux(), logger, opts...)
			if builds != 1 {
				t.Fatalf("ranker built %d times, want once", builds)
			}
			if len(built) != len(s.tools) || &built[0] != &s.tools[0] {
				t.Fatalf("ranker built over %d tools, want the profile-filtered s.tools itself (%d tools)", len(built), len(s.tools))
			}
			if len(s.tools) >= len(ReadOnlyTools()) {
				t.Fatalf("production s.tools has %d tools, want fewer than the registry %d", len(s.tools), len(ReadOnlyTools()))
			}
		})
	}
}
