// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestValidateEvidenceRejectsNornicDBWithoutSearchDocumentCorpus(t *testing.T) {
	t.Parallel()

	evidence := validEvidenceFixture()
	evidence.Corpus.DocumentCount = 0
	evidence.Corpus.SourceKindDistribution = nil
	evidence.Backends[1].SearchFlags = nil

	err := ValidateEvidence(evidence)
	if err == nil {
		t.Fatal("ValidateEvidence() error = nil, want search-document corpus and flag errors")
	}
	for _, want := range []string{
		"corpus.document_count is required",
		"corpus.source_kind_distribution is required",
		"nornicdb search flags are required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateEvidence() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateEvidenceRejectsCanonicalTruthClaims(t *testing.T) {
	t.Parallel()

	evidence := validEvidenceFixture()
	evidence.TruthScope.Level = searchdocs.TruthLevel("canonical")
	evidence.Backends[0].Metrics.FalseCanonicalClaimCount = nil

	err := ValidateEvidence(evidence)
	if err == nil {
		t.Fatal("ValidateEvidence() error = nil, want truth-scope and false-claim errors")
	}
	for _, want := range []string{
		"truth_scope.level must be derived",
		"false_canonical_claim_count is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateEvidence() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateEvidenceRejectsUnknownEvidenceVersion(t *testing.T) {
	t.Parallel()

	evidence := validEvidenceFixture()
	evidence.Version = "search-benchmark-evidence/v2"

	err := ValidateEvidence(evidence)
	if err == nil {
		t.Fatal("ValidateEvidence() error = nil, want evidence version error")
	}
	if want := "version must be search-benchmark-evidence/v1"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateEvidence() error = %q, want substring %q", err, want)
	}
}

func TestValidateEvidenceRejectsInvalidCorpusInventoryCounts(t *testing.T) {
	t.Parallel()

	evidence := validEvidenceFixture()
	evidence.Corpus.RepositoryCount = -1
	evidence.Corpus.FileCount = -1
	evidence.Corpus.EntityCount = -1
	evidence.Corpus.VectorCount = -1

	err := ValidateEvidence(evidence)
	if err == nil {
		t.Fatal("ValidateEvidence() error = nil, want corpus inventory errors")
	}
	for _, want := range []string{
		"corpus.repository_count must be non-negative",
		"corpus.file_count must be non-negative",
		"corpus.entity_count must be non-negative",
		"corpus.vector_count must be non-negative",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateEvidence() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateEvidenceRejectsMissingOrUnknownTruthBasis(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		basis searchdocs.TruthBasis
	}{
		{name: "missing", basis: ""},
		{name: "unknown", basis: searchdocs.TruthBasis("whole_graph_search")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evidence := validEvidenceFixture()
			evidence.TruthScope.Basis = tt.basis

			err := ValidateEvidence(evidence)
			if err == nil {
				t.Fatal("ValidateEvidence() error = nil, want truth basis error")
			}
			if want := "truth_scope.basis is invalid"; !strings.Contains(err.Error(), want) {
				t.Fatalf("ValidateEvidence() error = %q, want substring %q", err, want)
			}
		})
	}
}

func TestValidateEvidenceAcceptsPostgresAndNornicDBComparison(t *testing.T) {
	t.Parallel()

	if err := ValidateEvidence(validEvidenceFixture()); err != nil {
		t.Fatalf("ValidateEvidence() error = %v, want nil", err)
	}
}

func TestValidateEvidenceRejectsBackendModeMismatch(t *testing.T) {
	t.Parallel()

	evidence := validEvidenceFixture()
	evidence.Backends[1].Mode = ModeSemantic

	err := ValidateEvidence(evidence)
	if err == nil {
		t.Fatal("ValidateEvidence() error = nil, want backend-mode mismatch")
	}
	want := "backends[1].mode semantic is not compatible with backend nornicdb_bm25"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateEvidence() error = %q, want substring %q", err, want)
	}
}

func TestValidateEvidenceAllowsZeroNornicDBArtifactWhenPersistenceDisabled(t *testing.T) {
	t.Parallel()

	evidence := validEvidenceFixture()
	evidence.Backends[1].SearchFlags.PersistSearchIndexes = false
	evidence.Backends[1].IndexArtifactBytes = 0

	if err := ValidateEvidence(evidence); err != nil {
		t.Fatalf("ValidateEvidence() error = %v, want nil", err)
	}
}

