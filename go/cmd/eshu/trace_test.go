// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestTraceServiceCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"trace", "service", "checkout"})
	if err != nil {
		t.Fatalf("rootCmd.Find(trace service) error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "service" {
		t.Fatalf("resolved command = %#v, want service command", cmd)
	}
	for _, name := range []string{"json", "repo", "env", "service-id", "service-url", "api-key", "profile"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("trace service flag %q missing", name)
		}
	}
}

func TestFetchTraceServiceStoryRequestsCanonicalEnvelope(t *testing.T) {
	var gotAccept string
	var gotPath string
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.EscapedPath()
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"service_identity":{"service_name":"checkout"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	got, err := fetchTraceServiceStory(client, "checkout api", traceServiceOptions{
		Repo:        "checkout-service",
		Environment: "prod",
		ServiceID:   "workload:checkout",
	})
	if err != nil {
		t.Fatalf("fetchTraceServiceStory() error = %v, want nil", err)
	}
	if gotAccept != eshuEnvelopeMIMEType {
		t.Fatalf("Accept = %q, want %q", gotAccept, eshuEnvelopeMIMEType)
	}
	if gotPath != "/api/v0/services/checkout%20api/story" {
		t.Fatalf("path = %q, want escaped service story path", gotPath)
	}
	for key, want := range map[string]string{
		"repo":        "checkout-service",
		"environment": "prod",
		"service_id":  "workload:checkout",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Fatalf("query[%s] = %q, want %q; full query=%q", key, got, want, gotQuery.Encode())
		}
	}
	if got.Data == nil {
		t.Fatalf("Data = nil, want service story data")
	}
}

