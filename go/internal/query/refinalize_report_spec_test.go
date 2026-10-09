// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// TestOpenAPIDeltaActiveScopesReportMatchesTheWireType keeps the #7797
// DeltaActiveScopesReport component in lockstep with the JSON
// recovery.DeltaActiveScopesReport encodes, and checks both refinalize routes
// reference it.
func TestOpenAPIDeltaActiveScopesReportMatchesTheWireType(t *testing.T) {
	t.Parallel()

	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	raw := query.OpenAPISpec()
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatalf("OpenAPI spec is not valid JSON: %v", err)
	}
	component, ok := spec.Components.Schemas["DeltaActiveScopesReport"]
	if !ok {
		t.Fatal("OpenAPI has no DeltaActiveScopesReport component")
	}

	encoded, err := json.Marshal(recovery.DeltaActiveScopes{}.Report())
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	required := append([]string(nil), component.Required...)
	sort.Strings(required)
	if got, want := strings.Join(sortedResponseKeys(wire), ","), strings.Join(required, ","); got != want {
		t.Fatalf("DeltaActiveScopesReport encodes %s but the OpenAPI component requires %s", got, want)
	}

	ref := `"delta_active_scopes": {"$ref": "#/components/schemas/DeltaActiveScopesReport"}`
	if got := strings.Count(raw, ref); got != 2 {
		t.Fatalf("delta_active_scopes is referenced %d times, want 2 (refinalize, recover-generations)", got)
	}

	written, ok := spec.Components.Schemas["ReindexRequestsWrittenReport"]
	if !ok {
		t.Fatal("OpenAPI has no ReindexRequestsWrittenReport component")
	}
	encoded, err = json.Marshal(recovery.DeltaActiveScopes{}.ReindexRequestsWritten())
	if err != nil {
		t.Fatalf("marshal reindex_requests_written: %v", err)
	}
	wire = nil
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal reindex_requests_written: %v", err)
	}
	required = append([]string(nil), written.Required...)
	sort.Strings(required)
	if got, want := strings.Join(sortedResponseKeys(wire), ","), strings.Join(required, ","); got != want {
		t.Fatalf("ReindexRequestsWrittenReport encodes %s but the OpenAPI component requires %s", got, want)
	}
	writtenRef := `"reindex_requests_written": {"$ref": "#/components/schemas/ReindexRequestsWrittenReport"}`
	if got := strings.Count(raw, writtenRef); got != 2 {
		t.Fatalf("reindex_requests_written is referenced %d times, want 2 (refinalize, recover-generations)", got)
	}
}
