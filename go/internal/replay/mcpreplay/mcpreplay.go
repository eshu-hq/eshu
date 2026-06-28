// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpreplay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/replay/apirecording"
)

// CallDescriptor describes one MCP tool call to drive through the in-process
// message handler. It is the MCP-seam counterpart to
// [apirecording.RequestDescriptor]: the recorder and asserter use it to
// produce [apirecording.Exchange] values with Transport=TransportMCP.
type CallDescriptor struct {
	// Name is a stable, human-readable label for the exchange. It must be
	// unique within a call set and stable across re-records.
	Name string
	// ToolName is the MCP tool name passed in the tools/call params.
	ToolName string
	// Arguments are the tool arguments encoded in the tools/call params.
	Arguments map[string]any
}

// DefaultOptions returns the MCP-flavor canonical defaults. The defaults
// inherit the API volatile-key collapse (correlation_id, observed_at) and add
// the JSON-RPC id field, which is minted per call and must not churn a
// re-recorded golden.
func DefaultOptions() apirecording.Options {
	return apirecording.DefaultOptions().WithRedactedKeys()
}

// RecordToolCalls drives each call through msgHandler (obtained from
// [mcp.InProcessMessageHandler]), extracts the structured content from the
// JSON-RPC result, canonicalizes it, and returns a Recording with
// Transport=TransportMCP. The handler must be backed by deterministic,
// in-process query logic so no live backend is required.
//
// The recorded structured content is the structuredContent field of the
// JSON-RPC tools/call result — the canonical, non-text representation of
// what the tool returned. For envelope-backed tools this is the
// ResponseEnvelope (data, truth, error); for plain-JSON tools it is the
// decoded payload.
//
// RecordToolCalls is deterministic given a deterministic handler: it
// performs no I/O beyond driving the in-process handler.
func RecordToolCalls(msgHandler http.Handler, calls []CallDescriptor, opts apirecording.Options) (apirecording.Recording, error) {
	if msgHandler == nil {
		return apirecording.Recording{}, errors.New("mcpreplay: message handler is nil")
	}
	if len(calls) == 0 {
		return apirecording.Recording{}, errors.New("mcpreplay: no calls to record")
	}
	seen := make(map[string]struct{}, len(calls))
	exchanges := make([]apirecording.Exchange, 0, len(calls))
	for i, call := range calls {
		if strings.TrimSpace(call.Name) == "" {
			return apirecording.Recording{}, fmt.Errorf("mcpreplay: call[%d]: name is required", i)
		}
		if strings.TrimSpace(call.ToolName) == "" {
			return apirecording.Recording{}, fmt.Errorf("mcpreplay: call %q: tool name is required", call.Name)
		}
		if _, dup := seen[call.Name]; dup {
			return apirecording.Recording{}, fmt.Errorf("mcpreplay: duplicate call name %q", call.Name)
		}
		seen[call.Name] = struct{}{}

		ex, err := driveCall(msgHandler, call, opts)
		if err != nil {
			return apirecording.Recording{}, fmt.Errorf("mcpreplay: call %q: %w", call.Name, err)
		}
		exchanges = append(exchanges, ex)
	}
	sort.SliceStable(exchanges, func(a, b int) bool {
		return exchanges[a].Request.Name < exchanges[b].Request.Name
	})
	return apirecording.Recording{
		SchemaVersion: apirecording.SchemaVersion,
		Exchanges:     exchanges,
	}, nil
}

