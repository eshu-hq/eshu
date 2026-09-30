// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const publicErrorLiteral = "s3cr3t-literal"

// quotingDriverError builds the shape a Neo4j statement error takes: the
// message quotes the offending statement, inline literals included.
func quotingDriverError() *neo4jdriver.Neo4jError {
	return &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Statement.SyntaxError",
		Msg: "Invalid input (line 1, column 24 (offset: 23))\n\"MATCH (n:Secret {token: '" +
			publicErrorLiteral + "'}) RETURN n\"",
	}
}

func failingPolicyTestReader(driverErr error, logs *bytes.Buffer) *Neo4jReader {
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
	if logs != nil {
		reader.policy.logger = slog.New(slog.NewTextHandler(logs, nil))
	}
	return reader
}

// TestNeo4jReaderErrorTextIsStableAndRedacted is the #7253 regression at the
// reader: whatever a handler does with the error (err.Error(), a %w wrap, a
// %v format), the driver's statement text must not survive. The cause stays
// reachable through errors.As for classification, and the operator still gets
// the redacted detail in a log line.
func TestNeo4jReaderErrorTextIsStableAndRedacted(t *testing.T) {
	var logs bytes.Buffer
	reader := failingPolicyTestReader(quotingDriverError(), &logs)

	_, err := reader.Run(context.Background(), "MATCH (n:Secret {token: '"+publicErrorLiteral+"'}) RETURN n", nil)
	if err == nil {
		t.Fatal("Run() error = nil, want the driver error mapped to a public error")
	}
	for name, text := range map[string]string{
		"Error()":  err.Error(),
		"wrapped":  fmt.Errorf("list things: %w", err).Error(),
		"%v":       fmt.Sprintf("%v", err),
		"%+v":      fmt.Sprintf("%+v", err),
		"%s":       fmt.Sprintf("%s", err),
		"joined":   errors.Join(err, errors.New("other")).Error(),
		"http 500": recordServerError(err),
	} {
		if strings.Contains(text, publicErrorLiteral) || strings.Contains(text, "MATCH") ||
			strings.Contains(text, "SyntaxError") {
			t.Fatalf("%s exposed driver text: %q", name, text)
		}
	}
	if !errors.Is(err, ErrGraphQueryFailed) {
		t.Fatalf("errors.Is(err, ErrGraphQueryFailed) = false for %v", err)
	}
	var driverErr *neo4jdriver.Neo4jError
	if !errors.As(err, &driverErr) {
		t.Fatal("the driver cause must stay reachable through errors.As for classification")
	}

	logged := logs.String()
	if !strings.Contains(logged, "query.graph_read.error") {
		t.Fatalf("no operator log for the failed read; logs = %q", logged)
	}
	if strings.Contains(logged, publicErrorLiteral) {
		t.Fatalf("operator log exposed the statement literal: %q", logged)
	}
	if !strings.Contains(logged, "<REDACTED>") {
		t.Fatalf("operator log carries no redacted driver detail: %q", logged)
	}
}

// TestNeo4jReaderKeepsAvailabilityMappings pins that the new public error does
// not swallow the existing 503/504 classes.
func TestNeo4jReaderKeepsAvailabilityMappings(t *testing.T) {
	reader := failingPolicyTestReader(&neo4jdriver.ConnectivityError{Inner: errors.New("refused")}, nil)
	_, err := reader.Run(context.Background(), "RETURN 1", nil)
	if !errors.Is(err, ErrGraphUnavailable) || errors.Is(err, ErrGraphQueryFailed) {
		t.Fatalf("connectivity error = %v, want ErrGraphUnavailable and not ErrGraphQueryFailed", err)
	}
}

// TestInfraSearchFallbackDoesNotEchoDriverText drives the #7065 review F3 site
// end to end: a real Neo4jReader behind the infra search handler must answer a
// graph failure with a stable 500 body that carries no statement text.
func TestInfraSearchFallbackDoesNotEchoDriverText(t *testing.T) {
	reader := failingPolicyTestReader(quotingDriverError(), nil)
	handler := &InfraHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, accept := range []string{"", "application/eshu.envelope+json"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", strings.NewReader(`{"query":"api"}`))
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("accept=%q status = %d, want 500; body=%s", accept, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if strings.Contains(body, publicErrorLiteral) || strings.Contains(body, "MATCH") ||
			strings.Contains(body, "SyntaxError") || strings.Contains(body, "Invalid input") {
			t.Fatalf("accept=%q 500 body exposed driver text: %s", accept, body)
		}
	}
}

// recordServerError renders err the way the plain 5xx fallback does, so the
// table above also proves the response body, not only the string form.
func recordServerError(err error) string {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusInternalServerError, err.Error())
	return w.Body.String()
}
