package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestAskEmbeddedAnswerPacketPreserved (FINDING 1): when the runner returns an
// envelope whose Data contains an "answer_packet" key with a rich payload, the
// engine must use that embedded packet instead of building a bare NewAnswerPacket.
// Asserts that ans.Packets[0].Summary equals the embedded summary "S" and
// Supported is true — proving the embedded packet was used, not a bare one.
func TestAskEmbeddedAnswerPacketPreserved(t *testing.T) {
	t.Parallel()

	// Build an envelope whose Data embeds an answer_packet with Summary "S"
	// and Supported true (truth class deterministic, no error).
	embeddedData := map[string]any{
		"answer_packet": map[string]any{
			"summary":     "S",
			"supported":   true,
			"truth_class": "deterministic",
			"partial":     false,
		},
	}
	env := &query.ResponseEnvelope{
		Data: embeddedData,
		Truth: &query.TruthEnvelope{
			Level: query.TruthLevelExact,
			Basis: query.TruthBasisAuthoritativeGraph,
		},
	}

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "e1", Name: "service_story", Arguments: map[string]any{}},
		},
	}
	turn2 := provider.Completion{Text: "done"}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	runner := &recordingRunner{env: env}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "tell me about the service")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if len(ans.Packets) != 1 {
		t.Fatalf("len(Packets) = %d, want 1", len(ans.Packets))
	}
	got := ans.Packets[0].Summary
	if got != "S" {
		t.Errorf("Packets[0].Summary = %q, want %q (embedded packet not used)", got, "S")
	}
	if !ans.Packets[0].Supported {
		t.Error("Packets[0].Supported = false, want true (embedded packet not used)")
	}
}

// TestExtractEmbeddedPacket exercises extractEmbeddedPacket directly to cover
// the fallback and error paths independently of the Ask loop.
func TestExtractEmbeddedPacket(t *testing.T) {
	t.Parallel()

	t.Run("nil envelope returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := extractEmbeddedPacket(nil)
		if ok {
			t.Error("extractEmbeddedPacket(nil) = true, want false")
		}
	})

	t.Run("no answer_packet key returns false", func(t *testing.T) {
		t.Parallel()
		env := &query.ResponseEnvelope{Data: map[string]any{"other": "value"}}
		_, ok := extractEmbeddedPacket(env)
		if ok {
			t.Error("extractEmbeddedPacket without answer_packet key = true, want false")
		}
	})

	t.Run("non-object Data returns false", func(t *testing.T) {
		t.Parallel()
		env := &query.ResponseEnvelope{Data: "not an object"}
		_, ok := extractEmbeddedPacket(env)
		if ok {
			t.Error("extractEmbeddedPacket with non-object Data = true, want false")
		}
	})

	t.Run("valid embedded packet is decoded", func(t *testing.T) {
		t.Parallel()
		env := &query.ResponseEnvelope{
			Data: map[string]any{
				"answer_packet": map[string]any{
					"summary":   "decoded",
					"supported": true,
				},
			},
		}
		pkt, ok := extractEmbeddedPacket(env)
		if !ok {
			t.Fatal("extractEmbeddedPacket = false, want true for valid embedded packet")
		}
		if pkt.Summary != "decoded" {
			t.Errorf("pkt.Summary = %q, want %q", pkt.Summary, "decoded")
		}
		if !pkt.Supported {
			t.Error("pkt.Supported = false, want true")
		}
	})
}

// TestAskPlainJSONToolResultFedToAdapter (FINDING 2): when the runner returns
// RunResult{Value: map[...]} with a nil Envelope, the engine must feed the
// plain JSON to the adapter's next Complete call in a RoleTool message, record
// the trace entry as Supported=true, and NOT append an AnswerPacket.
func TestAskPlainJSONToolResultFedToAdapter(t *testing.T) {
	t.Parallel()

	plainValue := map[string]any{
		"collectors": []string{"pagerduty", "github"},
	}

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "p1", Name: "list_collectors", Arguments: map[string]any{}},
		},
	}
	turn2 := provider.Completion{Text: "here are your collectors"}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	// Nil envelope, non-nil value: plain JSON path.
	runner := &recordingRunner{env: nil, value: plainValue}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "list my collectors")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	// No AnswerPacket must be appended for a plain-JSON result.
	if len(ans.Packets) != 0 {
		t.Errorf("len(Packets) = %d, want 0 for plain-JSON result", len(ans.Packets))
	}

	// TraceEntry must be Supported=true.
	if len(ans.Trace) != 1 {
		t.Fatalf("len(Trace) = %d, want 1", len(ans.Trace))
	}
	if !ans.Trace[0].Supported {
		t.Error("Trace[0].Supported = false, want true for plain-JSON result")
	}

	// The second adapter call must have received a RoleTool message containing
	// the collectors data.
	if adapter.calls < 2 {
		t.Fatalf("adapter.calls = %d, want >= 2", adapter.calls)
	}
	msgs := adapter.received[1]
	var foundCollectors bool
	for _, m := range msgs {
		if m.Role == provider.RoleTool && m.ToolCallID == "p1" {
			if strings.Contains(m.Text, "pagerduty") {
				foundCollectors = true
			}
		}
	}
	if !foundCollectors {
		t.Error("second Complete call missing RoleTool message with plain-JSON collectors data")
	}
}

