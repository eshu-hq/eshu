// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpreplay_test

import (
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/replay/apirecording"
	"github.com/eshu-hq/eshu/go/internal/replay/mcpreplay"
)

// updateGolden rewrites the recorded MCP-shape golden from the live handler
// output. Run `go test ./internal/replay/mcpreplay -update-golden` after an
// intentional, reviewed shape change.
var updateGolden = flag.Bool("update-golden", false, "rewrite the mcpreplay golden file from live handler output")

const (
	mcpGoldenPath = "testdata/query-playbooks-mcp.recording.json"
	// apiGoldenPath references the R-8 HTTP API golden. The go test runner sets
	// the working directory to the package directory, so this relative path is
	// stable and resolves to the apirecording testdata under any build system.
	apiGoldenPath = "../apirecording/testdata/query-playbooks.recording.json"
)

// queryPlaybookHandler builds the real query mux mounting only the deterministic
// query-playbook handler. It is the same handler used in the apirecording tests,
// so the answer-parity check operates on genuinely shared truth.
func queryPlaybookHandler() http.Handler {
	mux := http.NewServeMux()
	router := &query.APIRouter{
		Playbooks: &query.QueryPlaybookHandler{Profile: query.ProfileProduction},
	}
	router.Mount(mux)
	return mux
}

// mcpMessageHandler returns an in-process MCP message handler backed by the
// query-playbook handler. It is the replay seam for MCP tool calls.
func mcpMessageHandler() http.Handler {
	return mcp.InProcessMessageHandler(queryPlaybookHandler(), slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

// toolCalls is the representative MCP call set the golden asserts on. It
// covers the two query-playbook tools (list and resolve), plus a resolve
// refusal (unknown playbook → not_found error envelope). These match the HTTP
// API request set in apirecording so the answer-parity check is meaningful.
func toolCalls() []mcpreplay.CallDescriptor {
	return []mcpreplay.CallDescriptor{
		{
			Name:      "list-playbooks-success",
			ToolName:  "list_query_playbooks",
			Arguments: map[string]any{},
		},
		{
			Name:     "resolve-playbook-success",
			ToolName: "resolve_query_playbook",
			Arguments: map[string]any{
				"playbook_id": "service_story_citation",
				"inputs": map[string]any{
					"service_name": "payments-api",
					"environment":  "prod",
				},
			},
		},
		{
			Name:     "resolve-playbook-unknown-refusal",
			ToolName: "resolve_query_playbook",
			Arguments: map[string]any{
				"playbook_id": "missing",
				"inputs":      map[string]any{},
			},
		},
	}
}

// TestMCPToolCallRecordingMatchesGolden is the offline MCP shape gate: it
// records the live handler output and asserts it against the committed golden.
// Under -update-golden it rewrites the golden instead. A tool handler or
// envelope shape change that is not reflected in the golden fails this test
// offline.
func TestMCPToolCallRecordingMatchesGolden(t *testing.T) {
	handler := mcpMessageHandler()
	opts := mcpreplay.DefaultOptions()

	recording, err := mcpreplay.RecordToolCalls(handler, toolCalls(), opts)
	if err != nil {
		t.Fatalf("RecordToolCalls() error = %v, want nil", err)
	}

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(mcpGoldenPath), 0o750); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := apirecording.WriteFile(mcpGoldenPath, recording); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		t.Logf("rewrote golden %s", mcpGoldenPath)
		return
	}

	golden, err := apirecording.LoadFile(mcpGoldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v; run with -update-golden to create it", mcpGoldenPath, err)
	}

	if err := mcpreplay.AssertToolCalls(handler, golden, opts); err != nil {
		t.Fatalf("recorded MCP shape diverged from golden:\n%v", err)
	}

	// Assert the golden carries exactly the expected exchange set so a silently
	// dropped exchange (which AssertToolCalls would not catch) fails here.
	assertExchangeSet(t, "golden", golden, expectedExchangeNames())
}

// TestMCPAssertCatchesDeliberateShapeChange proves the gate is not
// false-green: a golden with a mutated structured-content body MUST fail
// AssertToolCalls against the live handler. This is the key anti-false-green
// proof for R-9: if this test ever passes silently, the gate is asserting
// nothing.
func TestMCPAssertCatchesDeliberateShapeChange(t *testing.T) {
	handler := mcpMessageHandler()
	opts := mcpreplay.DefaultOptions()

	golden, err := apirecording.LoadFile(mcpGoldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", mcpGoldenPath, err)
	}

	// Confirm the unmutated golden matches first so the failure below is
	// attributable to the mutation, not a pre-existing drift.
	if err := mcpreplay.AssertToolCalls(handler, golden, opts); err != nil {
		t.Fatalf("baseline golden must match live handler before mutation: %v", err)
	}

	// Inject a phantom field into the list-playbooks-success structured content.
	// The live handler never emits __phantom__, so AssertToolCalls must fail.
	mutated := injectPhantomField(t, golden, "list-playbooks-success")
	err = mcpreplay.AssertToolCalls(handler, mutated, opts)
	if err == nil {
		t.Fatal("AssertToolCalls() = nil on a mutated golden; the gate is false-green")
	}
	t.Logf("correctly caught shape change: %v", err)
}

