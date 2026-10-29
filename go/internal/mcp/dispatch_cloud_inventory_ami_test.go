// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/testutil/contentreader"
)

// amiInventoryCanonicalRow returns one canonical identity envelope exactly as
// the admission writer emits it from a loader-admitted AMI record: the
// attributes map carries the validated name only.
func amiInventoryCanonicalRow(t *testing.T) []byte {
	t.Helper()
	row := map[string]any{
		"fact_id":       "reducer_cloud_resource_identity:ami-1",
		"fact_kind":     "reducer_cloud_resource_identity",
		"scope_id":      "cloud-scope:aws:ec2",
		"generation_id": "gen-1",
		"source_system": "aws_cloud_inventory",
		"observed_at":   "2026-06-09T00:00:00Z",
		"payload": map[string]any{
			"cloud_resource_uid":    "aws:111122223333:aws_ec2_ami:ami-0000000000000000a",
			"provider":              "aws",
			"resource_type":         "aws_ec2_ami",
			"management_origin":     "observed",
			"has_declared_evidence": false,
			"has_applied_evidence":  false,
			"has_observed_evidence": true,
			"scope_id":              "cloud-scope:aws:ec2",
			"raw_identity":          "ami-0000000000000000a",
			"account_id":            "111122223333",
			"attributes": map[string]any{
				"name": "ubuntu-jammy-22.04-amd64-server-20230601",
			},
		},
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal AMI canonical row: %v", err)
	}
	return encoded
}

// TestDispatchToolCloudInventorySurfacesAMIName is the MCP golden cassette
// for #9525: list_cloud_resource_inventory served through real MCP dispatch
// carries the loader-admitted name verbatim under data, and no description
// or dropped value. MCP dispatch is transport-only onto the HTTP handler, so
// this cassettes the same bytes the HTTP cassette asserts.
func TestDispatchToolCloudInventorySurfacesAMIName(t *testing.T) {
	t.Parallel()

	db := contentreader.OpenReaderTestDB(t, []contentreader.ReaderQueryResult{{
		Columns: []string{"payload"},
		Rows:    [][]driver.Value{{amiInventoryCanonicalRow(t)}},
		QueryContains: []string{
			"FROM fact_records",
			"fact_records.fact_kind = 'reducer_cloud_resource_identity'",
			"fact_records.payload->>'provider' = $1",
		},
	}})
	handler := &query.CloudInventoryHandler{
		Content: query.NewContentReader(db),
		Profile: query.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_cloud_resource_inventory",
		map[string]any{"provider": "aws", "limit": float64(10)},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatalf("result.Envelope = nil, want truth envelope with data")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", result.Envelope.Data)
	}
	resources, ok := data["resources"].([]any)
	if !ok || len(resources) != 1 {
		t.Fatalf("data.resources = %#v, want one AMI row", data["resources"])
	}
	attrs, ok := resources[0].(map[string]any)["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want object with the admitted name", resources[0])
	}
	if got, want := attrs["name"], "ubuntu-jammy-22.04-amd64-server-20230601"; got != want {
		t.Fatalf("attributes.name = %#v, want verbatim %q", got, want)
	}
	raw, err := json.Marshal(result.Envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	for _, forbidden := range []string{"must-not-surface", `"description"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("MCP envelope carries sensitive text %q: %s", forbidden, raw)
		}
	}
}
