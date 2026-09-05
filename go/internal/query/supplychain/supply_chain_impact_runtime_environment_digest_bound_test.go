// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func TestApplySupplyChainRuntimeContextDoesNotBorrowMismatchedDigestEvidenceForRepositoryCandidate(t *testing.T) {
	t.Parallel()

	row := osPackageFindingRowForRuntimeContext()
	store := &runtimeContextFindingStore{
		byRepo: map[string]impact.SupplyChainRuntimeContext{
			row.RepositoryID: {
				Environments: []string{"production"},
			},
		},
		byDigest: map[string]map[string]string{
			"sha256:other-artifact": {"production": impact.SupplyChainRuntimeEnvironmentEvidenceDeployEvent},
		},
	}
	rows := []impact.SupplyChainImpactFindingRow{row}
	if err := (&SupplyChainHandler{ImpactFindings: store}).applySupplyChainRuntimeContext(
		context.Background(),
		rows,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}

	resolved := rows[0].RuntimeContext
	if resolved == nil {
		t.Fatal("runtime context = nil, want repository environment candidate")
	}
	if got := resolved.Environments; len(got) != 1 || got[0] != "production" {
		t.Fatalf("runtime environments = %#v, want [production] candidate", got)
	}
	if len(resolved.EnvironmentEvidence) != 0 {
		t.Fatalf(
			"runtime environment evidence = %#v, want empty without an exact digest match",
			resolved.EnvironmentEvidence,
		)
	}
	if got := store.envCandidates; len(got) != 1 || got[0].SubjectDigest != row.SubjectDigest || got[0].Environment != "production" {
		t.Fatalf("exact-digest candidates = %#v, want finding digest paired with production", got)
	}
}

func TestApplySupplyChainRuntimeContextCapsOneRepositoryEnvironmentEvidenceAtPageBudget(t *testing.T) {
	t.Parallel()

	row := osPackageFindingRowForRuntimeContext()
	const environmentCount = MaxSupplyChainRuntimeEnvironmentCandidates + 1
	repositoryEnvironments := make([]string, 0, environmentCount)
	confirmed := make(map[string]string, MaxSupplyChainRuntimeEnvironmentCandidates)
	for index := 0; index < environmentCount; index++ {
		environment := fmt.Sprintf("environment-%03d", index)
		repositoryEnvironments = append(repositoryEnvironments, environment)
		if index < MaxSupplyChainRuntimeEnvironmentCandidates {
			confirmed[environment] = impact.SupplyChainRuntimeEnvironmentEvidenceDeployEvent
		}
	}
	store := &runtimeContextFindingStore{
		byRepo: map[string]impact.SupplyChainRuntimeContext{
			row.RepositoryID: {
				Environments: repositoryEnvironments,
			},
		},
		byDigest: map[string]map[string]string{row.SubjectDigest: confirmed},
	}
	rows := []impact.SupplyChainImpactFindingRow{row}
	if err := (&SupplyChainHandler{ImpactFindings: store}).applySupplyChainRuntimeContext(
		context.Background(),
		rows,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	); err != nil {
		t.Fatalf("applySupplyChainRuntimeContext() error = %v, want nil", err)
	}

	resolved := rows[0].RuntimeContext
	if resolved == nil {
		t.Fatal("runtime context = nil")
	}
	if got := len(resolved.EnvironmentEvidence); got > MaxSupplyChainRuntimeEnvironmentCandidates {
		t.Fatalf(
			"serialized environment evidence entries = %d, want <= %d",
			got,
			MaxSupplyChainRuntimeEnvironmentCandidates,
		)
	}
	probe := resolved.EnvironmentEvidenceProbe
	if probe == nil || probe.CandidateLimit != MaxSupplyChainRuntimeEnvironmentCandidates || !probe.CandidatesTruncated {
		t.Fatalf(
			"environment evidence probe = %#v, want candidate_limit=%d and truncated=true",
			probe,
			MaxSupplyChainRuntimeEnvironmentCandidates,
		)
	}
	if got := len(store.envCandidates); got != MaxSupplyChainRuntimeEnvironmentCandidates {
		t.Fatalf("set-based lookup candidates = %d, want %d", got, MaxSupplyChainRuntimeEnvironmentCandidates)
	}
}