func TestRunTraceServiceReturnsPartialExitAndRendersOperationalSummary(t *testing.T) {
	reset := stubTraceServiceFetch(t, traceServiceEnvelope{
		Data: sampleTraceServiceStoryData(),
		Truth: map[string]any{
			"level":      "exact",
			"capability": "platform_impact.context_overview",
			"freshness":  map[string]any{"state": "fresh"},
		},
	})
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestTraceServiceCommand()
	cmd.SetOut(out)

	err := runTraceService(cmd, []string{"checkout"})
	if err == nil {
		t.Fatal("runTraceService() error = nil, want partial trace exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 5; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}

	output := out.String()
	for _, want := range []string{
		"Service: checkout",
		"Repository: repo:checkout-service (checkout-service)",
		"Code to runtime:",
		"Trace status: partial",
		"code_entrypoints: derived (1 evidence) via api_surface_or_entrypoints",
		"deployment_config: derived",
		"runtime: exact",
		"cloud_dependencies: missing_evidence via cloud_resource_evidence",
		"Deployment lanes: 1",
		"Runtime instances: 2",
		"Coverage: partial",
		"What to worry about:",
		"deployment evidence is partial",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("trace output missing %q:\n%s", want, output)
		}
	}
}

func TestRunTraceServiceReturnsStaleExitAndRendersTruthFreshness(t *testing.T) {
	reset := stubTraceServiceFetch(t, traceServiceEnvelope{
		Data: sampleCompleteTraceServiceStoryData(),
		Truth: map[string]any{
			"level":     "exact",
			"profile":   "production",
			"reason":    "service-story generation is behind the latest index",
			"freshness": map[string]any{"state": "stale"},
		},
	})
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestTraceServiceCommand()
	cmd.SetOut(out)

	err := runTraceService(cmd, []string{"checkout"})
	if err == nil {
		t.Fatal("runTraceService() error = nil, want stale trace exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 4; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}

	output := out.String()
	for _, want := range []string{
		"Truth freshness: stale",
		"Freshness detail: service-story generation is behind the latest index",
		"Trace status: complete",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("trace output missing %q:\n%s", want, output)
		}
	}
}

func TestRunTraceServiceJSONPassesCanonicalEnvelope(t *testing.T) {
	reset := stubTraceServiceFetch(t, traceServiceEnvelope{
		Data: sampleCompleteTraceServiceStoryData(),
		Truth: map[string]any{
			"level":     "derived",
			"freshness": map[string]any{"state": "fresh"},
		},
	})
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestTraceServiceCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	if err := runTraceService(cmd, []string{"checkout"}); err != nil {
		t.Fatalf("runTraceService() error = %v, want nil", err)
	}

	var payload traceServiceEnvelope
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if payload.Error != nil {
		t.Fatalf("payload.Error = %#v, want nil", payload.Error)
	}
	if got := payload.Data["service_identity"].(map[string]any)["service_name"]; got != "checkout" {
		t.Fatalf("service_name = %#v, want checkout", got)
	}
}

func TestRunTraceServiceJSONReturnsPartialExitAfterWritingEnvelope(t *testing.T) {
	reset := stubTraceServiceFetch(t, traceServiceEnvelope{
		Data: sampleTraceServiceStoryData(),
		Truth: map[string]any{
			"level":     "derived",
			"freshness": map[string]any{"state": "fresh"},
		},
	})
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestTraceServiceCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	err := runTraceService(cmd, []string{"checkout"})
	if err == nil {
		t.Fatal("runTraceService() error = nil, want partial trace exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 5; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
	var payload traceServiceEnvelope
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	trace := payload.Data["code_to_runtime_trace"].(map[string]any)
	if got, want := trace["status"], "partial"; got != want {
		t.Fatalf("code_to_runtime_trace.status = %#v, want %#v", got, want)
	}
}

func TestRunTraceServiceJSONIncludesNullTruthForTransportFailure(t *testing.T) {
	original := traceFetchServiceStory
	traceFetchServiceStory = func(_ *APIClient, _ string, _ traceServiceOptions) (traceServiceEnvelope, error) {
		return traceServiceEnvelope{}, errors.New("connection refused")
	}
	defer func() {
		traceFetchServiceStory = original
	}()

	out := &bytes.Buffer{}
	cmd := newTestTraceServiceCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	err := runTraceService(cmd, []string{"checkout"})
	if err == nil {
		t.Fatal("runTraceService() error = nil, want transport failure")
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if _, ok := payload["truth"]; !ok {
		t.Fatalf("payload missing truth field: %#v", payload)
	}
	if payload["truth"] != nil {
		t.Fatalf("payload[truth] = %#v, want nil", payload["truth"])
	}
}

func TestRunTraceServiceReturnsTypedExitErrorForUnsupportedCapability(t *testing.T) {
	reset := stubTraceServiceFetch(t, traceServiceEnvelope{
		Error: &traceServiceError{
			Code:       "unsupported_capability",
			Message:    "service story requires authoritative platform context truth",
			Capability: "platform_impact.context_overview",
		},
	})
	defer reset()

	err := runTraceService(newTestTraceServiceCommand(), []string{"checkout"})
	if err == nil {
		t.Fatal("runTraceService() error = nil, want unsupported capability")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 6; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
}

func TestRunTraceServiceRendersAmbiguousCandidates(t *testing.T) {
	reset := stubTraceServiceFetch(t, traceServiceEnvelope{
		Error: &traceServiceError{
			Code:    "ambiguous",
			Message: "service selector \"checkout\" matched multiple services; add --service-id, --repo, or --env",
			Details: map[string]any{
				"status": "ambiguous",
				"candidates": []any{
					map[string]any{"service_id": "workload:checkout-api", "service_name": "checkout", "repo_id": "repo-checkout-api", "environment": "prod"},
					map[string]any{"service_id": "workload:checkout-worker", "service_name": "checkout", "repo_id": "repo-checkout-worker", "environment": "qa"},
				},
			},
		},
	})
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestTraceServiceCommand()
	cmd.SetOut(out)

	err := runTraceService(cmd, []string{"checkout"})
	if err == nil {
		t.Fatal("runTraceService() error = nil, want ambiguous selector exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 3; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
	output := out.String()
	for _, want := range []string{
		"Service selector is ambiguous",
		"workload:checkout-api",
		"repo-checkout-worker",
		"Add --service-id, --repo, or --env",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("ambiguous output missing %q:\n%s", want, output)
		}
	}
}

func newTestTraceServiceCommand() *cobra.Command {
	cmd := &cobra.Command{}
	addTraceServiceFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}

func stubTraceServiceFetch(t *testing.T, envelope traceServiceEnvelope) func() {
	t.Helper()
	original := traceFetchServiceStory
	traceFetchServiceStory = func(_ *APIClient, selector string, opts traceServiceOptions) (traceServiceEnvelope, error) {
		if selector != "checkout" {
			t.Fatalf("selector = %q, want checkout", selector)
		}
		return envelope, nil
	}
	return func() {
		traceFetchServiceStory = original
	}
}

func sampleTraceServiceStoryData() map[string]any {
	return map[string]any{
		"service_identity": map[string]any{
			"service_name":           "checkout",
			"workload_id":            "workload:checkout",
			"repo_id":                "repo:checkout-service",
			"repo_name":              "checkout-service",
			"materialization_status": "materialized",
			"query_basis":            "workload_graph",
			"limitations":            []any{"deployment evidence is partial"},
		},
		"deployment_lanes": []any{
			map[string]any{"lane": "gitops", "summary": "Kubernetes deployment evidence found"},
		},
		"code_to_runtime_trace": map[string]any{
			"status":           "partial",
			"missing_segments": []any{"cloud_dependencies"},
			"segments": []any{
				map[string]any{"name": "code_entrypoints", "status": "derived", "evidence_count": float64(1), "basis": "api_surface_or_entrypoints"},
				map[string]any{"name": "deployment_config", "status": "derived", "evidence_count": float64(1), "basis": "deployment_evidence"},
				map[string]any{"name": "runtime", "status": "exact", "evidence_count": float64(2), "basis": "materialized_workload_instances"},
				map[string]any{"name": "cloud_dependencies", "status": "missing_evidence", "evidence_count": float64(0), "basis": "cloud_resource_evidence"},
			},
		},
		"runtime_instances": []any{
			map[string]any{"environment": "prod", "name": "checkout-prod"},
			map[string]any{"environment": "qa", "name": "checkout-qa"},
		},
		"upstream_dependencies": []any{
			map[string]any{"name": "payments"},
		},
		"downstream_consumers": []any{
			map[string]any{"name": "portal"},
		},
		"investigation": map[string]any{
			"coverage_summary": map[string]any{
				"state":  "partial",
				"reason": "evidence was found, but exhaustive coverage is not proven",
			},
		},
	}
}

func sampleCompleteTraceServiceStoryData() map[string]any {
	data := sampleTraceServiceStoryData()
	trace := data["code_to_runtime_trace"].(map[string]any)
	trace["status"] = "complete"
	trace["missing_segments"] = []any{}
	for _, item := range trace["segments"].([]any) {
		segment := item.(map[string]any)
		if segment["name"] == "cloud_dependencies" {
			segment["status"] = "derived"
			segment["evidence_count"] = float64(1)
		}
	}
	identity := data["service_identity"].(map[string]any)
	identity["limitations"] = []any{}
	investigation := data["investigation"].(map[string]any)
	investigation["coverage_summary"] = map[string]any{
		"state":  "complete",
		"reason": "all code-to-runtime segments have evidence",
	}
	return data
}