// TestAskTruncatedToolCallsReplaySubset (FINDING 3): when comp.ToolCalls
// exceeds MaxToolCallsPerTurn (=1 here), only the dispatched subset must
// appear in the assistant message replayed to the adapter. The unmatched IDs
// for undispatched calls must not be present.
func TestAskTruncatedToolCallsReplaySubset(t *testing.T) {
	t.Parallel()

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "t1", Name: "find_code", Arguments: map[string]any{"q": "a"}},
			{ID: "t2", Name: "find_code", Arguments: map[string]any{"q": "b"}},
			{ID: "t3", Name: "find_code", Arguments: map[string]any{"q": "c"}},
		},
	}
	turn2 := provider.Completion{Text: "truncated done"}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, Options{
		MaxIterations:       6,
		MaxToolCallsPerTurn: 1, // only 1 dispatched
		SystemPrompt:        "sys",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = eng.Ask(context.Background(), "truncation test")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if adapter.calls < 2 {
		t.Fatalf("adapter.calls = %d, want >= 2", adapter.calls)
	}

	// In the second call's messages, the assistant message must carry only
	// the one dispatched call (ID "t1"), not "t2" or "t3".
	msgs := adapter.received[1]
	var assistantToolCalls []provider.ToolCall
	var toolResultIDs []string
	for _, m := range msgs {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			assistantToolCalls = m.ToolCalls
		}
		if m.Role == provider.RoleTool {
			toolResultIDs = append(toolResultIDs, m.ToolCallID)
		}
	}
	if len(assistantToolCalls) != 1 {
		t.Errorf("assistant message ToolCalls len = %d, want 1 (only dispatched call)", len(assistantToolCalls))
	} else if assistantToolCalls[0].ID != "t1" {
		t.Errorf("assistant message ToolCalls[0].ID = %q, want %q", assistantToolCalls[0].ID, "t1")
	}
	if len(toolResultIDs) != 1 || toolResultIDs[0] != "t1" {
		t.Errorf("RoleTool message IDs = %v, want [t1]", toolResultIDs)
	}
}

// TestAskPartialPacketPropagatesToAnswer (FINDING 4): when the runner returns
// an envelope whose embedded packet has Partial==true, ans.Partial must be set
// to true by the engine.
func TestAskPartialPacketPropagatesToAnswer(t *testing.T) {
	t.Parallel()

	// Envelope with an embedded packet that is partial (stale data).
	embeddedData := map[string]any{
		"answer_packet": map[string]any{
			"summary":             "partial summary",
			"supported":           true,
			"partial":             true,
			"truth_class":         "deterministic",
			"unsupported_reasons": []string{"underlying data is stale"},
		},
	}
	env := &query.ResponseEnvelope{
		Data: embeddedData,
		Truth: &query.TruthEnvelope{
			Level: query.TruthLevelExact,
			Basis: query.TruthBasisAuthoritativeGraph,
		},
	}

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "pa1", Name: "service_story", Arguments: map[string]any{}},
		},
	}
	turn2 := provider.Completion{Text: "partial answer"}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	runner := &recordingRunner{env: env}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "service status?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if !ans.Partial {
		t.Error("ans.Partial = false, want true when a packet is partial")
	}
	if len(ans.Packets) != 1 {
		t.Fatalf("len(Packets) = %d, want 1", len(ans.Packets))
	}
	if !ans.Packets[0].Partial {
		t.Error("Packets[0].Partial = false, want true (embedded partial packet not preserved)")
	}
}

