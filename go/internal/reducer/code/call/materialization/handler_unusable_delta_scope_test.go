// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projfailure "github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// unusableDeltaScopeEnvelopes is a delta-generation repository with resolvable
// call rows whose changed paths cannot be qualified into a file scope. The
// repository fields mirror handler_delta_partition_test.go so the repository
// decodes and gets a projection context.
func unusableDeltaScopeEnvelopes(repository map[string]any) []facts.Envelope {
	payload := map[string]any{
		"repo_id":          "repo-a",
		"source_run_id":    "run-a",
		"graph_id":         "repo-a",
		"graph_kind":       "repository",
		"name":             "repo-a",
		"delta_generation": true,
	}
	for key, value := range repository {
		payload[key] = value
	}
	return []facts.Envelope{
		{FactKind: "repository", Payload: payload},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "caller.py",
			"parsed_file_data": map[string]any{
				"path": "caller.py",
				"functions": []any{
					map[string]any{"name": "handle", "line_number": 3, "uid": "entity:handle"},
				},
				"function_calls_scip": []any{
					map[string]any{
						"caller_file": "caller.py", "caller_line": 3, "caller_symbol": "pkg/caller#handle().",
						"callee_file": "callee.py", "callee_line": 1, "callee_symbol": "pkg/callee#callee().",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "callee.py",
			"parsed_file_data": map[string]any{
				"path": "callee.py",
				"functions": []any{
					map[string]any{"name": "callee", "line_number": 1, "uid": "entity:callee"},
				},
			},
		}},
	}
}

// TestCodeCallMaterializationHandlerFailsClosedOnUnusableDeltaScope is the
// #10023 gap 1 regression. Before the fix, a handler fed a delta generation
// with an unusable scope succeeded and wrote an unscoped whole-repository
// refresh plus the delta's rows, so the projection runner would delete every
// unchanged file's CALLS edge and restore only the delta's. The fix returns a
// self-classifying, non-retryable error before the symbol load and before any
// intent is written, so the reducer queue dead-letters the work item on its
// first attempt and the graph keeps its existing edges until the next full
// generation.
//
// Production does not reach this path yet: the git collector emits no
// code_call_materialization marker on a delta generation until #10049 lands,
// so the fence is a guard placed ahead of that change.
func TestCodeCallMaterializationHandlerFailsClosedOnUnusableDeltaScope(t *testing.T) {
	cases := map[string]struct {
		repository map[string]any
		reason     string
	}{
		"no local path": {
			repository: map[string]any{"delta_relative_paths": []string{"caller.py"}},
			reason:     "missing_local_path",
		},
		"unnormalizable changed path": {
			repository: map[string]any{"path": "/repo", "delta_relative_paths": []string{"caller.py", "../outside.py"}},
			reason:     "unnormalizable_path",
		},
		"no changed paths": {
			repository: map[string]any{"path": "/repo"},
			reason:     "no_changed_paths",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previous)

			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			envelopes := unusableDeltaScopeEnvelopes(tc.repository)
			if len(shared.ReferencedSymbolKeys(envelopes)) == 0 {
				t.Fatal("fixture references no symbol keys, so the symbol-load ordering check would be vacuous")
			}
			now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
			writer := &recordingCodeCallIntentWriter{}
			handler := Handler{
				FactLoader:   &fenceOrderFactLoader{t: t, envelopes: envelopes},
				IntentWriter: writer,
				Instruments:  inst,
			}

			_, err = handler.Handle(context.Background(), reducercontract.Intent{
				IntentID:     "intent-code-call-unusable-delta",
				ScopeID:      "scope-1",
				GenerationID: "gen-delta",
				SourceSystem: "git",
				Domain:       reducercontract.DomainCodeCallMaterialization,
				EnqueuedAt:   now,
				AvailableAt:  now,
				Status:       reducercontract.IntentStatusPending,
			})
			if got := len(writer.rows); got != 0 {
				t.Fatalf("IntentWriter received %d rows, want 0 (a delta-only whole-repository rewrite)", got)
			}
			if err == nil {
				t.Fatal("Handle() error = nil, want the unusable delta scope failure")
			}
			if reducercontract.IsRetryable(err) {
				t.Fatalf("IsRetryable(%v) = true, want false (a retry rebuilds the same unusable scope)", err)
			}
			var classified interface{ FailureClass() string }
			if !errors.As(err, &classified) {
				t.Fatalf("error %T does not self-classify", err)
			}
			if got, want := classified.FailureClass(), DeltaScopeUnusableFailureClass; got != want {
				t.Fatalf("FailureClass() = %q, want %q", got, want)
			}

			var inputInvalid *projfailure.InputValidationError
			if !errors.As(err, &inputInvalid) {
				t.Fatalf("error %T does not wrap an InputValidationError, so dead-letter triage falls back to projection_bug", err)
			}

			assertDeltaScopeUnusableCounter(t, reader, tc.reason)
			assertDeltaScopeUnusableLog(t, logs.Bytes(), tc.reason)
		})
	}
}

