// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

const repositoryContextOnlyEnvironment = "repository-context-only"

type nonEmptyRepositoryContextImpactStore struct {
	repeatedDigestEnvironmentImpactStore
}

func (nonEmptyRepositoryContextImpactStore) ListSupplyChainImpactRuntimeContext(
	_ context.Context,
	_ []string,
	_ []string,
	_ []string,
) (map[string]query.SupplyChainRuntimeContext, error) {
	return map[string]query.SupplyChainRuntimeContext{
		"repository:r_environment_budget": {
			Environments: []string{repositoryContextOnlyEnvironment},
		},
	}, nil
}

func (nonEmptyRepositoryContextImpactStore) ListSupplyChainImpactRuntimeEnvironmentEvidence(
	_ context.Context,
	candidates []query.SupplyChainRuntimeEnvironmentCandidate,
	_ []string,
	_ []string,
) (map[string]map[string]string, error) {
	evidence := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		if candidate.Environment != repositoryContextOnlyEnvironment {
			evidence[candidate.Environment] = "deploy_event"
		}
	}
	return map[string]map[string]string{repeatedDigestMCPTestDigest: evidence}, nil
}

func TestDispatchToolSupplyChainRuntimeEnvironmentEvidenceDefaultPageDoesNotDefaultRepositoryCandidates(t *testing.T) {
	t.Parallel()

	handler := &query.SupplyChainHandler{
		ImpactFindings:              nonEmptyRepositoryContextImpactStore{},
		Neo4j:                       repeatedDigestRuntimeGraph{},
		KubernetesWorkloadInventory: repeatedDigestRuntimeInventory{},
		Profile:                     query.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_supply_chain_impact_findings",
		map[string]any{"cve_id": "CVE-2026-5835", "limit": float64(repeatedDigestEnvironmentDefaultPageSize)},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result == nil || result.IsError || result.Envelope == nil || result.Envelope.Error != nil {
		t.Fatalf("default production dispatch = %#v, want success", result)
	}
	if got := estimateResponseBytes(result); got > defaultToolResponseByteBudget {
		t.Fatalf("MCP response bytes = %d, want <= %d", got, defaultToolResponseByteBudget)
	}

	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data = %T, want object", result.Envelope.Data)
	}
	findings, ok := data["findings"].([]any)
	if !ok || len(findings) != repeatedDigestEnvironmentDefaultPageSize {
		t.Fatalf("findings = %#v, want %d rows", data["findings"], repeatedDigestEnvironmentDefaultPageSize)
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
		evidence, ok := runtimeContext["environment_evidence"].(map[string]any)
		if !ok || len(evidence) != 1 {
			t.Fatalf("finding %d environment evidence = %#v, want one exact-digest entry", index, runtimeContext["environment_evidence"])
		}
		if _, leaked := evidence[repositoryContextOnlyEnvironment]; leaked {
			t.Fatalf("finding %d borrowed repository evidence: %#v", index, evidence)
		}
		probe, ok := runtimeContext["environment_evidence_probe"].(map[string]any)
		if !ok || probe["candidate_limit"] != float64(2) || probe["candidates_truncated"] != false {
			t.Fatalf("finding %d environment evidence probe = %#v, want limit=2 truncated=false", index, runtimeContext["environment_evidence_probe"])
		}
	}
}
