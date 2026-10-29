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
)

// amiNameBenign is the admitted AMI name fixture shared by the HTTP and MCP
// cassettes: structurally validated at the loader, served verbatim here.
const amiNameBenign = "ubuntu-jammy-22.04-amd64-server-20230601"

// cloudInventoryAMICanonicalRow returns one canonical
// reducer_cloud_resource_identity envelope in loader-admitted shape: the
// attributes carry the validated name plus a rogue non-scalar description
// value. Production rows never carry description at all (the loader drops it
// before the writer, pinned by the loader battery in
// go/internal/storage/postgres where every source payload carries the
// description sentinel); the rogue object here probes the readback
// projector's own type gate, which drops non-scalar values, as defense in
// depth. A string description would surface through the permissive
// passthrough by design (ruling decision 7c), so only the loader pin covers
// that shape.
func cloudInventoryAMICanonicalRow(t *testing.T, withAttributes bool) []byte {
	t.Helper()
	payload := map[string]any{
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
	}
	if withAttributes {
		payload["attributes"] = map[string]any{
			"name": amiNameBenign,
			// Rogue non-scalar description: the projector's type gate must
			// drop it, so neither the key nor the sentinel inside surfaces.
			"description": map[string]any{"rogue": "must-not-surface"},
		}
	}
	row := map[string]any{
		"fact_id":       "reducer_cloud_resource_identity:ami-1",
		"fact_kind":     "reducer_cloud_resource_identity",
		"scope_id":      "cloud-scope:aws:ec2",
		"generation_id": "gen-1",
		"source_system": "aws_cloud_inventory",
		"observed_at":   "2026-06-09T00:00:00Z",
		"payload":       payload,
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal AMI canonical row: %v", err)
	}
	return encoded
}

// TestCloudInventoryHandlerSurfacesAMINameDropsDescription is the HTTP golden
// cassette for #9525: the benign name admitted by the loader surfaces
// verbatim, and the rogue non-scalar description carried by the fixture is
// dropped by the projector's type gate, so neither the description key nor
// the sentinel inside reaches the body. The read model stays a permissive
// scalar passthrough by design (ruling decision 7c); string-description
// exclusion rests on the loader pin proved in go/internal/storage/postgres.
func TestCloudInventoryHandlerSurfacesAMINameDropsDescription(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows: [][]driver.Value{
			{cloudInventoryAMICanonicalRow(t, true)},
			{cloudInventoryAMICanonicalRow(t, false)},
		},
		queryContains: []string{
			"FROM fact_records",
			"fact_records.fact_kind = 'reducer_cloud_resource_identity'",
			"fact_records.payload->>'provider' = $1",
		},
	}})
	handler := &CloudInventoryHandler{
		Content: NewContentReader(db),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=aws&limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	resources := data["resources"].([]any)
	if got, want := len(resources), 2; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	named := resources[0].(map[string]any)
	attrs, ok := named["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want object with the admitted name", named["attributes"])
	}
	if got := attrs["name"]; got != amiNameBenign {
		t.Fatalf("attributes.name = %#v, want verbatim %q", got, amiNameBenign)
	}
	if got, want := len(attrs), 1; got != want {
		t.Fatalf("len(attributes) = %d, want %d (name only, verbatim)", got, want)
	}
	for i, resource := range resources {
		row := resource.(map[string]any)
		if _, present := row["raw_identity"]; present {
			t.Fatalf("resources[%d] leaks raw_identity: %#v", i, row)
		}
	}
	for _, forbidden := range []string{"must-not-surface", `"description"`} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatalf("HTTP body carries sensitive text %q: %s", forbidden, w.Body.String())
		}
	}
}
