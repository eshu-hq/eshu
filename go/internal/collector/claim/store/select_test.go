// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// postgresStoreStub stands in for the Postgres store the caller injects. Its
// pointer identity proves New returns the injected value unchanged.
type postgresStoreStub struct{ name string }

func (*postgresStoreStub) ClaimNextEligible(context.Context, workflow.ClaimSelector, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
	return workflow.WorkItem{}, workflow.Claim{}, false, nil
}

func (*postgresStoreStub) HeartbeatClaim(context.Context, workflow.ClaimMutation) error { return nil }

func (*postgresStoreStub) CompleteClaim(context.Context, workflow.ClaimMutation) error { return nil }

func (*postgresStoreStub) ReleaseClaim(context.Context, workflow.ClaimMutation) error { return nil }

func (*postgresStoreStub) FailClaimRetryable(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (*postgresStoreStub) FailClaimTerminal(context.Context, workflow.ClaimMutation) error {
	return nil
}

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestNewUnsetKeepsInjectedPostgresStore(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "   ", "\t\n"} {
		pg := &postgresStoreStub{name: "core"}
		selection, err := store.New(context.Background(), store.Options{
			Service:  "collector-test",
			Getenv:   envOf(map[string]string{store.EnvClaimPlaneURL: value}),
			Postgres: pg,
		})
		if err != nil {
			t.Fatalf("New(%q) error = %v, want nil", value, err)
		}
		if selection.Store != pg {
			t.Fatalf("New(%q) store = %#v, want the injected Postgres store unchanged", value, selection.Store)
		}
		if selection.Backend != store.BackendPostgres || selection.Endpoint != "" {
			t.Fatalf("New(%q) = (%s, %q), want (postgres, \"\")", value, selection.Backend, selection.Endpoint)
		}
	}
}

func TestNewValidURLSelectsClaimPlaneClient(t *testing.T) {
	t.Parallel()
	selection, err := store.New(context.Background(), store.Options{
		Service:  "collector-test",
		Getenv:   envOf(map[string]string{store.EnvClaimPlaneURL: " http://eshu-workflow-claim-plane:8080/ "}),
		Postgres: &postgresStoreStub{},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, ok := selection.Store.(*workflow.ClaimPlaneClient); !ok {
		t.Fatalf("store = %T, want *workflow.ClaimPlaneClient", selection.Store)
	}
	if selection.Backend != store.BackendClaimPlane {
		t.Fatalf("backend = %s, want claim_plane", selection.Backend)
	}
	if selection.Endpoint != "http://eshu-workflow-claim-plane:8080" {
		t.Fatalf("endpoint = %q, want scheme and host only", selection.Endpoint)
	}
}

func TestNewInvalidURLFailsNamingVariableWithoutLeakingCredentials(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"bad scheme":        "ftp://claim-plane:8080",
		"no scheme":         "claim-plane:8080",
		"no host":           "http://",
		"credentials":       "https://svc-user:s3cret-pass@claim-plane:8080",
		"query":             "http://claim-plane:8080?token=abc",
		"fragment":          "http://claim-plane:8080#frag",
		"path":              "http://claim-plane:8080/workflow.v1",
		"path with token":   "http://claim-plane:8080/tok-9f8e7d",
		"unparseable":       "http://claim plane:8080",
		"control character": "http://claim-plane:8080/\x7f",
		"port out of range": "http://claim-plane:99999",
		"port zero":         "http://claim-plane:0",
		"non-ASCII host":    "http://\u0441laim-plane:8080",
		"NBSP in host":      "http://claim\u00a0plane:8080",
		"tab in host":       "http://claim\tplane:8080",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := store.New(context.Background(), store.Options{
				Service:  "collector-test",
				Getenv:   envOf(map[string]string{store.EnvClaimPlaneURL: raw}),
				Postgres: &postgresStoreStub{},
			})
			if err == nil {
				t.Fatalf("New(%q) error = nil, want a startup error", raw)
			}
			if !strings.Contains(err.Error(), store.EnvClaimPlaneURL) {
				t.Fatalf("error %q does not name %s", err, store.EnvClaimPlaneURL)
			}
			for _, secret := range []string{"s3cret-pass", "svc-user", "token=abc", "tok-9f8e7d", "99999", "\u0441laim", "claim\u00a0plane"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error %q leaks %q", err, secret)
				}
			}
		})
	}
}

