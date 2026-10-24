// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestNegotiateProtocolVersion pins the MCP lifecycle rule (spec
// 2025-06-18, "Version Negotiation"): a server that supports the requested
// version MUST answer with that same version, and otherwise answers with
// another version it supports, preferably its latest. The table drives the
// pure function with a two-version list so a hardcoded answer cannot pass.
func TestNegotiateProtocolVersion(t *testing.T) {
	t.Parallel()

	supported := []string{"2024-11-05", "2099-01-01"}
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{name: "oldest supported is echoed", requested: "2024-11-05", want: "2024-11-05"},
		{name: "newest supported is echoed", requested: "2099-01-01", want: "2099-01-01"},
		{name: "unsupported request gets the server latest", requested: "2025-06-18", want: "2099-01-01"},
		{name: "absent request gets the server latest", requested: "", want: "2099-01-01"},
		{name: "garbage request gets the server latest", requested: "not-a-version", want: "2099-01-01"},
		{name: "match is exact, not a prefix", requested: "2024-11-05 ", want: "2099-01-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := negotiateProtocolVersion(tc.requested, supported); got != tc.want {
				t.Fatalf("negotiateProtocolVersion(%q) = %q, want %q", tc.requested, got, tc.want)
			}
		})
	}
}

// TestSupportedProtocolVersionsAreOnlyThoseEshuImplements guards the claim
// itself. Eshu serves the HTTP+SSE transport, decodes one JSON-RPC object per
// POST (no batching), and does not read an MCP-Protocol-Version header, so it
// implements the 2024-11-05 wire contract and no later revision. Adding a
// version here means implementing that revision's transport changes first.
func TestSupportedProtocolVersionsAreOnlyThoseEshuImplements(t *testing.T) {
	t.Parallel()

	if got, want := strings.Join(supportedProtocolVersions, ","), "2024-11-05"; got != want {
		t.Fatalf("supportedProtocolVersions = %q, want %q", got, want)
	}
}

func initializeBody(params string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` + params + `}`
}

func protocolVersionOf(t *testing.T, raw []byte) string {
	t.Helper()
	var resp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode initialize reply %q: %v", raw, err)
	}
	return resp.Result.ProtocolVersion
}

var protocolVersionCases = []struct {
	name   string
	params string
	want   string
}{
	{name: "supported version is echoed", params: `{"protocolVersion":"2024-11-05"}`, want: "2024-11-05"},
	{name: "newer unsupported version gets the server version", params: `{"protocolVersion":"2025-06-18"}`, want: "2024-11-05"},
	{name: "absent version gets the server version", params: `{}`, want: "2024-11-05"},
	{name: "missing params gets the server version", params: `null`, want: "2024-11-05"},
	{name: "non-string version gets the server version", params: `{"protocolVersion":5}`, want: "2024-11-05"},
}

// TestInitializeProtocolVersionHTTP covers a standalone POST /mcp/message.
func TestInitializeProtocolVersionHTTP(t *testing.T) {
	for _, tc := range protocolVersionCases {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer()
			req := httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(initializeBody(tc.params)))
			rec := httptest.NewRecorder()
			s.handleHTTPMessage(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
			}
			if got := protocolVersionOf(t, rec.Body.Bytes()); got != tc.want {
				t.Fatalf("protocolVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestInitializeProtocolVersionSSESession covers a POST linked to an SSE
// session, whose reply travels over the session channel and not the response.
func TestInitializeProtocolVersionSSESession(t *testing.T) {
	for _, tc := range protocolVersionCases {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer()
			sess := &sseSession{ch: make(chan []byte, 4)}
			s.sessMu.Lock()
			s.sessions["sess-1"] = sess
			s.sessMu.Unlock()
			req := httptest.NewRequest(http.MethodPost, "/mcp/message?sessionId=sess-1", strings.NewReader(initializeBody(tc.params)))
			rec := httptest.NewRecorder()
			s.handleHTTPMessage(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body %s", rec.Code, rec.Body.String())
			}
			select {
			case msg := <-sess.ch:
				if got := protocolVersionOf(t, msg); got != tc.want {
					t.Fatalf("protocolVersion = %q, want %q", got, tc.want)
				}
			default:
				t.Fatal("no initialize reply on the SSE session channel")
			}
		})
	}
}

// TestInitializeProtocolVersionStdio covers the stdio transport by driving Run
// over a real pipe pair in place of os.Stdin and os.Stdout.
func TestInitializeProtocolVersionStdio(t *testing.T) {
	for _, tc := range protocolVersionCases {
		t.Run(tc.name, func(t *testing.T) {
			inR, inW, err := os.Pipe()
			if err != nil {
				t.Fatalf("stdin pipe: %v", err)
			}
			outR, outW, err := os.Pipe()
			if err != nil {
				t.Fatalf("stdout pipe: %v", err)
			}
			origIn, origOut := os.Stdin, os.Stdout
			os.Stdin, os.Stdout = inR, outW
			defer func() { os.Stdin, os.Stdout = origIn, origOut }()
			defer func() { _ = inR.Close(); _ = outR.Close() }()

			if _, err := inW.WriteString(initializeBody(tc.params) + "\n"); err != nil {
				t.Fatalf("write request: %v", err)
			}
			_ = inW.Close() // EOF ends Run after it answers the request

			if err := testServer().Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			_ = outW.Close()

			var buf bytes.Buffer
			if _, err := buf.ReadFrom(bufio.NewReader(outR)); err != nil {
				t.Fatalf("read reply: %v", err)
			}
			if got := protocolVersionOf(t, buf.Bytes()); got != tc.want {
				t.Fatalf("protocolVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNegotiateProtocolVersionEmptySupportedListDoesNotPanic pins the guard:
// an emptied supported list answers with no version instead of panicking on
// initialize.
func TestNegotiateProtocolVersionEmptySupportedListDoesNotPanic(t *testing.T) {
	t.Parallel()

	if got := negotiateProtocolVersion("2024-11-05", nil); got != "" {
		t.Fatalf("empty supported list answered %q, want empty", got)
	}
}