// TestAskEnvelopeDataFedToModel (FINDING 5 / issue #3437): when the runner
// returns a canonical ResponseEnvelope whose Data field carries a non-empty
// payload (e.g. a repository list), the engine must feed that data to the LLM
// in the tool-result message AND set a non-empty Summary on the assembled
// AnswerPacket so bestPacketSummary returns usable prose at max-iterations.
//
// Before the fix, dispatchCall called NewAnswerPacket with no Summary and
// marshalToolResult sent only the packet skeleton (no Data). The model saw
// {"summary":"","truth_class":"deterministic","supported":true} — no content —
// and kept calling tools until MaxIterations. bestPacketSummary then returned ""
// producing "no supported evidence assembled".
func TestAskEnvelopeDataFedToModel(t *testing.T) {
	t.Parallel()

	// Simulate list_indexed_repositories: envelope with real Data and no
	// embedded answer_packet (the plain repository list handler path).
	repoData := map[string]any{
		"repositories": []map[string]any{
			{"id": "repo-1", "name": "acme-api"},
			{"id": "repo-2", "name": "acme-web"},
		},
		"total": 2,
	}
	env := &query.ResponseEnvelope{
		Data: repoData,
		Truth: &query.TruthEnvelope{
			Level: query.TruthLevelExact,
			Basis: query.TruthBasisAuthoritativeGraph,
		},
	}

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "d1", Name: "list_indexed_repositories", Arguments: map[string]any{"limit": 10}},
		},
		Usage: provider.TokenUsage{InputTokens: 5, OutputTokens: 3},
	}
	turn2 := provider.Completion{
		Text:  "There are 2 repositories indexed: acme-api and acme-web.",
		Usage: provider.TokenUsage{InputTokens: 10, OutputTokens: 8},
	}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	runner := &recordingRunner{env: env}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "How many repositories are indexed?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	// Loop must terminate after turn2 (final text turn), not burn all iterations.
	if adapter.calls != 2 {
		t.Errorf("adapter.calls = %d, want 2 (loop must terminate once model has data)", adapter.calls)
	}

	// The tool-result message fed to the adapter's second call must contain the
	// actual repository data so the model can synthesize an answer.
	if adapter.calls >= 2 {
		msgs := adapter.received[1]
		var toolResultText string
		for _, m := range msgs {
			if m.Role == provider.RoleTool && m.ToolCallID == "d1" {
				toolResultText = m.Text
			}
		}
		if !strings.Contains(toolResultText, "acme-api") {
			t.Errorf("tool-result message %q missing envelope data (acme-api); model cannot answer without it", toolResultText)
		}
	}

	// The assembled packet must have a non-empty Summary so bestPacketSummary
	// can produce prose when the loop hits max iterations.
	if len(ans.Packets) != 1 {
		t.Fatalf("len(Packets) = %d, want 1", len(ans.Packets))
	}
	if ans.Packets[0].Summary == "" {
		t.Error("Packets[0].Summary is empty; bestPacketSummary will return '' causing 'no supported evidence assembled'")
	}
	if !ans.Packets[0].Supported {
		t.Error("Packets[0].Supported = false, want true")
	}

	// Final prose must come from the model's turn2 text.
	if ans.Prose != turn2.Text {
		t.Errorf("Prose = %q, want %q", ans.Prose, turn2.Text)
	}

	// Must not carry the "no supported evidence assembled" limitation.
	for _, lim := range ans.Limitations {
		if lim == "no supported evidence assembled" {
			t.Errorf("Limitations contains %q — evidence was assembled but not surfaced", lim)
		}
	}
}

// TestAskPartialViaNonEmbeddedPacket (FINDING 4): when NewAnswerPacket produces
// a partial packet (e.g. stale freshness in the truth envelope), ans.Partial
// must also be set to true even without an embedded answer_packet key.
func TestAskPartialViaNonEmbeddedPacket(t *testing.T) {
	t.Parallel()

	// An envelope with stale freshness and no embedded answer_packet.
	env := &query.ResponseEnvelope{
		Truth: &query.TruthEnvelope{
			Level: query.TruthLevelExact,
			Basis: query.TruthBasisAuthoritativeGraph,
			Freshness: query.TruthFreshness{
				State: query.FreshnessStale,
			},
		},
	}

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "pp1", Name: "find_code", Arguments: map[string]any{}},
		},
	}
	turn2 := provider.Completion{Text: "stale answer"}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	runner := &recordingRunner{env: env}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "stale test?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if !ans.Partial {
		t.Error("ans.Partial = false, want true when NewAnswerPacket returns Partial=true")
	}
}