func TestNewRefusesClaimPlaneForCoreOnlyCollector(t *testing.T) {
	t.Parallel()
	_, err := store.New(context.Background(), store.Options{
		Service:        "collector-vulnerability-intelligence",
		Getenv:         envOf(map[string]string{store.EnvClaimPlaneURL: "http://claim-plane:8080"}),
		Postgres:       &postgresStoreStub{},
		CoreOnlyReason: "reads workflow_runs.requested_scope_set from core Postgres",
	})
	if !errors.Is(err, store.ErrClaimPlaneUnsupported) {
		t.Fatalf("error = %v, want ErrClaimPlaneUnsupported", err)
	}
	for _, want := range []string{store.EnvClaimPlaneURL, "collector-vulnerability-intelligence", "requested_scope_set"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	// The same collector with the variable unset keeps its Postgres store.
	pg := &postgresStoreStub{}
	selection, err := store.New(context.Background(), store.Options{
		Service: "collector-vulnerability-intelligence", Getenv: envOf(nil),
		Postgres: pg, CoreOnlyReason: "reads workflow_runs.requested_scope_set from core Postgres",
	})
	if err != nil || selection.Store != pg {
		t.Fatalf("unset core-only New() = (%#v, %v), want injected store, nil", selection.Store, err)
	}
}

func TestNewRequiresPostgresStoreAndGetenv(t *testing.T) {
	t.Parallel()
	if _, err := store.New(context.Background(), store.Options{Service: "x", Getenv: envOf(nil)}); err == nil {
		t.Fatal("New() without a Postgres store error = nil, want error")
	}
	if _, err := store.New(context.Background(), store.Options{Service: "x", Postgres: &postgresStoreStub{}}); err == nil {
		t.Fatal("New() without Getenv error = nil, want error")
	}
}

func TestNewClaimPlaneClientHasBoundedTimeout(t *testing.T) {
	t.Parallel()
	if store.RequestTimeout <= 0 || store.RequestTimeout >= workflow.DefaultHeartbeatInterval() {
		t.Fatalf("RequestTimeout = %v, want positive and below the %v default heartbeat interval",
			store.RequestTimeout, workflow.DefaultHeartbeatInterval())
	}
}

func TestNewEmitsSelectionLogAndMetricsAndInstrumentsRequests(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	selection, err := store.New(context.Background(), store.Options{
		Service:     "collector-test",
		Getenv:      envOf(map[string]string{store.EnvClaimPlaneURL: server.URL}),
		Postgres:    &postgresStoreStub{},
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
		Instruments: instruments,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := selection.Store.HeartbeatClaim(context.Background(), workflow.ClaimMutation{}); err != nil {
		t.Fatalf("HeartbeatClaim() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatalf("decode selection log %q: %v", logs.String(), err)
	}
	if record["event_name"] != "collector.workflow_control_store.selected" ||
		record["workflow_control_store"] != "claim_plane" ||
		record["claim_plane_endpoint"] != server.URL {
		t.Fatalf("selection log = %v, want event, backend claim_plane, and endpoint", record)
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]map[string]string{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && len(sum.DataPoints) == 1 {
				labels := map[string]string{}
				for _, kv := range sum.DataPoints[0].Attributes.ToSlice() {
					labels[string(kv.Key)] = kv.Value.AsString()
				}
				got[m.Name] = labels
			}
		}
	}
	if got["eshu_dp_workflow_control_store_selections_total"]["backend_kind"] != "claim_plane" {
		t.Fatalf("selection counter labels = %v, want backend_kind=claim_plane", got["eshu_dp_workflow_control_store_selections_total"])
	}
	request := got["eshu_dp_workflow_claim_plane_requests_total"]
	if request["operation"] != "HeartbeatClaim" || request["outcome"] != "ok" {
		t.Fatalf("request counter labels = %v, want operation=HeartbeatClaim outcome=ok", request)
	}
}

func TestNewPostgresSelectionIsObservable(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	_, err := store.New(context.Background(), store.Options{
		Service:  "collector-test",
		Getenv:   envOf(nil),
		Postgres: &postgresStoreStub{},
		Logger:   slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !strings.Contains(logs.String(), `"workflow_control_store":"postgres"`) {
		t.Fatalf("selection log = %q, want workflow_control_store=postgres", logs.String())
	}
}