func TestValidateEvidenceRejectsMissingNornicDBArtifactWhenPersistenceEnabled(t *testing.T) {
	t.Parallel()

	evidence := validEvidenceFixture()
	evidence.Backends[1].SearchFlags.PersistSearchIndexes = true
	evidence.Backends[1].IndexArtifactBytes = 0

	err := ValidateEvidence(evidence)
	if err == nil {
		t.Fatal("ValidateEvidence() error = nil, want persisted artifact error")
	}
	want := "backends[1].index_artifact_bytes is required when search index persistence is enabled"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateEvidence() error = %q, want substring %q", err, want)
	}
}

func TestValidateEvidenceRejectsUnknownFailureClasses(t *testing.T) {
	t.Parallel()

	evidence := validEvidenceFixture()
	evidence.FailureClasses = append(evidence.FailureClasses, FailureClass("trunaction"))

	err := ValidateEvidence(evidence)
	if err == nil {
		t.Fatal("ValidateEvidence() error = nil, want unknown failure class error")
	}
	if want := "failure_classes[trunaction] is invalid"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateEvidence() error = %q, want substring %q", err, want)
	}
}

func TestRequiredFailureClassesMatchesIssue1264(t *testing.T) {
	t.Parallel()

	got := RequiredFailureClasses()
	want := []FailureClass{
		FailureClassTruncation,
		FailureClassTimeout,
		FailureClassDisabledSearch,
		FailureClassLazyWarm,
		FailureClassRebuild,
		FailureClassMissingArtifact,
		FailureClassCorruption,
	}
	if len(got) != len(want) {
		t.Fatalf("len(RequiredFailureClasses()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RequiredFailureClasses()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEvidenceJSONUsesVersionedFieldNames(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(validEvidenceFixture())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		`"eshu_commit"`,
		`"schema_bootstrap_state"`,
		`"level"`,
		`"basis"`,
		`"source_kind_distribution"`,
		`"clean_volume_ns"`,
		`"p50_ns"`,
		`"false_canonical_claim_count"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("json payload = %s, want field %s", body, want)
		}
	}
	if strings.Contains(body, "EshuCommit") || strings.Contains(body, "FalseCanonicalClaimCount") ||
		strings.Contains(body, `"Level"`) || strings.Contains(body, `"Basis"`) {
		t.Fatalf("json payload = %s, want stable evidence field names", body)
	}
}

func TestEvidenceJSONDurationsMarshalAsNanoseconds(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(validEvidenceFixture())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}

	var raw struct {
		Backends []struct {
			Startup struct {
				CleanVolume json.Number `json:"clean_volume_ns"`
			} `json:"startup"`
			Latency struct {
				P50 json.Number `json:"p50_ns"`
			} `json:"latency"`
		} `json:"backends"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		t.Fatalf("json.Decode() error = %v, want nil", err)
	}
	if len(raw.Backends) < 2 {
		t.Fatalf("len(raw.Backends) = %d, want at least 2", len(raw.Backends))
	}
	if got, want := raw.Backends[1].Startup.CleanVolume.String(), "7000000000"; got != want {
		t.Fatalf("clean_volume_ns = %q, want %q", got, want)
	}
	if got, want := raw.Backends[0].Latency.P50.String(), "35000000"; got != want {
		t.Fatalf("p50_ns = %q, want %q", got, want)
	}
}

func TestScoreQueryResultsCountsFalseCanonicalClaims(t *testing.T) {
	t.Parallel()

	metrics := ScoreQueryResults(Query{
		ID:              "q-runtime-owner",
		ExpectedHandles: []string{"service:svc-a", "file:repo-a:cmd/api/main.go"},
	}, []Result{
		{
			Document: searchdocs.Document{
				ID:           "searchdoc:runtime_summary:svc-a",
				GraphHandles: []searchdocs.GraphHandle{{Kind: "service", ID: "svc-a"}},
				TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
			},
			Rank: 1,
		},
		{
			Document: searchdocs.Document{
				ID:           "searchdoc:content_file:repo-a:cmd/api/main.go",
				GraphHandles: []searchdocs.GraphHandle{{Kind: "file", ID: "repo-a:cmd/api/main.go"}},
				TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevel("canonical")},
			},
			Rank: 2,
		},
		{
			Document: searchdocs.Document{
				ID:           "searchdoc:content_file:repo-a:README.md",
				GraphHandles: []searchdocs.GraphHandle{{Kind: "file", ID: "repo-a:README.md"}},
				TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
			},
			Rank: 3,
		},
	})

	if metrics.FalseCanonicalClaimCount == nil {
		t.Fatal("FalseCanonicalClaimCount = nil, want measured count")
	}
	if got, want := *metrics.FalseCanonicalClaimCount, 1; got != want {
		t.Fatalf("FalseCanonicalClaimCount = %d, want %d", got, want)
	}
	if got, want := metrics.Recall, 1.0; got != want {
		t.Fatalf("Recall = %v, want %v", got, want)
	}
	if got, want := metrics.Precision, 2.0/3.0; got != want {
		t.Fatalf("Precision = %v, want %v", got, want)
	}
	if metrics.NDCG <= 0 || metrics.NDCG > 1 {
		t.Fatalf("NDCG = %v, want within (0, 1]", metrics.NDCG)
	}
}

