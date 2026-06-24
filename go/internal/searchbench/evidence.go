// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// EvidenceVersion is the versioned search-benchmark evidence schema identifier.
// Live executors that assemble Evidence records must stamp this exact value so
// ValidateEvidence and the recorded design-doc evidence cannot drift apart.
const EvidenceVersion = "search-benchmark-evidence/v1"

const evidenceVersion = EvidenceVersion

// ValidateEvidence checks the issue #1264 evidence contract.
func ValidateEvidence(evidence Evidence) error {
	var problems []string
	if evidence.Version != evidenceVersion {
		problems = append(problems, "version must be "+evidenceVersion)
	}
	if evidence.EshuCommit == "" {
		problems = append(problems, "eshu_commit is required")
	}
	if evidence.SchemaBootstrapState == "" {
		problems = append(problems, "schema_bootstrap_state is required")
	}
	if evidence.TruthScope.Level != searchdocs.TruthLevelDerived {
		problems = append(problems, "truth_scope.level must be derived")
	}
	if !validTruthBasis(evidence.TruthScope.Basis) {
		problems = append(problems, "truth_scope.basis is invalid")
	}
	problems = append(problems, validateCorpus(evidence.Corpus)...)

	hasPostgres := false
	hasNornicDB := false
	for i, backend := range evidence.Backends {
		if backend.Backend == BackendPostgresContentSearch {
			hasPostgres = true
		}
		if isNornicDBBackend(backend.Backend) {
			hasNornicDB = true
		}
		problems = append(problems, validateBackendRun(i, backend)...)
	}
	if len(evidence.Backends) == 0 {
		problems = append(problems, "backends are required")
	}
	if !hasPostgres {
		problems = append(problems, "postgres_content_search backend is required")
	}
	if !hasNornicDB {
		problems = append(problems, "at least one nornicdb backend is required")
	}
	problems = append(problems, validateFailureClasses(evidence.FailureClasses)...)
	problems = append(problems, validateRecommendation(evidence.Recommendation)...)
	return joinedValidationError(problems)
}

// RequiredFailureClasses returns the complete issue #1264 failure-class contract.
func RequiredFailureClasses() []FailureClass {
	return []FailureClass{
		FailureClassTruncation,
		FailureClassTimeout,
		FailureClassDisabledSearch,
		FailureClassLazyWarm,
		FailureClassRebuild,
		FailureClassMissingArtifact,
		FailureClassCorruption,
	}
}

// ScoreQueryResults computes retrieval accuracy and truth-claim metrics.
func ScoreQueryResults(query Query, results []Result) RetrievalMetrics {
	falseCanonicalClaims := 0
	expected := make(map[string]struct{}, len(query.ExpectedHandles))
	for _, handle := range query.ExpectedHandles {
		if handle = strings.TrimSpace(handle); handle != "" {
			expected[handle] = struct{}{}
		}
	}

	matched := make(map[string]struct{}, len(expected))
	hitCount := 0
	dcg := 0.0
	for i, result := range results {
		if result.Document.TruthScope.Level != searchdocs.TruthLevelDerived {
			falseCanonicalClaims++
		}
		rank := result.Rank
		if rank <= 0 {
			rank = i + 1
		}
		if resultMatchesExpected(result, expected, matched) {
			hitCount++
			dcg += 1 / math.Log2(float64(rank)+1)
		}
	}

	metrics := RetrievalMetrics{FalseCanonicalClaimCount: &falseCanonicalClaims}
	if len(expected) > 0 {
		metrics.Recall = float64(len(matched)) / float64(len(expected))
		metrics.NDCG = normalizedDCG(dcg, len(expected), len(results))
	}
	if len(results) > 0 {
		metrics.Precision = float64(hitCount) / float64(len(results))
	}
	return metrics
}

