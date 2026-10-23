// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestLoadResultShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		want    resultshape.Mode
		wantErr bool
	}{
		{name: "unset defaults to dual", value: "", want: resultshape.Dual},
		{name: "dual", value: "dual", want: resultshape.Dual},
		{name: "structured", value: "structured", want: resultshape.Structured},
		{name: "surrounding whitespace is trimmed", value: " Structured\n", want: resultshape.Structured},
		{name: "unknown value refuses", value: "text", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				if key == "ESHU_MCP_RESULT_SHAPE" {
					return tt.value
				}
				return ""
			}
			got, err := loadResultShape(getenv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadResultShape(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if tt.wantErr {
				for _, allowed := range []string{"dual", "structured"} {
					if !strings.Contains(err.Error(), allowed) {
						t.Fatalf("error %q does not list allowed value %q", err, allowed)
					}
				}
				return
			}
			if got != tt.want {
				t.Fatalf("loadResultShape(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// TestProcessResultShapeReachesTheServer proves the option newMCPServer
// forwards sets the process shape: tools/call on a context with no override
// (the stdio path) answers in that shape, and the startup log line records it.
func TestProcessResultShapeReachesTheServer(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{"ok": true},
			query.BuildTruthEnvelope(query.ProfileLocalAuthoritative, "code_search.fuzzy_symbol", query.TruthBasisContentIndex, "x"))
	})
	for _, mode := range []resultshape.Mode{resultshape.Dual, resultshape.Structured} {
		server, build, err := newMCPServer(handler, discardLogger(), nil, query.ProfileLocalAuthoritative,
			router.SurfaceFull, newLexicalRanker, mcp.WithResultShape(mode))
		if err != nil {
			t.Fatalf("newMCPServer error = %v", err)
		}
		if got := server.ResultShape(); got != mode {
			t.Fatalf("ResultShape() = %q, want %q", got, mode)
		}
		ts := httptest.NewServer(server.Handler(nil))
		t.Cleanup(ts.Close)
		resp, err := http.Post(ts.URL+"/mcp/message", "application/json",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_indexed_repositories","arguments":{}}}`))
		if err != nil {
			t.Fatalf("POST /mcp/message: %v", err)
		}
		var decoded struct {
			Result struct {
				Content []json.RawMessage `json:"content"`
			} `json:"result"`
		}
		err = json.NewDecoder(resp.Body).Decode(&decoded)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("decode response: %v", err)
		}
		wantContent := map[resultshape.Mode]int{resultshape.Dual: 2, resultshape.Structured: 1}[mode]
		if len(decoded.Result.Content) != wantContent {
			t.Fatalf("shape %q content items = %d, want %d", mode, len(decoded.Result.Content), wantContent)
		}
		var buf strings.Builder
		logToolSurface(slog.New(slog.NewJSONHandler(&buf, nil)), server, build)
		var line map[string]any
		if err := json.Unmarshal([]byte(buf.String()), &line); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		if line["result_shape"] != string(mode) {
			t.Fatalf("log line result_shape = %v, want %q", line["result_shape"], mode)
		}
	}
}
