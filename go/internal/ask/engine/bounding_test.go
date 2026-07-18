// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestBoundToolCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		tool        string
		args        map[string]any
		wantRefused bool
	}{
		{"unbounded inventory refused", "list_indexed_repositories", map[string]any{}, true},
		{"inventory with limit allowed", "list_indexed_repositories", map[string]any{"limit": 25}, false},
		{"inventory with zero limit refused", "list_indexed_repositories", map[string]any{"limit": 0}, true},
		{"inventory with float limit allowed", "list_indexed_repositories", map[string]any{"limit": float64(10)}, false},
		{"inventory with json.Number limit allowed", "list_indexed_repositories", map[string]any{"limit": json.Number("5")}, false},
		{"unbounded edges refused", "list_relationship_edges", map[string]any{}, true},
		{"edges with scope allowed", "list_relationship_edges", map[string]any{"repo_id": "r1"}, false},
		{"edges with blank scope refused", "list_relationship_edges", map[string]any{"repo_id": "  "}, true},
		{"edges with source_tool allowed", "list_relationship_edges", map[string]any{"source_tool": "terraform"}, false},
		{"non-broad tool never refused", "find_code", map[string]any{}, false},
		{"scoped story never refused", "get_service_story", map[string]any{"service": "payments"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hint, refused := boundToolCall(tc.tool, tc.args)
			if refused != tc.wantRefused {
				t.Fatalf("boundToolCall(%q, %v) refused = %v, want %v", tc.tool, tc.args, refused, tc.wantRefused)
			}
			if refused && strings.TrimSpace(hint) == "" {
				t.Errorf("refused call returned an empty narrowing hint")
			}
			if refused && !strings.Contains(hint, "limit") {
				t.Errorf("hint %q is not executable (missing a limit instruction)", hint)
			}
		})
	}
}

func TestOversizedContinuationPacket(t *testing.T) {
	t.Parallel()

	overBudget := &query.ResponseEnvelope{Error: &query.ErrorEnvelope{
		Code:    query.ErrorCode(errCodeResponseOverBudget),
		Details: map[string]any{"guidance": "lower limit and add repo_id"},
	}}
	timeout := &query.ResponseEnvelope{Error: &query.ErrorEnvelope{
		Code: query.ErrorCode(errCodeDispatchTimeout),
	}}
	otherErr := &query.ResponseEnvelope{Error: &query.ErrorEnvelope{
		Code: query.ErrorCode("unsupported_capability"),
	}}

	t.Run("over budget becomes bounded continuation", func(t *testing.T) {
		t.Parallel()
		pkt, ok := oversizedContinuationPacket("q", "get_service_story", overBudget)
		if !ok {
			t.Fatal("over-budget envelope did not yield a continuation packet")
		}
		if pkt.Supported || !pkt.Partial {
			t.Errorf("continuation Supported=%v Partial=%v, want false/true", pkt.Supported, pkt.Partial)
		}
		if len(pkt.RecommendedNextCalls) == 0 || len(pkt.UnsupportedReasons) == 0 {
			t.Errorf("continuation missing next call or reason: %+v", pkt)
		}
		if len(pkt.Limitations) == 0 || !strings.Contains(pkt.Limitations[0], "lower limit") {
			t.Errorf("continuation did not surface the dispatch guidance: %v", pkt.Limitations)
		}
	})

	t.Run("timeout uses default guidance", func(t *testing.T) {
		t.Parallel()
		pkt, ok := oversizedContinuationPacket("q", "find_code", timeout)
		if !ok {
			t.Fatal("timeout envelope did not yield a continuation packet")
		}
		if len(pkt.Limitations) == 0 || !strings.Contains(pkt.Limitations[0], "limit") {
			t.Errorf("timeout continuation missing default narrowing guidance: %v", pkt.Limitations)
		}
	})

	t.Run("ordinary error keeps unsupported path", func(t *testing.T) {
		t.Parallel()
		if _, ok := oversizedContinuationPacket("q", "x", otherErr); ok {
			t.Error("ordinary error envelope must not yield a continuation packet")
		}
		if _, ok := oversizedContinuationPacket("q", "x", nil); ok {
			t.Error("nil envelope must not yield a continuation packet")
		}
	})
}

func TestEvidenceProgress(t *testing.T) {
	t.Parallel()

	evidence := func(tool, summary string) query.AnswerPacket {
		return query.AnswerPacket{PrimaryTool: tool, Summary: summary, Supported: true}
	}
	continuation := query.AnswerPacket{PrimaryTool: "get_service_story", Partial: true}

	var p evidenceProgress

	// Only a continuation packet: no answer evidence yet.
	if made, have := p.observe([]query.AnswerPacket{continuation}); made || have {
		t.Fatalf("continuation-only: made=%v have=%v, want false/false", made, have)
	}
	// A new supported summary packet: progress, evidence now held.
	pkts := []query.AnswerPacket{continuation, evidence("get_service_story", "payments overview")}
	if made, have := p.observe(pkts); !made || !have {
		t.Fatalf("first evidence: made=%v have=%v, want true/true", made, have)
	}
	// A redundant call to the same tool: no new progress, evidence still held.
	pkts = append(pkts, evidence("get_service_story", "payments overview again"))
	if made, have := p.observe(pkts); made || !have {
		t.Fatalf("redundant tool: made=%v have=%v, want false/true", made, have)
	}
	// A new distinct tool: progress again.
	pkts = append(pkts, evidence("get_repository_summary", "repo summary"))
	if made, have := p.observe(pkts); !made || !have {
		t.Fatalf("new distinct tool: made=%v have=%v, want true/true", made, have)
	}
}