// TestMCPAssertCatchesIsErrorFlip proves the gate catches error-classification
// drift, not only payload-shape drift: flipping the recorded isError flag on the
// refusal exchange (without touching the envelope body) MUST fail AssertToolCalls
// against the live handler. MCP clients branch on isError to tell a tool error
// from a successful payload, so a regression that returns the same
// {data:null,error:{...}} envelope but mislabels it isError:false has to be
// caught. The refusal golden records isError:true; flipping it to false and
// re-asserting proves the gate is watching the flag.
func TestMCPAssertCatchesIsErrorFlip(t *testing.T) {
	handler := mcpMessageHandler()
	opts := mcpreplay.DefaultOptions()

	golden, err := apirecording.LoadFile(mcpGoldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", mcpGoldenPath, err)
	}

	// Baseline must match so the failure below is attributable to the flip.
	if err := mcpreplay.AssertToolCalls(handler, golden, opts); err != nil {
		t.Fatalf("baseline golden must match live handler before mutation: %v", err)
	}

	// Confirm the refusal exchange genuinely records isError:true before the
	// flip, so flipping it to false is a real change the live handler contradicts.
	if got := recordedIsError(t, golden, "resolve-playbook-unknown-refusal"); got != true {
		t.Fatalf("refusal exchange records is_error=%v, want true; the golden does not encode the refusal classification", got)
	}

	flipped := flipIsError(t, golden, "resolve-playbook-unknown-refusal")
	if err := mcpreplay.AssertToolCalls(handler, flipped, opts); err == nil {
		t.Fatal("AssertToolCalls() = nil after flipping is_error on the refusal golden; the gate does not catch error-classification drift")
	} else {
		t.Logf("correctly caught is_error flip: %v", err)
	}
}

// TestMCPAnswerParity proves that the MCP list_query_playbooks tool and the
// HTTP GET /api/v0/query-playbooks endpoint return the same substantive data.
// Both exchange sets are recorded from the same in-process handler; this test
// asserts their data fields are byte-identical after canonicalization.
//
// This is the answer-parity requirement from issue #4111: an API/graph change
// that silently breaks MCP callers is caught because the data payload diverges
// between the HTTP API golden and the MCP golden.
func TestMCPAnswerParity(t *testing.T) {
	mcpGolden, err := apirecording.LoadFile(mcpGoldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", mcpGoldenPath, err)
	}
	apiGolden, err := apirecording.LoadFile(apiGoldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", apiGoldenPath, err)
	}

	// The MCP list_query_playbooks response and the HTTP GET /api/v0/query-playbooks
	// response must carry the same data payload.
	if err := mcpreplay.AssertAnswerParity(
		mcpGolden, "list-playbooks-success",
		apiGolden, "list-catalog-success",
	); err != nil {
		t.Fatalf("MCP/API answer parity failed:\n%v", err)
	}
}

// TestMCPAnswerParityParityCheckIsNotFalseGreen proves the parity check is not
// false-green: a recording with a deliberately wrong MCP data payload MUST fail
// AssertAnswerParity even when AssertToolCalls passes against a consistent but
// wrong handler.
func TestMCPAnswerParityParityCheckIsNotFalseGreen(t *testing.T) {
	apiGolden, err := apirecording.LoadFile(apiGoldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", apiGoldenPath, err)
	}

	// Build a fake MCP recording where the list exchange has a data field that
	// disagrees with the API golden's data (extra playbook count field injected).
	fakeRecording := buildFakeMCPRecordingWithWrongData(t)

	err = mcpreplay.AssertAnswerParity(fakeRecording, "list-playbooks-success", apiGolden, "list-catalog-success")
	if err == nil {
		t.Fatal("AssertAnswerParity() = nil on a recording with wrong data; the parity check is false-green")
	}
	t.Logf("correctly caught parity violation: %v", err)
}

// TestMCPRecordingIsDeterministic proves two records of the same handler
// produce byte-identical golden bytes.
func TestMCPRecordingIsDeterministic(t *testing.T) {
	handler := mcpMessageHandler()
	opts := mcpreplay.DefaultOptions()
	calls := toolCalls()

	first, err := mcpreplay.RecordToolCalls(handler, calls, opts)
	if err != nil {
		t.Fatalf("first RecordToolCalls() error = %v", err)
	}
	second, err := mcpreplay.RecordToolCalls(handler, calls, opts)
	if err != nil {
		t.Fatalf("second RecordToolCalls() error = %v", err)
	}
	firstBytes, err := apirecording.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal(first) error = %v", err)
	}
	secondBytes, err := apirecording.Marshal(second)
	if err != nil {
		t.Fatalf("Marshal(second) error = %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatalf("re-record is not byte-identical:\nfirst=\n%s\nsecond=\n%s", firstBytes, secondBytes)
	}
}

// expectedExchangeNames returns the set of exchange names every committed
// golden must carry — one per entry in toolCalls().
func expectedExchangeNames() map[string]struct{} {
	want := make(map[string]struct{})
	for _, c := range toolCalls() {
		want[c.Name] = struct{}{}
	}
	return want
}