func validateCorpus(corpus CorpusSummary) []string {
	var problems []string
	for name, count := range map[string]int{
		"repository_count": corpus.RepositoryCount,
		"file_count":       corpus.FileCount,
		"entity_count":     corpus.EntityCount,
		"vector_count":     corpus.VectorCount,
	} {
		if count < 0 {
			problems = append(problems, fmt.Sprintf("corpus.%s must be non-negative", name))
		}
	}
	if corpus.DocumentCount <= 0 {
		problems = append(problems, "corpus.document_count is required")
	}
	if len(corpus.SourceKindDistribution) == 0 {
		problems = append(problems, "corpus.source_kind_distribution is required")
		return problems
	}
	total := 0
	for sourceKind, count := range corpus.SourceKindDistribution {
		if count < 0 {
			problems = append(problems, fmt.Sprintf("corpus.source_kind_distribution[%s] must be non-negative", sourceKind))
		}
		total += count
	}
	if corpus.DocumentCount > 0 && total != corpus.DocumentCount {
		problems = append(problems, "corpus.source_kind_distribution must sum to corpus.document_count")
	}
	return problems
}

func validateBackendRun(index int, run BackendRun) []string {
	var problems []string
	prefix := fmt.Sprintf("backends[%d]", index)
	if !validBackend(run.Backend) {
		problems = append(problems, prefix+".backend is invalid")
	}
	if !validMode(run.Mode) {
		problems = append(problems, prefix+".mode is invalid")
	}
	if validBackend(run.Backend) && validMode(run.Mode) && !compatibleBackendMode(run.Backend, run.Mode) {
		problems = append(problems, fmt.Sprintf("%s.mode %s is not compatible with backend %s", prefix, run.Mode, run.Backend))
	}
	if run.BackendCommit == "" && run.BackendImage == "" {
		problems = append(problems, prefix+".backend_image or backend_commit is required")
	}
	if run.QueryCount <= 0 {
		problems = append(problems, prefix+".query_count is required")
	}
	if run.Latency.P50 <= 0 {
		problems = append(problems, prefix+".latency.p50 is required")
	}
	if run.Latency.P95 <= 0 {
		problems = append(problems, prefix+".latency.p95 is required")
	}
	if run.Latency.P50 > 0 && run.Latency.P95 > 0 && run.Latency.P95 < run.Latency.P50 {
		problems = append(problems, prefix+".latency.p95 must be greater than or equal to p50")
	}
	problems = append(problems, validateMetrics(prefix, run.Metrics)...)
	if run.MemoryHighWaterBytes <= 0 {
		problems = append(problems, prefix+".memory_high_water_bytes is required")
	}
	if run.IndexArtifactBytes < 0 {
		problems = append(problems, prefix+".index_artifact_bytes must be non-negative")
	}
	if run.RebuildBehavior == "" {
		problems = append(problems, prefix+".rebuild_behavior is required")
	}
	if isNornicDBBackend(run.Backend) {
		problems = append(problems, validateNornicDBRun(prefix, run)...)
	}
	return problems
}

func validateNornicDBRun(prefix string, run BackendRun) []string {
	var problems []string
	if run.SearchFlags == nil {
		return append(problems, prefix+".nornicdb search flags are required")
	}
	if run.SearchFlags.BM25Warming == "" {
		problems = append(problems, prefix+".search_flags.bm25_warming is required")
	}
	if run.SearchFlags.VectorWarming == "" {
		problems = append(problems, prefix+".search_flags.vector_warming is required")
	}
	if (run.Backend == BackendNornicDBBM25 || run.Backend == BackendNornicDBHybrid) && !run.SearchFlags.BM25Enabled {
		problems = append(problems, prefix+".search_flags.bm25_enabled must be true for bm25 or hybrid runs")
	}
	if (run.Backend == BackendNornicDBVector || run.Backend == BackendNornicDBHybrid) && !run.SearchFlags.VectorEnabled {
		problems = append(problems, prefix+".search_flags.vector_enabled must be true for vector or hybrid runs")
	}
	if run.Startup.CleanVolume <= 0 {
		problems = append(problems, prefix+".startup.clean_volume is required")
	}
	if run.Startup.PreservedVolume <= 0 {
		problems = append(problems, prefix+".startup.preserved_volume is required")
	}
	if run.SearchFlags.PersistSearchIndexes && run.IndexArtifactBytes <= 0 {
		problems = append(problems, prefix+".index_artifact_bytes is required when search index persistence is enabled")
	}
	return problems
}

