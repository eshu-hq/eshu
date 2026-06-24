// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

type hostedReadinessGraph struct {
	repositoryCount int
	err             error
}

func (g hostedReadinessGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, errors.New("unexpected Run call")
}

func (g hostedReadinessGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if g.err != nil {
		return nil, g.err
	}
	return map[string]any{"count": g.repositoryCount}, nil
}

func TestStatusHandlerHostedReadinessFailsClosedForEmptyState(t *testing.T) {
	t.Parallel()

	payload := hostedReadinessPayload(t, statuspkg.RawSnapshot{
		AsOf: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	}, hostedReadinessGraph{})

	if got, want := payload["state"], "not_ready"; got != want {
		t.Fatalf("state = %#v, want %#v; payload=%#v", got, want, payload)
	}
	requireFailureClass(t, payload, "empty_state")
	requireFailureClass(t, payload, "collector_instances_missing")
}

func TestStatusHandlerHostedReadinessReportsPartialRolloutAndStaleQueue(t *testing.T) {
	t.Parallel()

	payload := hostedReadinessPayload(t, statuspkg.RawSnapshot{
		AsOf: time.Date(2026, 6, 9, 12, 5, 0, 0, time.UTC),
		Queue: statuspkg.QueueSnapshot{
			Total:                4,
			Outstanding:          2,
			Pending:              2,
			OldestOutstandingAge: 15 * time.Minute,
		},
		ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 1},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{InstanceID: "collector:terraform-state", CollectorKind: "terraform_state", Enabled: true, ClaimsEnabled: true},
			},
		},
	}, hostedReadinessGraph{repositoryCount: 1})

	if got, want := payload["state"], "not_ready"; got != want {
		t.Fatalf("state = %#v, want %#v; payload=%#v", got, want, payload)
	}
	requireFailureClass(t, payload, "queue_stalled")
	requireFailureClass(t, payload, "queue_not_drained")
}

func TestStatusHandlerHostedReadinessReportsDeadLettersAndGraphUnavailable(t *testing.T) {
	t.Parallel()

	payload := hostedReadinessPayload(t, statuspkg.RawSnapshot{
		AsOf: time.Date(2026, 6, 9, 12, 10, 0, 0, time.UTC),
		Queue: statuspkg.QueueSnapshot{
			Total:      3,
			DeadLetter: 1,
			Failed:     1,
		},
		ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 1},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{InstanceID: "collector:package-registry", CollectorKind: "package_registry", Enabled: true, ClaimsEnabled: true},
			},
		},
	}, hostedReadinessGraph{err: errors.New("bolt unavailable")})

	if got, want := payload["state"], "not_ready"; got != want {
		t.Fatalf("state = %#v, want %#v; payload=%#v", got, want, payload)
	}
	requireFailureClass(t, payload, "dead_lettered_work")
	requireFailureClass(t, payload, "graph_unavailable")
}

func TestStatusHandlerHostedReadinessReportsCollectorGenerationDeadLetters(t *testing.T) {
	t.Parallel()

	payload := hostedReadinessPayload(t, statuspkg.RawSnapshot{
		AsOf: time.Date(2026, 6, 12, 19, 5, 0, 0, time.UTC),
		CollectorGenerationDeadLetters: statuspkg.CollectorGenerationDeadLetterSnapshot{
			DeadLetter:          0,
			ReplayRequested:     1,
			ReplayAttempts:      1,
			OldestDeadLetterAge: 5 * time.Minute,
		},
		ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 1},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{InstanceID: "collector:git", CollectorKind: "git", Enabled: true},
			},
		},
	}, hostedReadinessGraph{repositoryCount: 1})

	if got, want := payload["state"], "not_ready"; got != want {
		t.Fatalf("state = %#v, want %#v; payload=%#v", got, want, payload)
	}
	requireFailureClass(t, payload, "collector_generation_dead_lettered")
}

func TestStatusHandlerHostedReadinessReportsHealthyConvergence(t *testing.T) {
	t.Parallel()

	payload := hostedReadinessPayload(t, statuspkg.RawSnapshot{
		AsOf:          time.Date(2026, 6, 9, 12, 15, 0, 0, time.UTC),
		ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 2, Unchanged: 2},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{InstanceID: "collector:aws-cloud", CollectorKind: "aws_cloud", Enabled: true, ClaimsEnabled: true},
			},
			CompletenessCounts: []statuspkg.NamedCount{{Name: "completed", Count: 1}},
		},
	}, hostedReadinessGraph{repositoryCount: 2})

	if got, want := payload["state"], "ready"; got != want {
		t.Fatalf("state = %#v, want %#v; payload=%#v", got, want, payload)
	}
	if failures := payload["failure_classes"].([]any); len(failures) != 0 {
		t.Fatalf("failure_classes = %#v, want empty", failures)
	}
	checks := payload["checks"].([]any)
	if len(checks) < 6 {
		t.Fatalf("checks len = %d, want at least 6; payload=%#v", len(checks), payload)
	}
}

func TestStatusHandlerHostedReadinessTextSummary(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		Neo4j: hostedReadinessGraph{repositoryCount: 1},
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf:          time.Date(2026, 6, 9, 12, 20, 0, 0, time.UTC),
				ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 1},
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{
						{InstanceID: "collector:jira", CollectorKind: "jira", Enabled: true, ClaimsEnabled: true},
					},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/hosted-readiness?format=text", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hosted readiness: ready") {
		t.Fatalf("body missing ready summary: %s", body)
	}
	if !strings.Contains(body, "query_readback: pass") {
		t.Fatalf("body missing query readback check: %s", body)
	}
}

func hostedReadinessPayload(
	t *testing.T,
	snapshot statuspkg.RawSnapshot,
	graph GraphQuery,
) map[string]any {
	t.Helper()
	handler := &StatusHandler{
		Neo4j: graph,
		StatusReader: fakeStatusReader{
			snapshot: snapshot,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/hosted-readiness", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body=%s", err, rec.Body.String())
	}
	return payload
}

func requireFailureClass(t *testing.T, payload map[string]any, want string) {
	t.Helper()
	for _, got := range payload["failure_classes"].([]any) {
		if got == want {
			return
		}
	}
	t.Fatalf("failure_classes = %#v, want %q", payload["failure_classes"], want)
}
