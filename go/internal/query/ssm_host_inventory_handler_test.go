// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func ssmHostInventoryEnvelopeRow(t *testing.T, factID, kind string, payload map[string]any) []byte {
	t.Helper()
	row := map[string]any{
		"fact_id":       factID,
		"fact_kind":     kind,
		"scope_id":      "scope-1",
		"generation_id": "gen-2",
		"is_tombstone":  false,
		"payload":       payload,
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal ssm host envelope row: %v", err)
	}
	return encoded
}

func TestSSMHostInventoryHandlerListsHosts(t *testing.T) {
	t.Parallel()

	appPayload := map[string]any{
		"partition": "aws", "account_id": "000000000001", "region": "us-east-1", "instance_id": "i-1",
		"resource_type": "AWS::EC2::Instance", "source_type": "AWS:Application", "schema_version": "1.0.0",
		"capture_time": "2026-10-29T22:31:19Z", "name": "alpha", "version": "1.0",
	}
	platPayload := map[string]any{
		"partition": "aws", "account_id": "000000000001", "region": "us-east-1", "instance_id": "i-1",
		"resource_type": "AWS::EC2::Instance", "source_type": "ssm_inventory",
		"platform_type": "Linux", "platform_name": "ExampleLinux",
		"capture_time": "2026-10-29T22:31:19Z", "observation_time": "2026-10-29T22:33:02Z",
	}
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"envelope"},
		rows: [][]driver.Value{
			{ssmHostInventoryEnvelopeRow(t, "a1", "aws_ssm_host_application", appPayload)},
			{ssmHostInventoryEnvelopeRow(t, "p1", "aws_ssm_host_platform", platPayload)},
		},
		queryContains: []string{
			"FROM fact_records",
			"aws_ssm_host_application",
			"aws_ssm_host_platform",
		},
	}})
	graph := &stubSSMGraph{rows: ssmCommittedRows(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		[]map[string]any{ssmReadPlatformNode("uid-plat-1", "ssm_inventory", "2026-10-29T22:33:02Z")},
		[][2]string{{"uid-app-1", "uid-plat-1"}},
	)}
	handler := &SSMHostInventoryHandler{
		Content: NewContentReader(db),
		Graph:   graph,
		Profile: ProfileProduction,
		Bounds:  ssmReadBounds,
		Now:     func() time.Time { return ssmReadNow },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/ssm-host-inventory?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, _ := body["data"].(map[string]any)
	hosts, _ := data["hosts"].([]any)
	if len(hosts) != 1 {
		t.Fatalf("hosts = %d, want 1: %s", len(hosts), w.Body.String())
	}
	host, _ := hosts[0].(map[string]any)
	if host["status"] != "successful" {
		t.Fatalf("status = %v, want successful: %s", host["status"], w.Body.String())
	}
	if host["instance_id"] != "i-1" {
		t.Fatalf("instance_id = %v, want i-1", host["instance_id"])
	}
}

func TestSSMHostInventoryHandlerRejectsUnknownStatus(t *testing.T) {
	t.Parallel()

	handler := &SSMHostInventoryHandler{Bounds: ssmReadBounds}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/ssm-host-inventory?status=haunted", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown status filter", w.Code)
	}
}

func TestSSMHostInventoryHandlerRefusesWithoutBounds(t *testing.T) {
	t.Parallel()

	handler := &SSMHostInventoryHandler{Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/ssm-host-inventory", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 fail-closed without freshness bounds", w.Code)
	}
}

func TestSSMHostInventoryHandlerResponseCarriesNoFindings(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"envelope"},
		rows:    [][]driver.Value{},
	}})
	graph := &stubSSMGraph{rows: ssmCommittedRows(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		nil, nil,
	)}
	handler := &SSMHostInventoryHandler{
		Content: NewContentReader(db),
		Graph:   graph,
		Profile: ProfileProduction,
		Bounds:  ssmReadBounds,
		Now:     func() time.Time { return ssmReadNow },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/ssm-host-inventory", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	for _, key := range []string{`"vulnerability"`, `"os_package"`, `"supply_chain_impact"`, `"purl"`, `"image_digest"`} {
		if strings.Contains(w.Body.String(), key) {
			t.Fatalf("response carries %s: observations must never synthesize findings", key)
		}
	}
}