func TestScoreQueryResultsCountsAllExpectedHandlesOnOneResult(t *testing.T) {
	t.Parallel()

	metrics := ScoreQueryResults(Query{
		ID:              "q-multi-handle",
		ExpectedHandles: []string{"service:svc-a", "file:repo-a:cmd/api/main.go"},
	}, []Result{
		{
			Document: searchdocs.Document{
				ID: "searchdoc:runtime_summary:svc-a",
				GraphHandles: []searchdocs.GraphHandle{
					{Kind: "service", ID: "svc-a"},
					{Kind: "file", ID: "repo-a:cmd/api/main.go"},
				},
				TruthScope: searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
			},
			Rank: 1,
		},
	})

	if got, want := metrics.Recall, 1.0; got != want {
		t.Fatalf("Recall = %v, want %v", got, want)
	}
	if got, want := metrics.Precision, 1.0; got != want {
		t.Fatalf("Precision = %v, want %v", got, want)
	}
}

func validEvidenceFixture() Evidence {
	falseClaims := 0
	return Evidence{
		Version:              "search-benchmark-evidence/v1",
		EshuCommit:           "0123456789abcdef",
		SchemaBootstrapState: "bootstrapped",
		TruthScope:           TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
		Corpus: CorpusSummary{
			RepositoryCount: 3,
			FileCount:       42,
			EntityCount:     120,
			DocumentCount:   12,
			VectorCount:     12,
			SourceKindDistribution: map[searchdocs.SourceKind]int{
				searchdocs.SourceKindCodeEntity:     7,
				searchdocs.SourceKindRepositoryFile: 3,
				searchdocs.SourceKindRuntimeSummary: 2,
			},
		},
		Backends: []BackendRun{
			{
				Backend:              BackendPostgresContentSearch,
				Mode:                 ModeKeyword,
				BackendCommit:        "postgres-local",
				QueryCount:           8,
				Latency:              LatencySummary{P50: 35 * time.Millisecond, P95: 90 * time.Millisecond},
				Metrics:              RetrievalMetrics{Recall: 0.75, Precision: 0.70, NDCG: 0.80, FalseCanonicalClaimCount: &falseClaims},
				MemoryHighWaterBytes: 128 * 1024 * 1024,
				IndexArtifactBytes:   0,
				RebuildBehavior:      "not_applicable",
			},
			{
				Backend:       BackendNornicDBBM25,
				Mode:          ModeKeyword,
				BackendImage:  "timothyswt/nornicdb-cpu-bge:v1.1.2",
				BackendCommit: "nornicdb-commit",
				SearchFlags: &NornicDBSearchFlags{
					BM25Enabled:          true,
					VectorEnabled:        false,
					BM25Warming:          "lazy",
					VectorWarming:        "disabled",
					EmbeddingEnabled:     false,
					PersistSearchIndexes: true,
				},
				Startup: StartupSummary{
					CleanVolume:     7 * time.Second,
					PreservedVolume: 5 * time.Second,
				},
				QueryCount:           8,
				Latency:              LatencySummary{P50: 22 * time.Millisecond, P95: 60 * time.Millisecond},
				Metrics:              RetrievalMetrics{Recall: 0.82, Precision: 0.76, NDCG: 0.84, FalseCanonicalClaimCount: &falseClaims},
				MemoryHighWaterBytes: 256 * 1024 * 1024,
				IndexArtifactBytes:   64 * 1024 * 1024,
				RebuildBehavior:      "preserved_index_loaded",
			},
		},
		FailureClasses: RequiredFailureClasses(),
		Recommendation: Recommendation{
			Decision:  RecommendationAddNornicDBSearchLane,
			Rationale: "NornicDB BM25 improved latency and relevance without false canonical claims.",
		},
	}
}