func validateMetrics(prefix string, metrics RetrievalMetrics) []string {
	var problems []string
	for name, value := range map[string]float64{
		"recall":    metrics.Recall,
		"precision": metrics.Precision,
		"ndcg":      metrics.NDCG,
	} {
		if value < 0 || value > 1 {
			problems = append(problems, fmt.Sprintf("%s.metrics.%s must be between 0 and 1", prefix, name))
		}
	}
	if metrics.FalseCanonicalClaimCount == nil {
		problems = append(problems, prefix+".metrics.false_canonical_claim_count is required")
	} else if *metrics.FalseCanonicalClaimCount < 0 {
		problems = append(problems, prefix+".metrics.false_canonical_claim_count must be non-negative")
	}
	return problems
}

func validateFailureClasses(classes []FailureClass) []string {
	if len(classes) == 0 {
		return []string{"failure_classes are required"}
	}
	var problems []string
	seen := make(map[FailureClass]struct{}, len(classes))
	for _, class := range classes {
		if !validFailureClass(class) {
			problems = append(problems, fmt.Sprintf("failure_classes[%s] is invalid", class))
		}
		seen[class] = struct{}{}
	}
	for _, required := range RequiredFailureClasses() {
		if _, ok := seen[required]; !ok {
			problems = append(problems, fmt.Sprintf("failure_classes must include %s", required))
		}
	}
	return problems
}

func validTruthBasis(basis searchdocs.TruthBasis) bool {
	switch basis {
	case searchdocs.TruthBasisContentIndex, searchdocs.TruthBasisReadModel:
		return true
	default:
		return false
	}
}

func validFailureClass(class FailureClass) bool {
	switch class {
	case FailureClassTruncation,
		FailureClassTimeout,
		FailureClassDisabledSearch,
		FailureClassLazyWarm,
		FailureClassRebuild,
		FailureClassMissingArtifact,
		FailureClassCorruption:
		return true
	default:
		return false
	}
}

func validateRecommendation(recommendation Recommendation) []string {
	var problems []string
	switch recommendation.Decision {
	case RecommendationKeepPostgresSearch, RecommendationAddNornicDBSearchLane, RecommendationDeferSearchChange:
	default:
		problems = append(problems, "recommendation.decision is invalid")
	}
	if strings.TrimSpace(recommendation.Rationale) == "" {
		problems = append(problems, "recommendation.rationale is required")
	}
	return problems
}

func validBackend(backend Backend) bool {
	switch backend {
	case BackendPostgresContentSearch, BackendNornicDBBM25, BackendNornicDBVector, BackendNornicDBHybrid:
		return true
	default:
		return false
	}
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeKeyword, ModeSemantic, ModeHybrid:
		return true
	default:
		return false
	}
}

func isNornicDBBackend(backend Backend) bool {
	switch backend {
	case BackendNornicDBBM25, BackendNornicDBVector, BackendNornicDBHybrid:
		return true
	default:
		return false
	}
}

func compatibleBackendMode(backend Backend, mode Mode) bool {
	switch backend {
	case BackendPostgresContentSearch, BackendNornicDBBM25:
		return mode == ModeKeyword
	case BackendNornicDBVector:
		return mode == ModeSemantic
	case BackendNornicDBHybrid:
		return mode == ModeHybrid
	default:
		return false
	}
}

func resultMatchesExpected(result Result, expected map[string]struct{}, matched map[string]struct{}) bool {
	matchedResult := false
	for _, handle := range result.Document.GraphHandles {
		key := handleKey(handle)
		if _, ok := expected[key]; !ok {
			continue
		}
		if _, ok := matched[key]; ok {
			continue
		}
		matched[key] = struct{}{}
		matchedResult = true
	}
	return matchedResult
}

func handleKey(handle searchdocs.GraphHandle) string {
	if handle.Kind == "" || handle.ID == "" {
		return ""
	}
	return handle.Kind + ":" + handle.ID
}

func normalizedDCG(dcg float64, expectedCount int, resultCount int) float64 {
	idealHits := expectedCount
	if resultCount < idealHits {
		idealHits = resultCount
	}
	if idealHits <= 0 {
		return 0
	}
	ideal := 0.0
	for rank := 1; rank <= idealHits; rank++ {
		ideal += 1 / math.Log2(float64(rank)+1)
	}
	if ideal == 0 {
		return 0
	}
	return dcg / ideal
}

func joinedValidationError(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	errs := make([]error, 0, len(problems))
	for _, problem := range problems {
		errs = append(errs, errors.New(problem))
	}
	return errors.Join(errs...)
}
