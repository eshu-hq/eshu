// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

type repeatedDigestEnvironmentImpactStore struct{}

const (
	repeatedDigestEnvironmentDefaultPageSize = 50
	repeatedDigestEnvironmentMaximumPageSize = 200
)

func (repeatedDigestEnvironmentImpactStore) ListSupplyChainImpactFindings(
	_ context.Context,
	_ query.SupplyChainImpactFindingFilter,
) ([]query.SupplyChainImpactFindingRow, error) {
	rows := make([]query.SupplyChainImpactFindingRow, repeatedDigestEnvironmentMaximumPageSize)
	for index := range rows {
		rows[index] = query.SupplyChainImpactFindingRow{
			FindingID:     fmt.Sprintf("finding-environment-%03d", index),
			CVEID:         "CVE-2026-5835",
			PackageID:     fmt.Sprintf("pkg:maven/example/environment-%03d@1.0.0", index),
			RepositoryID:  "repository:r_environment_budget",
			SubjectDigest: repeatedDigestMCPTestDigest,
			ImpactStatus:  "affected_exact",
			Environments:  []string{fmt.Sprintf("environment-%03d", index)},
		}
	}
	return rows, nil
}

func (repeatedDigestEnvironmentImpactStore) ListSupplyChainImpactRuntimeContext(
	_ context.Context,
	_ []string,
	_ []string,
	_ []string,
) (map[string]query.SupplyChainRuntimeContext, error) {
	return map[string]query.SupplyChainRuntimeContext{
		"repository:r_environment_budget": {},
	}, nil
}

func (repeatedDigestEnvironmentImpactStore) ListSupplyChainImpactRuntimeEnvironmentEvidence(
	_ context.Context,
	candidates []query.SupplyChainRuntimeEnvironmentCandidate,
	_ []string,
	_ []string,
) (map[string]map[string]string, error) {
	evidence := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		evidence[candidate.Environment] = "deploy_event"
	}
	return map[string]map[string]string{repeatedDigestMCPTestDigest: evidence}, nil
}

func TestDispatchToolSupplyChainRuntimeEnvironmentEvidenceMaximumPageStaysRowBound(t *testing.T) {
	t.Parallel()

	result, err := dispatchToolWithOptions(
		context.Background(),
		repeatedDigestEnvironmentMux(),
		"list_supply_chain_impact_findings",
		map[string]any{"cve_id": "CVE-2026-5835", "limit": float64(repeatedDigestEnvironmentMaximumPageSize)},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		dispatchOptions{responseByteBudget: 0},
	)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions() error = %v, want nil", err)
	}
	if result == nil || result.IsError || result.Envelope == nil || result.Envelope.Error != nil {
		t.Fatalf("unbudgeted maximum page must remain a success, got %#v", result)
	}
	totalEvidence := requireRepeatedDigestEnvironmentPage(
		t,
		result,
		repeatedDigestEnvironmentMaximumPageSize,
	)
	if totalEvidence > repeatedDigestEnvironmentMaximumPageSize {
		t.Fatalf(
			"serialized exact-digest evidence entries = %d, want <= %d",
			totalEvidence,
			repeatedDigestEnvironmentMaximumPageSize,
		)
	}
	stripRuntimeEnvironmentProofFields(t, result)
	legacySize := estimateResponseBytes(result)
	if legacySize <= defaultToolResponseByteBudget {
		t.Fatalf(
			"maximum page without #5835 fields = %d bytes, want > pre-existing %d byte budget",
			legacySize,
			defaultToolResponseByteBudget,
		)
	}

	guarded, err := dispatchTool(
		context.Background(),
		repeatedDigestEnvironmentMux(),
		"list_supply_chain_impact_findings",
		map[string]any{"cve_id": "CVE-2026-5835", "limit": float64(repeatedDigestEnvironmentMaximumPageSize)},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("guarded dispatchTool() error = %v, want nil", err)
	}
	if guarded == nil || !guarded.IsError || guarded.Envelope == nil || guarded.Envelope.Error == nil {
		t.Fatalf("guarded maximum page = %#v, want canonical over-budget error", guarded)
	}
	if got := guarded.Envelope.Error.Code; got != errorCodeResponseOverBudget {
		t.Fatalf("guarded maximum page error code = %q, want %q", got, errorCodeResponseOverBudget)
	}
	t.Logf(
		"maximum page evidence_entries=%d legacy_response_bytes=%d guarded_error=%s",
		totalEvidence,
		legacySize,
		guarded.Envelope.Error.Code,
	)
}

