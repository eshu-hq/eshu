// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"testing"

	ecosystemtools "github.com/eshu-hq/eshu/go/internal/mcp/ecosystem"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/openapi"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestTraceDeploymentChainDefaultArgumentsFitBudget drives trace_deployment_chain
// through the real dispatch path with default MCP arguments (#7174). The fake
// handler asserts the adapter asked for handles without sections, shapes the
// every-family-at-cap fixture with the real exported shaper, and writes the
// canonical envelope; the dispatched result must keep structuredContent
// (no resource-only fallback), stay within 80% of the response budget, and
// carry the omissions on truth.
func TestTraceDeploymentChainDefaultArgumentsFitBudget(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body["evidence_detail"] != deployment.EvidenceDetailHandles {
			t.Errorf("MCP default evidence_detail = %#v, want handles", body["evidence_detail"])
		}
		if _, present := body["sections"]; present {
			t.Errorf("MCP default forwarded sections = %#v, want absent", body["sections"])
		}
		sc := testutil.TraceAtCapScenario{PerFamily: 50, EnrichmentRows: 25, PlatformsPerInst: 1}
		ctx := testutil.TraceAtCapWorkloadContext(sc)
		response := deployment.BuildDeploymentTraceResponse("payments-api", ctx, testutil.TraceAtCapOverview(ctx, false))
		selection := deployment.SectionSelection{EvidenceDetail: querycontract.StringVal(body, "evidence_detail")}
		truth := querycontract.BuildTruthEnvelope(querycontract.ProfileProduction, "platform_impact.deployment_chain",
			querycontract.TruthBasisHybrid, "resolved from deployment topology and service evidence")
		truth.Omissions = deployment.ApplySectionSelection(response, selection)
		querycontract.WriteSuccess(w, r, http.StatusOK, response, truth)
	})

	result, err := dispatchToolWithOptions(
		context.Background(), handler, "trace_deployment_chain",
		map[string]any{"service_name": "payments-api"}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		dispatchOptions{responseByteBudget: defaultToolResponseByteBudget},
	)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions() error = %v", err)
	}
	counted := estimateResponseBytes(result)
	ceiling := defaultToolResponseByteBudget * 8 / 10
	t.Logf("trace_deployment_chain MCP default at cap: %d counted bytes (%.1f%% of %d; ceiling %d)",
		counted, 100*float64(counted)/float64(defaultToolResponseByteBudget), defaultToolResponseByteBudget, ceiling)
	if result.IsError || result.ResourceOnly {
		t.Fatalf("result IsError=%v ResourceOnly=%v, want a structured success within budget", result.IsError, result.ResourceOnly)
	}
	if counted > ceiling {
		t.Fatalf("counted bytes = %d, want <= %d (80%% of the dispatch budget)", counted, ceiling)
	}
	if result.Envelope == nil || result.Envelope.Truth == nil || len(result.Envelope.Truth.Omissions) == 0 {
		t.Fatal("truth.omissions did not survive the MCP envelope decode")
	}
	var deliveryOmitted bool
	for _, omission := range result.Envelope.Truth.Omissions {
		if omission.Section == "delivery_paths" && omission.Detail == "omitted" && omission.Total > 0 {
			deliveryOmitted = true
		}
	}
	if !deliveryOmitted {
		t.Fatalf("truth.omissions = %+v, want delivery_paths omitted with its total", result.Envelope.Truth.Omissions)
	}
}

// TestTraceDeploymentChainSectionEnumsAgree proves the sections enum is one
// list on every surface: the deployment package's SectionNames, the MCP input
// schema, and the OpenAPI request fragment.
func TestTraceDeploymentChainSectionEnumsAgree(t *testing.T) {
	t.Parallel()

	want := deployment.SectionNames()
	if got := ecosystemtools.TraceDeploymentSectionNames(); !slices.Equal(got, want) {
		t.Fatalf("MCP schema sections enum = %v, want deployment.SectionNames() %v", got, want)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Properties map[string]struct {
							Enum  []string `json:"enum"`
							Items struct {
								Enum []string `json:"enum"`
							} `json:"items"`
						} `json:"properties"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(openapi.Spec()), &spec); err != nil {
		t.Fatalf("decode OpenAPI spec: %v", err)
	}
	properties := spec.Paths["/api/v0/impact/trace-deployment-chain"]["post"].RequestBody.Content["application/json"].Schema.Properties
	if got := properties["sections"].Items.Enum; !slices.Equal(got, want) {
		t.Fatalf("OpenAPI sections enum = %v, want deployment.SectionNames() %v", got, want)
	}
	if got := properties["evidence_detail"].Enum; !slices.Equal(got, deployment.EvidenceDetailValues()) {
		t.Fatalf("OpenAPI evidence_detail enum = %v, want %v", got, deployment.EvidenceDetailValues())
	}
}
