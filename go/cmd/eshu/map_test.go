// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestMapFromCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"map", "--from", "terraform/aws_lb.main"})
	if err != nil {
		t.Fatalf("rootCmd.Find(map --from) error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "map" {
		t.Fatalf("resolved command = %#v, want map command", cmd)
	}
	for _, name := range []string{"from", "type", "repo", "env", "relationship", "depth", "limit", "json", "service-url", "api-key", "profile"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("map flag %q missing", name)
		}
	}
}

func TestFetchEntityMapRequestsCanonicalEnvelope(t *testing.T) {
	var gotAccept string
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.EscapedPath()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v, want nil", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("json.Unmarshal(request) error = %v, want nil; body=%s", err, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"mapped","sections":{},"evidence":{"truncated":false}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	got, err := fetchEntityMap(client, entityMapOptions{
		From:         "terraform/aws_lb.main",
		FromType:     "terraform_resource",
		Repo:         "repo-infra",
		Environment:  "prod",
		Relationship: "PROVISIONS_DEPENDENCY_FOR",
		Depth:        2,
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("fetchEntityMap() error = %v, want nil", err)
	}
	if gotAccept != eshuEnvelopeMIMEType {
		t.Fatalf("Accept = %q, want %q", gotAccept, eshuEnvelopeMIMEType)
	}
	if gotPath != "/api/v0/impact/entity-map" {
		t.Fatalf("path = %q, want entity-map endpoint", gotPath)
	}
	for key, want := range map[string]any{
		"from":         "terraform/aws_lb.main",
		"from_type":    "terraform_resource",
		"repo_id":      "repo-infra",
		"environment":  "prod",
		"relationship": "PROVISIONS_DEPENDENCY_FOR",
		"depth":        float64(2),
		"limit":        float64(10),
	} {
		if got := gotBody[key]; got != want {
			t.Fatalf("request[%s] = %#v, want %#v; body=%#v", key, got, want, gotBody)
		}
	}
	if got.Data == nil {
		t.Fatalf("Data = nil, want map data")
	}
}

func TestRunMapFromRendersGroupedSummary(t *testing.T) {
	reset := stubEntityMapFetch(t, entityMapEnvelope{
		Data: sampleEntityMapData("mapped"),
		Truth: map[string]any{
			"level":      "exact",
			"capability": "platform_impact.entity_map",
			"freshness":  map[string]any{"state": "fresh"},
		},
	})
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestMapCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("from", "terraform/aws_lb.main"); err != nil {
		t.Fatalf("Set(from) error = %v, want nil", err)
	}

	if err := runMapFrom(cmd, nil); err != nil {
		t.Fatalf("runMapFrom() error = %v, want nil", err)
	}

	output := out.String()
	for _, want := range []string{
		"Map: terraform/aws_lb.main",
		"Resolved: TerraformResource tfstate:aws_lb.main",
		"Defined by",
		"Depends on",
		"Evidence: 2 relationships",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("map output missing %q:\n%s", want, output)
		}
	}
}

func TestRunMapFromJSONPassesCanonicalEnvelope(t *testing.T) {
	reset := stubEntityMapFetch(t, entityMapEnvelope{
		Data: sampleEntityMapData("mapped"),
		Truth: map[string]any{
			"level":     "exact",
			"freshness": map[string]any{"state": "fresh"},
		},
	})
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestMapCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("from", "terraform/aws_lb.main"); err != nil {
		t.Fatalf("Set(from) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	if err := runMapFrom(cmd, nil); err != nil {
		t.Fatalf("runMapFrom() error = %v, want nil", err)
	}

	var payload entityMapEnvelope
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if payload.Error != nil {
		t.Fatalf("payload.Error = %#v, want nil", payload.Error)
	}
	if got := payload.Data["status"]; got != "mapped" {
		t.Fatalf("status = %#v, want mapped", got)
	}
}

func TestRunMapFromReturnsAmbiguousExitCode(t *testing.T) {
	reset := stubEntityMapFetch(t, entityMapEnvelope{
		Data: sampleEntityMapData("ambiguous"),
		Truth: map[string]any{
			"level":     "exact",
			"freshness": map[string]any{"state": "fresh"},
		},
	})
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestMapCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("from", "orders"); err != nil {
		t.Fatalf("Set(from) error = %v, want nil", err)
	}

	err := runMapFrom(cmd, nil)
	if err == nil {
		t.Fatal("runMapFrom() error = nil, want ambiguous selector exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 3; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
	if output := out.String(); !strings.Contains(output, "Map selector is ambiguous") {
		t.Fatalf("ambiguous output missing guidance:\n%s", output)
	}
}

func TestRunMapFromReturnsStaleExitCode(t *testing.T) {
	reset := stubEntityMapFetch(t, entityMapEnvelope{
		Data: sampleEntityMapData("mapped"),
		Truth: map[string]any{
			"level":     "exact",
			"freshness": map[string]any{"state": "stale"},
		},
	})
	defer reset()

	cmd := newTestMapCommand()
	if err := cmd.Flags().Set("from", "terraform/aws_lb.main"); err != nil {
		t.Fatalf("Set(from) error = %v, want nil", err)
	}

	err := runMapFrom(cmd, nil)
	if err == nil {
		t.Fatal("runMapFrom() error = nil, want stale index exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 4; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
}

func newTestMapCommand() *cobra.Command {
	cmd := &cobra.Command{}
	addEntityMapFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}

func stubEntityMapFetch(t *testing.T, envelope entityMapEnvelope) func() {
	t.Helper()
	original := entityMapFetch
	entityMapFetch = func(_ *APIClient, opts entityMapOptions) (entityMapEnvelope, error) {
		if strings.TrimSpace(opts.From) == "" {
			t.Fatal("opts.From is empty, want --from value")
		}
		return envelope, nil
	}
	return func() {
		entityMapFetch = original
	}
}

func sampleEntityMapData(status string) map[string]any {
	data := map[string]any{
		"status": status,
		"from":   "terraform/aws_lb.main",
		"resolution": map[string]any{
			"status": "resolved",
			"selected": map[string]any{
				"id":     "tfstate:aws_lb.main",
				"name":   "aws_lb.main",
				"labels": []any{"TerraformResource"},
			},
		},
		"sections": map[string]any{
			"defined_by": []any{
				map[string]any{"relationship_type": "DEFINES", "entity_name": "infra-repo"},
			},
			"deployed_by": []any{},
			"runs_as":     []any{},
			"depends_on": []any{
				map[string]any{"relationship_type": "PROVISIONS_DEPENDENCY_FOR", "entity_name": "checkout"},
			},
			"consumed_by": []any{},
		},
		"evidence": map[string]any{
			"relationship_count": float64(2),
			"truncated":          false,
		},
	}
	if status == "ambiguous" {
		data["resolution"] = map[string]any{
			"status": "ambiguous",
			"candidates": []any{
				map[string]any{"id": "workload:orders-api", "name": "orders", "repo_id": "repo-api"},
				map[string]any{"id": "workload:orders-worker", "name": "orders", "repo_id": "repo-worker"},
			},
		}
	}
	return data
}