// AssertToolCalls re-drives every recorded exchange's call through msgHandler
// and fails if any live structured content diverges from the golden. It is the
// offline MCP shape gate: a tool handler or response shape change that alters
// the structured content is caught here without a live backend.
//
// The returned error names the diverging exchange and shows the diff so a
// reviewer sees exactly what shifted. A nil error means every recorded MCP
// shape still holds.
func AssertToolCalls(msgHandler http.Handler, recording apirecording.Recording, opts apirecording.Options) error {
	if msgHandler == nil {
		return errors.New("mcpreplay: message handler is nil")
	}
	if err := validateRecording(recording); err != nil {
		return fmt.Errorf("mcpreplay: invalid recording: %w", err)
	}
	var mismatches []string
	for _, want := range recording.Exchanges {
		call, err := callFromExchange(want)
		if err != nil {
			return fmt.Errorf("mcpreplay: decode call for %q: %w", want.Request.Name, err)
		}
		got, err := driveCall(msgHandler, call, opts)
		if err != nil {
			return fmt.Errorf("mcpreplay: re-drive %q: %w", want.Request.Name, err)
		}
		if diff := diffBodies(want.Response.Body, got.Response.Body); diff != "" {
			mismatches = append(mismatches, fmt.Sprintf("exchange %q (tool %s):\n%s",
				want.Request.Name, call.ToolName, diff))
		}
	}
	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		return fmt.Errorf("mcpreplay: %d recorded MCP shape(s) diverged:\n%s",
			len(mismatches), strings.Join(mismatches, "\n\n"))
	}
	return nil
}

// AssertAnswerParity asserts that the data field of the named MCP exchange in
// mcpRecording equals the data field of the named HTTP exchange in
// apiRecording. It proves the MCP tool and the HTTP API endpoint answering the
// same query return consistent substantive truth.
//
// Legitimate envelope-level differences (truth metadata, text summaries,
// transport wrappers) are ignored: only the data payload is compared.
// A nil error means the two data payloads are byte-identical after canonical
// serialization.
func AssertAnswerParity(mcpRecording apirecording.Recording, mcpExchangeName string,
	apiRecording apirecording.Recording, apiExchangeName string,
) error {
	mcpData, err := extractData(mcpRecording, mcpExchangeName)
	if err != nil {
		return fmt.Errorf("mcpreplay: parity MCP side %q: %w", mcpExchangeName, err)
	}
	apiData, err := extractData(apiRecording, apiExchangeName)
	if err != nil {
		return fmt.Errorf("mcpreplay: parity API side %q: %w", apiExchangeName, err)
	}
	mcpBytes := marshalCanonical(mcpData)
	apiBytes := marshalCanonical(apiData)
	if mcpBytes != apiBytes {
		return fmt.Errorf("mcpreplay: answer parity failed between MCP %q and API %q:\n  --- MCP data\n%s\n  +++ API data\n%s",
			mcpExchangeName, apiExchangeName,
			indentBlock(mcpBytes), indentBlock(apiBytes))
	}
	return nil
}

// driveCall drives one MCP tool call through the message handler via httptest
// and returns the exchange with the canonical structured-content body.
func driveCall(msgHandler http.Handler, call CallDescriptor, opts apirecording.Options) (apirecording.Exchange, error) {
	rpcBody, err := buildToolCallBody(call)
	if err != nil {
		return apirecording.Exchange{}, fmt.Errorf("build JSON-RPC body: %w", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp/message", bytes.NewReader(rpcBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	msgHandler.ServeHTTP(rec, req)

	canonical, err := canonicalizeToolResult(rec.Body.Bytes(), opts)
	if err != nil {
		return apirecording.Exchange{}, fmt.Errorf("canonicalize tool result: %w", err)
	}

	reqDesc := apirecording.RequestDescriptor{
		Name:      call.Name,
		Transport: apirecording.TransportMCP,
		// Method and Path encode the MCP transport seam so the exchange is
		// self-describing and replayable: method=tools/call, path=the tool name.
		Method: "tools/call",
		Path:   call.ToolName,
		Body:   string(rpcBody),
	}
	return apirecording.Exchange{
		Request:  reqDesc,
		Response: apirecording.RecordedResponse{Status: rec.Code, Body: canonical},
	}, nil
}

// buildToolCallBody constructs the JSON-RPC 2.0 tools/call request body.
func buildToolCallBody(call CallDescriptor) ([]byte, error) {
	args := call.Arguments
	if args == nil {
		args = map[string]any{}
	}
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      call.ToolName,
			"arguments": args,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal tools/call body: %w", err)
	}
	return data, nil
}

// canonicalizeToolResult extracts the structuredContent from the JSON-RPC
// result and canonicalizes it. The structured content is the authoritative,
// non-text representation of the tool response — it is what replay must assert
// on to catch shape drift, not the text summary which is a human convenience.
func canonicalizeToolResult(raw []byte, opts apirecording.Options) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("empty JSON-RPC response body")
	}

	// Parse the outer JSON-RPC envelope.
	var rpcResp struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse JSON-RPC response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if len(rpcResp.Result.StructuredContent) == 0 {
		return nil, fmt.Errorf("JSON-RPC result has no structuredContent field; the tool may not support structured output")
	}

	canonical, err := replay.Canonicalize(rpcResp.Result.StructuredContent, opts.Canonical())
	if err != nil {
		return nil, fmt.Errorf("canonicalize structuredContent: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(canonical))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode canonical structuredContent: %w", err)
	}
	return value, nil
}

