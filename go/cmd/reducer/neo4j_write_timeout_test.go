// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// TestReducerTransactionTimeoutAppliesConfiguredTimeoutToBothBackends pins the server-side
// transaction timeout contract for ESHU_CANONICAL_WRITE_TIMEOUT: NornicDB keeps
// its 30s default when the variable is unset, while Neo4j applies the timeout
// only when the operator configures a valid positive duration, so an unset
// Neo4j deployment keeps its unbounded transactions.
func TestReducerTransactionTimeoutAppliesConfiguredTimeoutToBothBackends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend runtimecfg.GraphBackend
		raw     string
		want    time.Duration
	}{
		{name: "neo4j configured", backend: runtimecfg.GraphBackendNeo4j, raw: "300s", want: 300 * time.Second},
		{name: "neo4j unset", backend: runtimecfg.GraphBackendNeo4j, raw: "", want: 0},
		{name: "neo4j invalid", backend: runtimecfg.GraphBackendNeo4j, raw: "soon", want: 0},
		{name: "neo4j non-positive", backend: runtimecfg.GraphBackendNeo4j, raw: "-1s", want: 0},
		{name: "nornicdb configured", backend: runtimecfg.GraphBackendNornicDB, raw: "3s", want: 3 * time.Second},
		{name: "nornicdb unset keeps default", backend: runtimecfg.GraphBackendNornicDB, raw: "", want: 30 * time.Second},
		{name: "nornicdb invalid keeps default", backend: runtimecfg.GraphBackendNornicDB, raw: "soon", want: 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				if key == "ESHU_CANONICAL_WRITE_TIMEOUT" {
					return tt.raw
				}
				return ""
			}
			if got := reducerTransactionTimeout(tt.backend, getenv); got != tt.want {
				t.Fatalf("reducerTransactionTimeout(%s, %q) = %s, want %s", tt.backend, tt.raw, got, tt.want)
			}
		})
	}
}

// TestWarnUnboundedNeo4jWriteTimeoutLogsOnceWhenNeo4jHasNoTimeout pins the
// startup WARN that makes an unbounded Neo4j write budget visible: it fires
// once for Neo4j when ESHU_CANONICAL_WRITE_TIMEOUT is unset or invalid, and
// never for a configured Neo4j timeout or for NornicDB.
func TestWarnUnboundedNeo4jWriteTimeoutLogsOnceWhenNeo4jHasNoTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backend  runtimecfg.GraphBackend
		raw      string
		wantWarn bool
	}{
		{name: "neo4j unset", backend: runtimecfg.GraphBackendNeo4j, raw: "", wantWarn: true},
		{name: "neo4j invalid", backend: runtimecfg.GraphBackendNeo4j, raw: "soon", wantWarn: true},
		{name: "neo4j configured", backend: runtimecfg.GraphBackendNeo4j, raw: "300s"},
		{name: "nornicdb unset", backend: runtimecfg.GraphBackendNornicDB, raw: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			getenv := func(key string) string {
				if key == "ESHU_CANONICAL_WRITE_TIMEOUT" {
					return tt.raw
				}
				return ""
			}
			warnUnboundedNeo4jWriteTimeout(logger, tt.backend, getenv)

			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			if !tt.wantWarn {
				if buf.Len() != 0 {
					t.Fatalf("unexpected log output: %s", buf.String())
				}
				return
			}
			if len(lines) != 1 {
				t.Fatalf("log lines = %d, want 1: %s", len(lines), buf.String())
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
				t.Fatalf("decode log record: %v", err)
			}
			if record["level"] != "WARN" ||
				record["event_name"] != "graph.write_timeout.unbounded" ||
				record["graph_backend"] != "neo4j" ||
				record["env_var"] != "ESHU_CANONICAL_WRITE_TIMEOUT" {
				t.Fatalf("log record = %v, want WARN graph.write_timeout.unbounded for neo4j", record)
			}
		})
	}
}