// fenceOrderFactLoader serves the fixture and fails the test if the handler
// reaches the cross-repository symbol load: the fence must run before that
// load, so a fenced work item costs no symbol query.
type fenceOrderFactLoader struct {
	t         *testing.T
	envelopes []facts.Envelope
}

func (l *fenceOrderFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

func (l *fenceOrderFactLoader) LoadActiveCodeCallSymbolDefinitionFacts(context.Context, []string) ([]facts.Envelope, error) {
	l.t.Error("LoadActiveCodeCallSymbolDefinitionFacts ran: the unusable delta scope fence must run before the symbol load")
	return nil, nil
}

// TestCodeCallMaterializationHandlerFencesOnlyProjectedRepositories pins
// the projection-context filter: an unusable delta repository with no
// projection context gets no intents anyway, so it must not fail the work
// item for a sibling repository whose delta scope is usable.
func TestCodeCallMaterializationHandlerFencesOnlyProjectedRepositories(t *testing.T) {
	t.Parallel()

	envelopes := unusableDeltaScopeEnvelopes(map[string]any{"path": "/repo", "delta_relative_paths": []string{"caller.py"}})
	envelopes = append(envelopes, facts.Envelope{FactKind: "repository", Payload: map[string]any{
		// No source_run_id, so no projection context is built for repo-b.
		"repo_id": "repo-b", "graph_id": "repo-b", "graph_kind": "repository", "name": "repo-b",
		"delta_generation": true, "delta_relative_paths": []string{"main.go"},
	}})
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{FactLoader: &stubFactLoader{envelopes: envelopes}, IntentWriter: writer}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-code-call-sibling-unusable",
		ScopeID:      "scope-1",
		GenerationID: "gen-delta",
		SourceSystem: "git",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want success: repo-b has no projection context", err)
	}
	if len(writer.rows) == 0 {
		t.Fatal("IntentWriter received no rows for repo-a's usable delta scope")
	}
	for _, row := range writer.rows {
		if row.RepositoryID == "repo-b" {
			t.Fatalf("repo-b (no projection context) got an intent: %#v", row)
		}
	}
}

func assertDeltaScopeUnusableCounter(t *testing.T, reader sdkmetric.Reader, reason string) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_code_call_delta_scope_unusable_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				if value, _ := point.Attributes.Value(attribute.Key("reason")); value.AsString() == reason && point.Value == 1 {
					return
				}
			}
			t.Fatalf("no data point with reason=%q value=1: %#v", reason, sum.DataPoints)
		}
	}
	t.Fatal("eshu_dp_code_call_delta_scope_unusable_total not recorded")
}

func assertDeltaScopeUnusableLog(t *testing.T, logs []byte, reason string) {
	t.Helper()
	for _, line := range bytes.Split(logs, []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(line, &record) != nil || record["msg"] != "code call delta scope unusable, failing closed" {
			continue
		}
		if record["level"] != "ERROR" {
			t.Fatalf("log level = %v, want ERROR", record["level"])
		}
		if record["failure_class"] != DeltaScopeUnusableFailureClass {
			t.Fatalf("failure_class = %v, want %q", record["failure_class"], DeltaScopeUnusableFailureClass)
		}
		repos, _ := record["unusable_delta_scopes"].([]any)
		if len(repos) != 1 || repos[0] != "repo-a="+reason {
			t.Fatalf("unusable_delta_scopes = %#v, want [repo-a=%s]", record["unusable_delta_scopes"], reason)
		}
		return
	}
	t.Fatalf("no unusable delta scope log line in %s", logs)
}

// TestCodeCallMaterializationHandlerFullGenerationFallbackStillSucceeds pins
// the kept-green half: a full-generation repository with no usable file scope
// still emits its whole-repository refresh and succeeds.
func TestCodeCallMaterializationHandlerFullGenerationFallbackStillSucceeds(t *testing.T) {
	t.Parallel()

	envelopes := unusableDeltaScopeEnvelopes(map[string]any{"delta_relative_paths": []string{"caller.py"}})
	delete(envelopes[0].Payload, "delta_generation")
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{FactLoader: &stubFactLoader{envelopes: envelopes}, IntentWriter: writer}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-code-call-full-fallback",
		ScopeID:      "scope-1",
		GenerationID: "gen-full",
		SourceSystem: "git",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	refreshes := 0
	for _, row := range writer.rows {
		if row.Payload["intent_type"] != "repo_refresh" || row.Payload["evidence_source"] != "reducer/code-call-refresh" {
			continue
		}
		refreshes++
		if _, scoped := row.Payload["delta_projection"]; scoped {
			t.Fatalf("full-generation refresh carries delta_projection: %#v", row.Payload)
		}
	}
	if refreshes != 1 {
		t.Fatalf("code-call refresh rows = %d, want 1 (rows %#v)", refreshes, writer.rows)
	}
}
