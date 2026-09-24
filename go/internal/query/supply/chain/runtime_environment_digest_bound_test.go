// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

func TestApplySupplyChainRuntimeContextDoesNotBorrowMismatchedDigestEvidenceForRepositoryCandidate(t *testing.T) {
	t.Parallel()

	row := osPackageFindingRowForRuntimeContext()
	store := &graph.FakeRuntimeContextFindingStore{
		ByRepo: map[string]impact.RuntimeContext{
			row.RepositoryID: {
				Environments: []string{"production"},
			},
		},
		ByDigest: map[string]map[string]string{
			"sha256:other-artifact": {"production": impact.RuntimeEnvironmentEvidenceDeployEvent},
		},
	}
	rows := []impact.FindingRow{row}
	if err := (&Handler{ImpactFindings: store}).applySupplyChainRuntimeContext(
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
	if got := store.EnvCandidates; len(got) != 1 || got[0].SubjectDigest != row.SubjectDigest || got[0].Environment != "production" {
		t.Fatalf("exact-digest candidates = %#v, want finding digest paired with production", got)
	}
}

func TestApplySupplyChainRuntimeContextCapsOneRepositoryEnvironmentEvidenceAtPageBudget(t *testing.T) {
	t.Parallel()

	row := osPackageFindingRowForRuntimeContext()
	const environmentCount = MaxRuntimeEnvironmentCandidates + 1
	repositoryEnvironments := make([]string, 0, environmentCount)
	confirmed := make(map[string]string, MaxRuntimeEnvironmentCandidates)
	for index := 0; index < environmentCount; index++ {
		environment := fmt.Sprintf("environment-%03d", index)
		repositoryEnvironments = append(repositoryEnvironments, environment)
		if index < MaxRuntimeEnvironmentCandidates {
			confirmed[environment] = impact.RuntimeEnvironmentEvidenceDeployEvent
		}
	}
	store := &graph.FakeRuntimeContextFindingStore{
		ByRepo: map[string]impact.RuntimeContext{
			row.RepositoryID: {
				Environments: repositoryEnvironments,
			},
		},
		ByDigest: map[string]map[string]string{row.SubjectDigest: confirmed},
	}
	rows := []impact.FindingRow{row}
	if err := (&Handler{ImpactFindings: store}).applySupplyChainRuntimeContext(
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
	if got := len(resolved.EnvironmentEvidence); got > MaxRuntimeEnvironmentCandidates {
		t.Fatalf(
			"serialized environment evidence entries = %d, want <= %d",
			got,
			MaxRuntimeEnvironmentCandidates,
		)
	}
	probe := resolved.EnvironmentEvidenceProbe
	if probe == nil || probe.CandidateLimit != MaxRuntimeEnvironmentCandidates || !probe.CandidatesTruncated {
		t.Fatalf(
			"environment evidence probe = %#v, want candidate_limit=%d and truncated=true",
			probe,
			MaxRuntimeEnvironmentCandidates,
		)
	}
	if got := len(store.EnvCandidates); got != MaxRuntimeEnvironmentCandidates {
		t.Fatalf("set-based lookup candidates = %d, want %d", got, MaxRuntimeEnvironmentCandidates)
	}
}
