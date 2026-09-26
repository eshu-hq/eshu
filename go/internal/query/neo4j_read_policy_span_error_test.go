// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestNeo4jReaderSpanErrorRedactsEchoedStatementLiterals is the #7065
// regression: a driver error that echoes an ad-hoc statement must never reach
// the span's error status or exception event with inline literals intact. The
// span still reports the error outcome with a redacted message.
func TestNeo4jReaderSpanErrorRedactsEchoedStatementLiterals(t *testing.T) {
	const literal = "s3cr3t-literal"
	queryText := "MATCH (n:Secret {token: '" + literal + "'}) RETURN n"
	// Neo4j statement errors quote the offending statement in the message;
	// ad-hoc routes send it with no parameters, so literals ride along.
	driverErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Statement.SyntaxError",
		Msg:  "Invalid input (line 1, column 24 (offset: 23))\n\"" + queryText + "\"",
	}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{run: func(
			context.Context,
			string,
			map[string]any,
			...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			return nil, driverErr
		}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")

	if _, err := reader.Run(context.Background(), queryText, nil); err == nil {
		t.Fatal("Run() error = nil, want the driver syntax error")
	}

	var querySpan sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == "neo4j.query" {
			querySpan = span
		}
	}
	if querySpan == nil {
		t.Fatal("no neo4j.query span recorded")
	}
	for _, field := range querySpan.Attributes() {
		if string(field.Key) == telemetry.SpanAttrGraphReadOutcome && field.Value.AsString() != string(graphReadOutcomeError) {
			t.Fatalf("span outcome = %q, want %q", field.Value.AsString(), graphReadOutcomeError)
		}
	}
	if status := querySpan.Status(); status.Code != codes.Error {
		t.Fatalf("span status code = %v, want Error", status.Code)
	} else if strings.Contains(status.Description, literal) {
		t.Fatalf("span status exposed statement literal: %q", status.Description)
	} else if !strings.Contains(status.Description, "<REDACTED>") {
		t.Fatalf("span status = %q, want the literal replaced by the redactor placeholder", status.Description)
	}
	for _, event := range querySpan.Events() {
		for _, field := range event.Attributes {
			if strings.Contains(field.Value.AsString(), literal) {
				t.Fatalf("span event %q exposed statement literal in %q", event.Name, field.Key)
			}
		}
	}
}
