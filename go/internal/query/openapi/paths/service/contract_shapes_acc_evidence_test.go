// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestAccServiceIntelReportErrors pins the audited (#acc-evidence)
// intelligence-report error contract: the route relays the shared
// service-story seam (entity.BuildServiceStoryEnvelope), so it answers the
// same 501 capability, 400, 404, 409, 503, and graph-verdict 504 the story
// route documents, plus 403 via the scoped-token gate and 500. 503 without
// Retry-After also covers the nil-Entities not-configured path.
func TestAccServiceIntelReportErrors(t *testing.T) {
	t.Parallel()

	trimmed := strings.TrimSpace(IntelligenceReport)
	trimmed = strings.TrimSuffix(trimmed, ",")
	var doc map[string]any
	if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
		t.Fatalf("IntelligenceReport: fragment is not valid JSON: %v", err)
	}
	route, ok := doc["/api/v0/services/{service_name}/intelligence-report"].(map[string]any)
	if !ok {
		t.Fatal("IntelligenceReport: fragment paths missing /api/v0/services/{service_name}/intelligence-report")
	}
	op, ok := route["get"].(map[string]any)
	if !ok {
		t.Fatal("IntelligenceReport intelligence-report: missing get op")
	}
	responses, ok := op["responses"].(map[string]any)
	if !ok {
		t.Fatal("IntelligenceReport intelligence-report: missing responses")
	}
	var got []string
	for code := range responses {
		got = append(got, code)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "200,400,403,404,409,500,501,503,504" {
		t.Fatalf("intelligence-report: responses = [%s], want [200,400,403,404,409,500,501,503,504]",
			strings.Join(got, ","))
	}
}
