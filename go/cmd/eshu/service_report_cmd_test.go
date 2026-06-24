// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const sampleServiceStoryEnvelope = `{
  "data": {
    "service_identity": {
      "service_id": "svc:checkout",
      "service_name": "checkout",
      "kind": "service",
      "repo_id": "repo:checkout",
      "limitations": ["materialization pending for one lane"]
    },
    "entrypoints": [{"x": 1}],
    "network_paths": [{"y": 1}],
    "deployment_lanes": [{"lane_type": "kubernetes"}],
    "result_limits": {"truncated": false}
  },
  "truth": {
    "level": "exact",
    "basis": "authoritative_graph",
    "freshness": {"state": "fresh"}
  }
}`

func runServiceReportCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newServiceReportCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestServiceReportRendersFromEnvelope(t *testing.T) {
	out, err := runServiceReportCmd(t, sampleServiceStoryEnvelope)
	if err != nil {
		t.Fatalf("unexpected error: %v (out=%s)", err, out)
	}
	for _, want := range []string{"checkout", "Service identity", "Supply-chain evidence", "[UNSUPPORTED]", "Suggested investigations:", "unsupported_lane"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestServiceReportJSONComposesSupportedReport(t *testing.T) {
	out, err := runServiceReportCmd(t, sampleServiceStoryEnvelope, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var report struct {
		Schema    string `json:"schema"`
		Supported bool   `json:"supported"`
		Subject   struct {
			ServiceName string `json:"service_name"`
		} `json:"subject"`
		Sections []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"sections"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("output is not valid report JSON: %v\n%s", err, out)
	}
	if report.Schema != "service_intelligence_report.v1" {
		t.Fatalf("schema = %q", report.Schema)
	}
	if !report.Supported || report.Subject.ServiceName != "checkout" {
		t.Fatalf("expected a supported checkout report, got %+v", report)
	}
	if len(report.Sections) != 5 {
		t.Fatalf("expected 5 sections, got %d", len(report.Sections))
	}
}

func TestServiceReportAcceptsBareDossier(t *testing.T) {
	bare := `{"service_identity": {"service_id": "svc:x", "service_name": "x"}}`
	out, err := runServiceReportCmd(t, bare, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No truth in a bare dossier -> identity unsupported -> report unsupported.
	if !strings.Contains(out, "\"supported\": false") {
		t.Fatalf("bare dossier with no truth should be unsupported:\n%s", out)
	}
}

func TestServiceReportEmptyInputErrors(t *testing.T) {
	if _, err := runServiceReportCmd(t, "   "); err == nil {
		t.Fatalf("empty input should error")
	}
}

func TestServiceReportInvalidJSONErrors(t *testing.T) {
	if _, err := runServiceReportCmd(t, "{not json"); err == nil {
		t.Fatalf("invalid JSON should error")
	}
}
