// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbenchrun

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func intPtr(n int) *int { return &n }

func nornicHybridRun() searchbench.BackendRun {
	return searchbench.BackendRun{
		Backend:              searchbench.BackendNornicDBHybrid,
		Mode:                 searchbench.ModeHybrid,
		BackendImage:         "nornicdb-cpu-bge:v1.1.3",
		QueryCount:           searchbench.MinimumQuerySuiteSize,
		Latency:              searchbench.LatencySummary{P50: 2 * time.Millisecond, P95: 5 * time.Millisecond},
		Metrics:              searchbench.RetrievalMetrics{Recall: 0.8, Precision: 0.7, NDCG: 0.75, FalseCanonicalClaimCount: intPtr(0)},
		MemoryHighWaterBytes: 200 << 20,
		IndexArtifactBytes:   0,
		RebuildBehavior:      "lazy",
		Startup:              searchbench.StartupSummary{CleanVolume: time.Second, PreservedVolume: 2 * time.Second},
		SearchFlags: &searchbench.NornicDBSearchFlags{
			BM25Enabled:   true,
			VectorEnabled: true,
			BM25Warming:   "lazy",
			VectorWarming: "lazy",
		},
	}
}

func TestAssembleEvidenceValid(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize)
	postgres, err := RunSuite(context.Background(), suite, backend, postgresDescriptor())
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	evidence, err := AssembleEvidence(EvidenceInput{
		EshuCommit:           "eshu-deadbeef",
		SchemaBootstrapState: "ready",
		Corpus: searchbench.CorpusSummary{
			RepositoryCount:        1,
			FileCount:              10,
			EntityCount:            20,
			DocumentCount:          2,
			SourceKindDistribution: map[searchdocs.SourceKind]int{searchdocs.SourceKindCodeEntity: 1, searchdocs.SourceKindRepositoryFile: 1},
		},
		Backends:       []searchbench.BackendRun{postgres.Run, nornicHybridRun()},
		Recommendation: searchbench.Recommendation{Decision: searchbench.RecommendationDeferSearchChange, Rationale: "fixture corpus only"},
	})
	if err != nil {
		t.Fatalf("AssembleEvidence returned error: %v", err)
	}
	if evidence.Version != searchbench.EvidenceVersion {
		t.Errorf("version = %q, want %q", evidence.Version, searchbench.EvidenceVersion)
	}
	if evidence.TruthScope.Level != searchdocs.TruthLevelDerived {
		t.Errorf("truth level = %q, want derived", evidence.TruthScope.Level)
	}
	if evidence.TruthScope.Basis != searchdocs.TruthBasisContentIndex {
		t.Errorf("default truth basis = %q, want content_index", evidence.TruthScope.Basis)
	}
	if len(evidence.FailureClasses) != len(searchbench.RequiredFailureClasses()) {
		t.Errorf("failure classes = %d, want %d", len(evidence.FailureClasses), len(searchbench.RequiredFailureClasses()))
	}
	// The assembled record must satisfy the canonical validator.
	if err := searchbench.ValidateEvidence(evidence); err != nil {
		t.Errorf("assembled evidence failed ValidateEvidence: %v", err)
	}
}

func TestAssembleEvidenceRejectsMissingNornicDB(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize)
	postgres, err := RunSuite(context.Background(), suite, backend, postgresDescriptor())
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	_, err = AssembleEvidence(EvidenceInput{
		EshuCommit:           "eshu-deadbeef",
		SchemaBootstrapState: "ready",
		Corpus: searchbench.CorpusSummary{
			RepositoryCount:        1,
			DocumentCount:          1,
			SourceKindDistribution: map[searchdocs.SourceKind]int{searchdocs.SourceKindCodeEntity: 1},
		},
		Backends:       []searchbench.BackendRun{postgres.Run},
		Recommendation: searchbench.Recommendation{Decision: searchbench.RecommendationKeepPostgresSearch, Rationale: "no nornic arm"},
	})
	if err == nil {
		t.Fatal("expected error when no NornicDB backend is present")
	}
}

func TestAssembleEvidenceHonorsExplicitTruthBasis(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize)
	postgres, err := RunSuite(context.Background(), suite, backend, postgresDescriptor())
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	evidence, err := AssembleEvidence(EvidenceInput{
		EshuCommit:           "eshu-deadbeef",
		SchemaBootstrapState: "ready",
		TruthBasis:           searchdocs.TruthBasisReadModel,
		Corpus: searchbench.CorpusSummary{
			RepositoryCount:        1,
			DocumentCount:          1,
			SourceKindDistribution: map[searchdocs.SourceKind]int{searchdocs.SourceKindRuntimeSummary: 1},
		},
		Backends:       []searchbench.BackendRun{postgres.Run, nornicHybridRun()},
		Recommendation: searchbench.Recommendation{Decision: searchbench.RecommendationDeferSearchChange, Rationale: "read model basis"},
	})
	if err != nil {
		t.Fatalf("AssembleEvidence returned error: %v", err)
	}
	if evidence.TruthScope.Basis != searchdocs.TruthBasisReadModel {
		t.Errorf("truth basis = %q, want read_model", evidence.TruthScope.Basis)
	}
}
