// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/loki"
	"github.com/eshu-hq/eshu/go/internal/collector/prometheusmimir"
	"github.com/eshu-hq/eshu/go/internal/replay/inputtape"
)

// TestLokiRecordReplayProducesIdenticalFacts records the Loki collector against a
// fake endpoint, replays the captured tape with the endpoint shut down, and
// asserts the replayed run produces byte-identical facts with no live
// credential in the tape. This is the R-4 acceptance: a record->replay round
// trip on an HTTP collector reproduces identical facts credential-free.
func TestLokiRecordReplayProducesIdenticalFacts(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{"app", "namespace"}})
		case "/loki/api/v1/label/app/values":
			writeJSON(t, w, map[string]any{"status": "success", "data": []string{"checkout", "billing"}})
		case "/loki/api/v1/series":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data":   []map[string]string{{"app": "checkout", "namespace": "payments"}},
			})
		case "/loki/api/v1/rules":
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte("prod:\n- name: checkout.rules\n  rules:\n  - alert: HighErrors\n    expr: rate({app=\"checkout\"}[5m]) > 0\n"))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(handler))

	target := loki.TargetConfig{
		ScopeID:                "loki:tenant:prod",
		InstanceID:             "loki-prod",
		BaseURL:                server.URL,
		Token:                  "live-loki-token-DO-NOT-LEAK",
		TenantID:               "tenant-prod",
		ResourceLimit:          50,
		LabelValueNames:        []string{"app"},
		MaxLabelValuesPerLabel: 5,
	}

	// RECORD: wrap the tape RoundTripper as the collector's HTTP client.
	recorder := inputtape.New(inputtape.Config{
		RedactHeaders:       []string{"X-Scope-OrgID"},
		VolatileQueryParams: []string{"end", "start"},
	})
	recClient, err := loki.NewHTTPClient(loki.HTTPClientConfig{
		BaseURL: server.URL,
		Client:  &http.Client{Transport: recorder},
	})
	if err != nil {
		t.Fatalf("record client: %v", err)
	}
	liveResult, err := recClient.CollectObservedMetadata(context.Background(), target)
	if err != nil {
		t.Fatalf("record collection: %v", err)
	}

	tape := recorder.Tape("loki")
	canonical, err := inputtape.MarshalTape(tape)
	if err != nil {
		t.Fatalf("marshal tape: %v", err)
	}
	assertCredentialFree(t, canonical, "live-loki-token-DO-NOT-LEAK", "tenant-prod")

	// REPLAY: shut the endpoint down, then collect again from the tape.
	server.Close()

	replayer, err := inputtape.NewReplayer(tape, inputtape.Config{
		RedactHeaders:       []string{"X-Scope-OrgID"},
		VolatileQueryParams: []string{"end", "start"},
	})
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}
	replayClient, err := loki.NewHTTPClient(loki.HTTPClientConfig{
		BaseURL: server.URL,
		Client:  &http.Client{Transport: replayer},
	})
	if err != nil {
		t.Fatalf("replay client: %v", err)
	}
	replayResult, err := replayClient.CollectObservedMetadata(context.Background(), target)
	if err != nil {
		t.Fatalf("replay collection (no server running): %v", err)
	}

	// ObservedAt is stamped by the collector from its own wall clock on each run
	// (it is not sourced from the tape), so normalize it before comparing facts.
	liveResult.ObservedAt = time.Time{}
	replayResult.ObservedAt = time.Time{}
	if !reflect.DeepEqual(liveResult, replayResult) {
		t.Fatalf("replay facts differ from live facts:\nlive=%+v\nreplay=%+v", liveResult, replayResult)
	}
	if len(replayResult.Signals) == 0 {
		t.Fatalf("replay produced no signals; record/replay is vacuous")
	}
}

// TestPrometheusMimirRecordReplayProducesIdenticalFacts is the second HTTP
// collector required by R-4 acceptance.
func TestPrometheusMimirRecordReplayProducesIdenticalFacts(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/targets":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"activeTargets": []map[string]any{{
						"scrapePool": "kubernetes-pods",
						"scrapeUrl":  "http://10.0.0.1:9090/metrics",
						"health":     "up",
						"labels":     map[string]any{"job": "api", "instance": "10.0.0.1"},
					}},
				},
			})
		case "/api/v1/rules":
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"groups": []map[string]any{{
						"name": "slo.rules",
						"file": "/etc/prometheus/rules.yml",
						"rules": []map[string]any{{
							"name":  "ErrorBudgetBurn",
							"type":  "alerting",
							"query": "rate(http_errors[5m]) > 0",
						}},
					}},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(handler))

	target := prometheusmimir.TargetConfig{
		ScopeID:       "prom:cluster:prod",
		InstanceID:    "prom-prod",
		Provider:      prometheusmimir.ProviderPrometheus,
		BaseURL:       server.URL,
		Token:         "live-prom-token-DO-NOT-LEAK",
		ResourceLimit: 50,
	}

	recorder := inputtape.New(inputtape.Config{})
	recClient, err := prometheusmimir.NewHTTPClient(prometheusmimir.HTTPClientConfig{
		BaseURL: server.URL,
		Client:  &http.Client{Transport: recorder},
	})
	if err != nil {
		t.Fatalf("record client: %v", err)
	}
	liveResult, err := recClient.CollectObservedMetadata(context.Background(), target)
	if err != nil {
		t.Fatalf("record collection: %v", err)
	}

	tape := recorder.Tape("prometheusmimir")
	canonical, err := inputtape.MarshalTape(tape)
	if err != nil {
		t.Fatalf("marshal tape: %v", err)
	}
	assertCredentialFree(t, canonical, "live-prom-token-DO-NOT-LEAK")

	server.Close()

	replayer, err := inputtape.NewReplayer(tape, inputtape.Config{})
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}
	replayClient, err := prometheusmimir.NewHTTPClient(prometheusmimir.HTTPClientConfig{
		BaseURL: server.URL,
		Client:  &http.Client{Transport: replayer},
	})
	if err != nil {
		t.Fatalf("replay client: %v", err)
	}
	replayResult, err := replayClient.CollectObservedMetadata(context.Background(), target)
	if err != nil {
		t.Fatalf("replay collection (no server running): %v", err)
	}

	// ObservedAt is stamped by the collector from its own wall clock on each run
	// (it is not sourced from the tape), so normalize it before comparing facts.
	liveResult.ObservedAt = time.Time{}
	replayResult.ObservedAt = time.Time{}
	if !reflect.DeepEqual(liveResult, replayResult) {
		t.Fatalf("replay facts differ from live facts:\nlive=%+v\nreplay=%+v", liveResult, replayResult)
	}
	if len(replayResult.Targets) == 0 || len(replayResult.Rules) == 0 {
		t.Fatalf("replay produced no targets/rules; record/replay is vacuous")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode payload: %v", err)
	}
}

func assertCredentialFree(t *testing.T, tape []byte, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if bytes.Contains(tape, []byte(secret)) {
			t.Fatalf("tape leaked secret %q:\n%s", secret, tape)
		}
	}
}