func TestDispatchToolSupplyChainRuntimeEnvironmentEvidenceDefaultPageStaysWithinBudget(t *testing.T) {
	t.Parallel()

	result, err := dispatchTool(
		context.Background(),
		repeatedDigestEnvironmentMux(),
		"list_supply_chain_impact_findings",
		map[string]any{"cve_id": "CVE-2026-5835", "limit": float64(repeatedDigestEnvironmentDefaultPageSize)},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result == nil || result.IsError || result.Envelope == nil || result.Envelope.Error != nil {
		t.Fatalf("default repeated-digest page must remain a success, got %#v", result)
	}
	responseBytes := estimateResponseBytes(result)
	if responseBytes > defaultToolResponseByteBudget {
		t.Fatalf("MCP response bytes = %d, want <= %d", responseBytes, defaultToolResponseByteBudget)
	}
	totalEvidence := requireRepeatedDigestEnvironmentPage(
		t,
		result,
		repeatedDigestEnvironmentDefaultPageSize,
	)
	t.Logf(
		"default repeated-digest page response_bytes=%d budget=%d evidence_entries=%d",
		responseBytes,
		defaultToolResponseByteBudget,
		totalEvidence,
	)
}

func repeatedDigestEnvironmentMux() *http.ServeMux {
	handler := &query.SupplyChainHandler{
		ImpactFindings:              repeatedDigestEnvironmentImpactStore{},
		Neo4j:                       repeatedDigestRuntimeGraph{},
		KubernetesWorkloadInventory: repeatedDigestRuntimeInventory{},
		Profile:                     query.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func requireRepeatedDigestEnvironmentPage(t *testing.T, result *dispatchResult, wantRows int) int {
	t.Helper()

	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data = %T, want object", result.Envelope.Data)
	}
	findings, ok := data["findings"].([]any)
	if !ok || len(findings) != wantRows {
		t.Fatalf("findings = %#v, want %d rows", data["findings"], wantRows)
	}
	totalEvidence := 0
	for index, raw := range findings {
		finding, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("finding %d = %T, want object", index, raw)
		}
		runtimeContext, ok := finding["runtime_context"].(map[string]any)
		if !ok {
			t.Fatalf("finding %d runtime_context = %T, want object", index, finding["runtime_context"])
		}
		evidence, ok := runtimeContext["environment_evidence"].(map[string]any)
		wantEnvironment := fmt.Sprintf("environment-%03d", index)
		if !ok || len(evidence) != 1 || evidence[wantEnvironment] != "deploy_event" {
			t.Fatalf("finding %d evidence = %#v, want only %s=deploy_event", index, evidence, wantEnvironment)
		}
		totalEvidence += len(evidence)
	}
	return totalEvidence
}

func stripRuntimeEnvironmentProofFields(t *testing.T, result *dispatchResult) {
	t.Helper()

	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data = %T, want object", result.Envelope.Data)
	}
	findings, ok := data["findings"].([]any)
	if !ok {
		t.Fatalf("findings = %T, want array", data["findings"])
	}
	for index, raw := range findings {
		finding, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("finding %d = %T, want object", index, raw)
		}
		runtimeContext, ok := finding["runtime_context"].(map[string]any)
		if !ok {
			t.Fatalf("finding %d runtime_context = %T, want object", index, finding["runtime_context"])
		}
		delete(runtimeContext, "environment_evidence")
		delete(runtimeContext, "environment_evidence_probe")
	}
}