// assertExchangeSet fails when r's exchange names do not exactly equal want.
func assertExchangeSet(t *testing.T, label string, r apirecording.Recording, want map[string]struct{}) {
	t.Helper()
	got := make(map[string]struct{}, len(r.Exchanges))
	for _, ex := range r.Exchanges {
		got[ex.Request.Name] = struct{}{}
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Fatalf("%s is missing required exchange %q; got %v", label, name, got)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Fatalf("%s carries unexpected exchange %q; want exactly %v", label, name, want)
		}
	}
}

// injectPhantomField returns a deep copy of r with a phantom field injected
// into the named exchange's structured-content body so AssertToolCalls must
// detect the divergence.
func injectPhantomField(t *testing.T, r apirecording.Recording, name string) apirecording.Recording {
	t.Helper()
	out := deepCopy(t, r)
	for i := range out.Exchanges {
		if out.Exchanges[i].Request.Name != name {
			continue
		}
		body, ok := out.Exchanges[i].Response.Body.(map[string]any)
		if !ok {
			t.Fatalf("exchange %q body is %T, want map", name, out.Exchanges[i].Response.Body)
		}
		body["__phantom_field__"] = "not-emitted-by-handler"
		out.Exchanges[i].Response.Body = body
		return out
	}
	t.Fatalf("exchange %q not found to mutate", name)
	return out
}

// recordedIsError returns the recorded is_error flag for the named exchange.
// It is used to confirm the refusal golden genuinely encodes isError:true
// before the flip test mutates it, so the flip is a real change.
func recordedIsError(t *testing.T, r apirecording.Recording, name string) bool {
	t.Helper()
	for _, ex := range r.Exchanges {
		if ex.Request.Name != name {
			continue
		}
		body, ok := ex.Response.Body.(map[string]any)
		if !ok {
			t.Fatalf("exchange %q body is %T, want map", name, ex.Response.Body)
		}
		v, ok := body["is_error"].(bool)
		if !ok {
			t.Fatalf("exchange %q body has no bool is_error field; got %T", name, body["is_error"])
		}
		return v
	}
	t.Fatalf("exchange %q not found", name)
	return false
}

// flipIsError returns a deep copy of r with the named exchange's recorded
// is_error flag negated, leaving the structured content untouched. It proves
// AssertToolCalls compares the classification bit, not only the payload shape.
func flipIsError(t *testing.T, r apirecording.Recording, name string) apirecording.Recording {
	t.Helper()
	out := deepCopy(t, r)
	for i := range out.Exchanges {
		if out.Exchanges[i].Request.Name != name {
			continue
		}
		body, ok := out.Exchanges[i].Response.Body.(map[string]any)
		if !ok {
			t.Fatalf("exchange %q body is %T, want map", name, out.Exchanges[i].Response.Body)
		}
		cur, ok := body["is_error"].(bool)
		if !ok {
			t.Fatalf("exchange %q body has no bool is_error field; got %T", name, body["is_error"])
		}
		body["is_error"] = !cur
		out.Exchanges[i].Response.Body = body
		return out
	}
	t.Fatalf("exchange %q not found to mutate", name)
	return out
}

// buildFakeMCPRecordingWithWrongData builds a minimal MCP recording whose
// list-playbooks-success data field disagrees with the real API response.
// It is used to prove the parity check is not false-green.
func buildFakeMCPRecordingWithWrongData(t *testing.T) apirecording.Recording {
	t.Helper()
	fakeData := map[string]any{
		"data": map[string]any{
			"count":          999,
			"playbooks":      []any{},
			"schema_version": "query-playbooks.v1",
			"versions":       []any{},
		},
		"error": nil,
		"truth": map[string]any{
			"basis":      "runtime_state",
			"capability": "query.playbooks",
			"freshness":  map[string]any{"state": "fresh"},
			"level":      "exact",
			"profile":    "production",
			"reason":     "deterministic query playbook catalog; no live backend read",
		},
	}
	bodyBytes, err := json.Marshal(fakeData)
	if err != nil {
		t.Fatalf("marshal fake data: %v", err)
	}
	var bodyValue any
	if err := json.Unmarshal(bodyBytes, &bodyValue); err != nil {
		t.Fatalf("unmarshal fake data: %v", err)
	}
	return apirecording.Recording{
		SchemaVersion: apirecording.SchemaVersion,
		Exchanges: []apirecording.Exchange{
			{
				Request: apirecording.RequestDescriptor{
					Name:      "list-playbooks-success",
					Transport: apirecording.TransportMCP,
					Method:    "tools/call",
					Path:      "list_query_playbooks",
					Body:      `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_query_playbooks","arguments":{}}}`,
				},
				Response: apirecording.RecordedResponse{
					Status: http.StatusOK,
					Body:   bodyValue,
				},
			},
		},
	}
}

// deepCopy round-trips a recording through JSON to produce an independent copy.
func deepCopy(t *testing.T, r apirecording.Recording) apirecording.Recording {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal for deep copy: %v", err)
	}
	var out apirecording.Recording
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal for deep copy: %v", err)
	}
	return out
}
