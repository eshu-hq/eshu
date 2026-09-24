// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestGraphStatementFingerprintStableUnderWhitespace proves the fingerprint
// depends only on statement content, not incidental formatting, so the same
// logical shape always reports the same identity (#7035).
func TestGraphStatementFingerprintStableUnderWhitespace(t *testing.T) {
	a := "MATCH (n:Repo {id: $id})\nRETURN   n"
	b := "MATCH (n:Repo {id: $id}) RETURN n"
	if got, want := graphStatementFingerprint(a), graphStatementFingerprint(b); got != want {
		t.Fatalf("graphStatementFingerprint(a) = %q, want %q (equal to b)", got, want)
	}
}

// TestGraphStatementFingerprintDiffersBetweenStatements proves distinct
// statement shapes never collide, so a fingerprint reliably names one shape.
func TestGraphStatementFingerprintDiffersBetweenStatements(t *testing.T) {
	a := graphStatementFingerprint("MATCH (n:Repo) RETURN n")
	b := graphStatementFingerprint("MATCH (n:Workload) RETURN n")
	if a == b {
		t.Fatalf("fingerprints of distinct statements collided: %q", a)
	}
}

// TestGraphStatementFingerprintLength proves the fingerprint is bounded to
// the documented 12 hex characters.
func TestGraphStatementFingerprintLength(t *testing.T) {
	got := graphStatementFingerprint("MATCH (n) RETURN n")
	if len(got) != 12 {
		t.Fatalf("len(fingerprint) = %d, want 12 (%q)", len(got), got)
	}
}

// TestGraphStatementHeadTruncatesWithMarker proves a long statement is
// bounded with a visible truncation marker, and a short one passes through
// collapsed but otherwise unmarked.
func TestGraphStatementHeadTruncatesWithMarker(t *testing.T) {
	long := strings.Repeat("MATCH (n) ", 100)
	head := graphStatementHead(long)
	if len(head) <= graphStatementHeadMaxLen {
		t.Fatalf("head len = %d, want > %d (bound plus marker)", len(head), graphStatementHeadMaxLen)
	}
	if !strings.HasSuffix(head, graphStatementHeadTruncatedMarker) {
		t.Fatalf("head = %q, want suffix %q", head, graphStatementHeadTruncatedMarker)
	}

	short := "MATCH (n)   RETURN\tn"
	gotShort := graphStatementHead(short)
	if gotShort != "MATCH (n) RETURN n" {
		t.Fatalf("graphStatementHead(short) = %q, want whitespace-collapsed unmarked text", gotShort)
	}
	if strings.HasSuffix(gotShort, graphStatementHeadTruncatedMarker) {
		t.Fatalf("short head unexpectedly truncated: %q", gotShort)
	}
}

// TestNeo4jReaderSlowWarningLogsFingerprintAndHeadWithoutParams proves the
// slow-read warning names the exact statement shape (fingerprint plus a
// bounded head) while never exposing bound parameter values (#7035).
func TestNeo4jReaderSlowWarningLogsFingerprintAndHeadWithoutParams(t *testing.T) {
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}}}
	})
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	reader.policy.slowThreshold = time.Nanosecond

	const cypher = "MATCH (s:Secret {token: $token}) RETURN s"
	const secretParam = "private-token-marker"
	if _, err := reader.Run(context.Background(), cypher, map[string]any{"token": secretParam}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := logs.String()
	wantFingerprint := graphStatementFingerprint(cypher)
	wantHead := graphStatementHead(cypher)
	if !strings.Contains(got, wantFingerprint) {
		t.Fatalf("warning log = %s, want fingerprint %q", got, wantFingerprint)
	}
	if !strings.Contains(got, wantHead) {
		t.Fatalf("warning log = %s, want head %q", got, wantHead)
	}
	if strings.Contains(got, secretParam) {
		t.Fatalf("warning log exposed bound parameter value: %s", got)
	}
}

// TestNeo4jReaderSpanCarriesStatementFingerprint proves the fingerprint is
// attached to the read span so a trace links back to the exact statement
// shape (#7035), on every read, not only warned ones.
func TestNeo4jReaderSpanCarriesStatementFingerprint(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")
	const cypher = "MATCH (n:Repo) RETURN n"

	if _, err := reader.Run(context.Background(), cypher, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	want := graphStatementFingerprint(cypher)
	if got := graphReadSpanString(spans[0].Attributes(), telemetry.SpanAttrGraphReadStatementFingerprint); got != want {
		t.Fatalf("span fingerprint = %q, want %q", got, want)
	}
}

// TestResolveGraphReadSlowThresholdDefaultIsOneSecond proves the default
// threshold matches the 1-second endpoint budget (#7035).
func TestResolveGraphReadSlowThresholdDefaultIsOneSecond(t *testing.T) {
	getenv := func(string) string { return "" }
	got := resolveGraphReadSlowThreshold(getenv, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	if got != time.Second {
		t.Fatalf("resolveGraphReadSlowThreshold() = %v, want 1s", got)
	}
	if defaultGraphReadSlowThreshold != time.Second {
		t.Fatalf("defaultGraphReadSlowThreshold = %v, want 1s", defaultGraphReadSlowThreshold)
	}
}

// TestResolveGraphReadSlowThresholdEnvOverride proves a valid override wins.
func TestResolveGraphReadSlowThresholdEnvOverride(t *testing.T) {
	getenv := func(key string) string {
		if key == graphReadSlowThresholdEnv {
			return "500ms"
		}
		return ""
	}
	got := resolveGraphReadSlowThreshold(getenv, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	if got != 500*time.Millisecond {
		t.Fatalf("resolveGraphReadSlowThreshold() = %v, want 500ms", got)
	}
}

// TestResolveGraphReadSlowThresholdInvalidFallsBackAndLogsOnce proves a
// malformed override never disables the warning: it logs once and falls back
// to the default duration (#7035).
func TestResolveGraphReadSlowThresholdInvalidFallsBackAndLogsOnce(t *testing.T) {
	var logs bytes.Buffer
	getenv := func(key string) string {
		if key == graphReadSlowThresholdEnv {
			return "not-a-duration"
		}
		return ""
	}
	got := resolveGraphReadSlowThreshold(getenv, slog.New(slog.NewJSONHandler(&logs, nil)))
	if got != defaultGraphReadSlowThreshold {
		t.Fatalf("resolveGraphReadSlowThreshold() = %v, want default %v", got, defaultGraphReadSlowThreshold)
	}
	logged := logs.String()
	if strings.Count(logged, "\n") > 1 {
		t.Fatalf("resolveGraphReadSlowThreshold logged more than once: %s", logged)
	}
	if !strings.Contains(logged, "not-a-duration") {
		t.Fatalf("resolveGraphReadSlowThreshold warning = %s, want the invalid raw value for diagnosis", logged)
	}
}

// TestResolveGraphReadSlowThresholdNonPositiveFallsBack proves a zero or
// negative override never disables the slow-read warning (graphReadResult
// only emits `slow` when slowThreshold > 0).
func TestResolveGraphReadSlowThresholdNonPositiveFallsBack(t *testing.T) {
	for _, raw := range []string{"0s", "-1s"} {
		getenv := func(key string) string {
			if key == graphReadSlowThresholdEnv {
				return raw
			}
			return ""
		}
		got := resolveGraphReadSlowThreshold(getenv, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
		if got != defaultGraphReadSlowThreshold {
			t.Fatalf("resolveGraphReadSlowThreshold(%q) = %v, want default %v", raw, got, defaultGraphReadSlowThreshold)
		}
	}
}
