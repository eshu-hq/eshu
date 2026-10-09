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

// TestBootstrapCanonicalTransactionTimeoutAppliesConfiguredTimeoutToBothBackends pins the server-side
// transaction timeout contract for ESHU_CANONICAL_WRITE_TIMEOUT: NornicDB keeps
// its 30s default when the variable is unset, while Neo4j falls back to its
// 300s default (issue #7471) unless the operator configures a positive
// duration or explicitly opts out with a non-positive one.
func TestBootstrapCanonicalTransactionTimeoutAppliesConfiguredTimeoutToBothBackends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend runtimecfg.GraphBackend
		raw     string
		want    time.Duration
	}{
		{name: "neo4j configured", backend: runtimecfg.GraphBackendNeo4j, raw: "45s", want: 45 * time.Second},
		{name: "neo4j unset keeps default", backend: runtimecfg.GraphBackendNeo4j, raw: "", want: 300 * time.Second},
		{name: "neo4j invalid keeps default", backend: runtimecfg.GraphBackendNeo4j, raw: "soon", want: 300 * time.Second},
		{name: "neo4j zero opts out", backend: runtimecfg.GraphBackendNeo4j, raw: "0s", want: 0},
		{name: "neo4j negative opts out", backend: runtimecfg.GraphBackendNeo4j, raw: "-1s", want: 0},
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
			if got := bootstrapCanonicalTransactionTimeout(tt.backend, getenv); got != tt.want {
				t.Fatalf("bootstrapCanonicalTransactionTimeout(%s, %q) = %s, want %s", tt.backend, tt.raw, got, tt.want)
			}
		})
	}
}

// TestWarnUnboundedNeo4jWriteTimeoutLogsOnceWhenNeo4jHasNoTimeout pins the
// startup WARN that makes an unbounded Neo4j write budget visible: under the
// #7471 default it fires once for Neo4j only when ESHU_CANONICAL_WRITE_TIMEOUT
// explicitly opts out with a non-positive duration, and never for an unset,
// invalid, or configured value, or for NornicDB.
func TestWarnUnboundedNeo4jWriteTimeoutLogsOnceWhenNeo4jHasNoTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backend  runtimecfg.GraphBackend
		raw      string
		wantWarn bool
	}{
		{name: "neo4j unset", backend: runtimecfg.GraphBackendNeo4j, raw: ""},
		{name: "neo4j invalid", backend: runtimecfg.GraphBackendNeo4j, raw: "soon"},
		{name: "neo4j zero opts out", backend: runtimecfg.GraphBackendNeo4j, raw: "0s", wantWarn: true},
		{name: "neo4j negative opts out", backend: runtimecfg.GraphBackendNeo4j, raw: "-1s", wantWarn: true},
		{name: "neo4j configured", backend: runtimecfg.GraphBackendNeo4j, raw: "45s"},
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

// TestBootstrapNeo4jWriteTimeoutDefaultsToBounded pins the issue #7471 bound:
// an unset or invalid ESHU_CANONICAL_WRITE_TIMEOUT leaves Neo4j canonical
// writes bounded by the documented finite default instead of unbounded, while
// an explicit non-positive duration stays the deliberate unbounded opt-out.
func TestBootstrapNeo4jWriteTimeoutDefaultsToBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "unset defaults to bound", raw: "", want: 300 * time.Second},
		{name: "invalid defaults to bound", raw: "soon", want: 300 * time.Second},
		{name: "explicit zero opts out", raw: "0s", want: 0},
		{name: "explicit negative opts out", raw: "-1s", want: 0},
		{name: "explicit positive passes through", raw: "45s", want: 45 * time.Second},
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
			if got := bootstrapCanonicalTransactionTimeout(runtimecfg.GraphBackendNeo4j, getenv); got != tt.want {
				t.Fatalf("bootstrapCanonicalTransactionTimeout(neo4j, %q) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}