// callFromExchange reconstructs a CallDescriptor from a recorded exchange so
// AssertToolCalls can re-drive it. The tool name is the exchange's Path and
// the arguments are decoded from the Body JSON-RPC params.
func callFromExchange(ex apirecording.Exchange) (CallDescriptor, error) {
	var msg struct {
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(ex.Request.Body), &msg); err != nil {
		return CallDescriptor{}, fmt.Errorf("decode exchange body: %w", err)
	}
	return CallDescriptor{
		Name:      ex.Request.Name,
		ToolName:  msg.Params.Name,
		Arguments: msg.Params.Arguments,
	}, nil
}

// validateRecording rejects a recording that cannot be safely replayed as MCP
// exchanges. It wraps the format check with an MCP-specific transport check.
func validateRecording(r apirecording.Recording) error {
	if r.SchemaVersion != apirecording.SchemaVersion {
		return fmt.Errorf("unsupported schema_version %q (want %q)", r.SchemaVersion, apirecording.SchemaVersion)
	}
	if len(r.Exchanges) == 0 {
		return errors.New("recording must contain at least one exchange")
	}
	for i, ex := range r.Exchanges {
		if ex.Request.Transport != apirecording.TransportMCP {
			return fmt.Errorf("exchange[%d] %q: transport %q is not %q; use RecordToolCalls to produce MCP recordings",
				i, ex.Request.Name, ex.Request.Transport, apirecording.TransportMCP)
		}
	}
	return nil
}

// extractData retrieves the data field from the named exchange's body in the
// recording. The body must be a JSON object with a "data" key, as the
// canonical response envelope shape requires. A null JSON data field (nil in
// Go) is an error: AssertAnswerParity is only meaningful when both sides carry
// a non-nil payload. A nil-vs-nil match would be a vacuous pass that guards
// nothing — the caller must supply exchanges with real payloads.
func extractData(r apirecording.Recording, exchangeName string) (any, error) {
	for _, ex := range r.Exchanges {
		if ex.Request.Name != exchangeName {
			continue
		}
		body, ok := ex.Response.Body.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("exchange %q body is %T, want map", exchangeName, ex.Response.Body)
		}
		data, ok := body["data"]
		if !ok {
			return nil, fmt.Errorf("exchange %q body has no data field", exchangeName)
		}
		if data == nil {
			return nil, fmt.Errorf("exchange %q data field is null; parity requires a non-null payload on both sides", exchangeName)
		}
		return data, nil
	}
	return nil, fmt.Errorf("exchange %q not found in recording", exchangeName)
}

// diffBodies returns an empty string when want and got are equal (by canonical
// JSON bytes), or a human-readable diff otherwise. It mirrors the apirecording
// diffResponse body comparison without the status line.
func diffBodies(want, got any) string {
	wantBody := marshalCanonical(want)
	gotBody := marshalCanonical(got)
	if wantBody == gotBody {
		return ""
	}
	var lines []string
	lines = append(lines, "  structured content:")
	lines = append(lines, "    --- recorded")
	lines = append(lines, indentBlock(wantBody))
	lines = append(lines, "    +++ live")
	lines = append(lines, indentBlock(gotBody))
	return strings.Join(lines, "\n")
}

// marshalCanonical renders a value to stable indented JSON for comparison.
func marshalCanonical(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Sprintf("<unmarshalable: %v>", err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// indentBlock prefixes every line of s with a fixed indent.
func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "      " + lines[i]
	}
	return strings.Join(lines, "\n")
}
