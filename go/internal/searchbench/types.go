// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdecay"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// Backend identifies one search backend measured by the benchmark harness.
type Backend string

const (
	// BackendPostgresContentSearch is the current Postgres content-search baseline.
	BackendPostgresContentSearch Backend = "postgres_content_search"
	// BackendNornicDBBM25 is curated NornicDB BM25 retrieval over search documents.
	BackendNornicDBBM25 Backend = "nornicdb_bm25"
	// BackendNornicDBVector is curated NornicDB vector retrieval over search documents.
	BackendNornicDBVector Backend = "nornicdb_vector"
	// BackendNornicDBHybrid is curated NornicDB hybrid retrieval over search documents.
	BackendNornicDBHybrid Backend = "nornicdb_hybrid"
)

// Mode identifies the query mode used for one backend run.
type Mode string

const (
	// ModeKeyword measures keyword-only search.
	ModeKeyword Mode = "keyword"
	// ModeSemantic measures semantic vector search.
	ModeSemantic Mode = "semantic"
	// ModeHybrid measures combined keyword and semantic retrieval.
	ModeHybrid Mode = "hybrid"
)

// FailureClass identifies an operator-visible benchmark failure condition.
type FailureClass string

const (
	// FailureClassTruncation records bounded top-K or page truncation.
	FailureClassTruncation FailureClass = "truncation"
	// FailureClassTimeout records query or readiness timeout behavior.
	FailureClassTimeout FailureClass = "timeout"
	// FailureClassDisabledSearch records disabled backend search behavior.
	FailureClassDisabledSearch FailureClass = "disabled_search"
	// FailureClassLazyWarm records first-query lazy warm behavior.
	FailureClassLazyWarm FailureClass = "lazy_warm"
	// FailureClassRebuild records index rebuild behavior.
	FailureClassRebuild FailureClass = "rebuild"
	// FailureClassMissingArtifact records missing persisted search artifacts.
	FailureClassMissingArtifact FailureClass = "missing_artifact"
	// FailureClassCorruption records corrupt persisted search artifacts.
	FailureClassCorruption FailureClass = "corruption"
)

// RecommendationDecision is the benchmark conclusion.
type RecommendationDecision string

const (
	// RecommendationKeepPostgresSearch keeps the current Postgres search lane.
	RecommendationKeepPostgresSearch RecommendationDecision = "keep_postgres_search"
	// RecommendationAddNornicDBSearchLane adds NornicDB as a separate search lane.
	RecommendationAddNornicDBSearchLane RecommendationDecision = "add_nornicdb_search_lane"
	// RecommendationDeferSearchChange defers NornicDB search adoption.
	RecommendationDeferSearchChange RecommendationDecision = "defer_search_change"
)

// Evidence is one versioned search benchmark evidence record.
type Evidence struct {
	Version              string         `json:"version"`
	EshuCommit           string         `json:"eshu_commit"`
	SchemaBootstrapState string         `json:"schema_bootstrap_state"`
	TruthScope           TruthScope     `json:"truth_scope"`
	Corpus               CorpusSummary  `json:"corpus"`
	Backends             []BackendRun   `json:"backends"`
	FailureClasses       []FailureClass `json:"failure_classes"`
	Recommendation       Recommendation `json:"recommendation"`
}

// TruthScope records the evidence authority for a benchmark run.
type TruthScope struct {
	Level searchdocs.TruthLevel `json:"level"`
	Basis searchdocs.TruthBasis `json:"basis"`
}

// CorpusSummary describes the indexed input corpus used by a benchmark run.
type CorpusSummary struct {
	RepositoryCount        int                           `json:"repository_count"`
	FileCount              int                           `json:"file_count"`
	EntityCount            int                           `json:"entity_count"`
	DocumentCount          int                           `json:"document_count"`
	VectorCount            int                           `json:"vector_count"`
	SourceKindDistribution map[searchdocs.SourceKind]int `json:"source_kind_distribution"`
}

// BackendRun is one backend and mode measurement within an evidence record.
type BackendRun struct {
	Backend              Backend              `json:"backend"`
	Mode                 Mode                 `json:"mode"`
	BackendImage         string               `json:"backend_image,omitempty"`
	BackendCommit        string               `json:"backend_commit,omitempty"`
	SearchFlags          *NornicDBSearchFlags `json:"search_flags,omitempty"`
	Startup              StartupSummary       `json:"startup"`
	QueryCount           int                  `json:"query_count"`
	Latency              LatencySummary       `json:"latency"`
	Metrics              RetrievalMetrics     `json:"metrics"`
	MemoryHighWaterBytes int64                `json:"memory_high_water_bytes"`
	IndexArtifactBytes   int64                `json:"index_artifact_bytes"`
	RebuildBehavior      string               `json:"rebuild_behavior"`
}

// NornicDBSearchFlags records the effective NornicDB search controls for a run.
type NornicDBSearchFlags struct {
	BM25Enabled          bool   `json:"bm25_enabled"`
	VectorEnabled        bool   `json:"vector_enabled"`
	BM25Warming          string `json:"bm25_warming"`
	VectorWarming        string `json:"vector_warming"`
	EmbeddingEnabled     bool   `json:"embedding_enabled"`
	PersistSearchIndexes bool   `json:"persist_search_indexes"`
}

// StartupSummary records clean and preserved volume startup timings.
type StartupSummary struct {
	CleanVolume     time.Duration `json:"clean_volume_ns"`
	PreservedVolume time.Duration `json:"preserved_volume_ns"`
}

// LatencySummary records query latency distribution for one backend run.
type LatencySummary struct {
	P50 time.Duration `json:"p50_ns"`
	P95 time.Duration `json:"p95_ns"`
}

// RetrievalMetrics records accuracy metrics for one backend run.
type RetrievalMetrics struct {
	Recall                   float64 `json:"recall"`
	Precision                float64 `json:"precision"`
	NDCG                     float64 `json:"ndcg"`
	FalseCanonicalClaimCount *int    `json:"false_canonical_claim_count"`
}

// Recommendation records the benchmark decision and why it is safe.
type Recommendation struct {
	Decision  RecommendationDecision `json:"decision"`
	Rationale string                 `json:"rationale"`
}

// QuerySuiteVersion is the first semantic retrieval query-suite schema version.
const QuerySuiteVersion = "semantic-retrieval-query-suite/v1"

// MinimumQuerySuiteSize is the issue #417 baseline query count.
const MinimumQuerySuiteSize = 15

// MaximumQueryLimit is the maximum top-K limit for benchmark suite queries.
const MaximumQueryLimit = 100

// QuerySuite is one versioned semantic retrieval evaluation suite.
type QuerySuite struct {
	Version string  `json:"version"`
	Queries []Query `json:"queries"`
}

// Query is one evaluation query scored by the harness.
type Query struct {
	ID              string   `json:"id"`
	Text            string   `json:"text"`
	ServiceID       string   `json:"service_id,omitempty"`
	WorkloadID      string   `json:"workload_id,omitempty"`
	RepoID          string   `json:"repo_id,omitempty"`
	Environment     string   `json:"environment,omitempty"`
	Mode            Mode     `json:"mode"`
	Limit           int      `json:"limit"`
	ExpectedHandles []string `json:"expected_handles"`
}

// Result is one ranked retrieval result for a query.
type Result struct {
	Document searchdocs.Document `json:"document"`
	Rank     int                 `json:"rank"`
}

// DecayEvaluationInput is one pure decay-scoring benchmark evaluation.
type DecayEvaluationInput struct {
	Query      Query              `json:"query"`
	Candidates []DecayCandidate   `json:"candidates"`
	Scorer     searchdecay.Scorer `json:"-"`
}

// DecayCandidate couples one ranked result with the evidence score to decay.
type DecayCandidate struct {
	Result   Result               `json:"result"`
	Evidence searchdecay.Evidence `json:"evidence"`
}

// DecayEvaluation records before/after ranking evidence for one query.
type DecayEvaluation struct {
	QueryID                       string                  `json:"query_id"`
	PolicyID                      string                  `json:"policy_id"`
	MetricsBefore                 RetrievalMetrics        `json:"metrics_before"`
	MetricsAfter                  RetrievalMetrics        `json:"metrics_after"`
	RecallDelta                   float64                 `json:"recall_delta"`
	PrecisionDelta                float64                 `json:"precision_delta"`
	NDCGDelta                     float64                 `json:"ndcg_delta"`
	RequiredEvidenceVisibleBefore bool                    `json:"required_evidence_visible_before"`
	RequiredEvidenceVisibleAfter  bool                    `json:"required_evidence_visible_after"`
	FalseCanonicalCandidateCount  int                     `json:"false_canonical_candidate_count"`
	Results                       []DecayEvaluationResult `json:"results"`
}

// DecayEvaluationResult records how decay changed one candidate's rank metadata.
type DecayEvaluationResult struct {
	DocumentID    string              `json:"document_id"`
	OriginalRank  int                 `json:"original_rank"`
	DecayedRank   int                 `json:"decayed_rank"`
	OriginalScore float64             `json:"original_score"`
	DecayedScore  float64             `json:"decayed_score"`
	DecayOutcome  searchdecay.Outcome `json:"decay_outcome"`
	Required      bool                `json:"required"`
}

// RerankEvaluationInput is one pure reranking benchmark evaluation.
type RerankEvaluationInput struct {
	Query                  Query                  `json:"query"`
	Baseline               []Result               `json:"baseline"`
	Reranked               []Result               `json:"reranked"`
	BaselineHybridEvidence RerankBaselineEvidence `json:"baseline_hybrid_evidence"`
	BaselineLatency        time.Duration          `json:"baseline_latency_ns"`
	RerankedLatency        time.Duration          `json:"reranked_latency_ns"`
	BaselineCostMicrosUSD  int64                  `json:"baseline_cost_micros_usd"`
	RerankedCostMicrosUSD  int64                  `json:"reranked_cost_micros_usd"`
}

// RerankBaselineEvidence ties a rerank eval to prior hybrid retrieval evidence.
type RerankBaselineEvidence struct {
	EvidenceID string  `json:"evidence_id"`
	Backend    Backend `json:"backend"`
	Mode       Mode    `json:"mode"`
}

// RerankEvaluation records baseline and reranked evidence for one query.
type RerankEvaluation struct {
	QueryID                      string                   `json:"query_id"`
	BaselineHybridEvidence       RerankBaselineEvidence   `json:"baseline_hybrid_evidence"`
	BaselineMetrics              RetrievalMetrics         `json:"baseline_metrics"`
	RerankedMetrics              RetrievalMetrics         `json:"reranked_metrics"`
	RecallDelta                  float64                  `json:"recall_delta"`
	PrecisionDelta               float64                  `json:"precision_delta"`
	NDCGDelta                    float64                  `json:"ndcg_delta"`
	FalseCanonicalClaimDelta     int                      `json:"false_canonical_claim_delta"`
	FalseCanonicalCandidateCount int                      `json:"false_canonical_candidate_count"`
	BaselineLatency              time.Duration            `json:"baseline_latency_ns"`
	RerankedLatency              time.Duration            `json:"reranked_latency_ns"`
	LatencyDelta                 time.Duration            `json:"latency_delta_ns"`
	BaselineCostMicrosUSD        int64                    `json:"baseline_cost_micros_usd"`
	RerankedCostMicrosUSD        int64                    `json:"reranked_cost_micros_usd"`
	CostDeltaMicrosUSD           int64                    `json:"cost_delta_micros_usd"`
	Results                      []RerankEvaluationResult `json:"results"`
}

// RerankEvaluationResult records how reranking changed one result rank.
type RerankEvaluationResult struct {
	DocumentID   string `json:"document_id"`
	BaselineRank int    `json:"baseline_rank,omitempty"`
	RerankedRank int    `json:"reranked_rank,omitempty"`
	Required     bool   `json:"required"`
}

// ProtocolCandidate identifies a search protocol option under review.
type ProtocolCandidate string

const (
	// ProtocolCandidateCurrentAPIMCP keeps the existing API/MCP search path.
	ProtocolCandidateCurrentAPIMCP ProtocolCandidate = "current_api_mcp_search"
	// ProtocolCandidateGraphQL evaluates a GraphQL query protocol.
	ProtocolCandidateGraphQL ProtocolCandidate = "graphql_query_protocol"
	// ProtocolCandidateGRPC evaluates a generic gRPC query protocol.
	ProtocolCandidateGRPC ProtocolCandidate = "grpc_query_protocol"
	// ProtocolCandidateQdrantGRPC evaluates a Qdrant gRPC adapter path.
	ProtocolCandidateQdrantGRPC ProtocolCandidate = "qdrant_grpc_adapter"
	// ProtocolCandidateNornicNative evaluates a Nornic native protocol path.
	ProtocolCandidateNornicNative ProtocolCandidate = "nornic_native_protocol"
	// ProtocolCandidateDeferred records that no protocol candidate is ready.
	ProtocolCandidateDeferred ProtocolCandidate = "defer_protocol_expansion"
)

// ProtocolDecision is the protocol recommendation outcome.
type ProtocolDecision string

const (
	// ProtocolDecisionKeepCurrentPath keeps the current API/MCP search path.
	ProtocolDecisionKeepCurrentPath ProtocolDecision = "keep_current_api_mcp_path"
	// ProtocolDecisionAddProtocol recommends adding the candidate protocol.
	ProtocolDecisionAddProtocol ProtocolDecision = "add_protocol"
	// ProtocolDecisionDeferExpansion defers protocol expansion.
	ProtocolDecisionDeferExpansion ProtocolDecision = "defer_protocol_expansion"
)

// ProtocolUserValue identifies the user value claimed by a protocol option.
type ProtocolUserValue string

const (
	// ProtocolUserValueLatency claims query latency value.
	ProtocolUserValueLatency ProtocolUserValue = "latency"
	// ProtocolUserValueCost claims cost value.
	ProtocolUserValueCost ProtocolUserValue = "cost"
	// ProtocolUserValueOperability claims operator usability value.
	ProtocolUserValueOperability ProtocolUserValue = "operability"
	// ProtocolUserValueSecurity claims security or authorization value.
	ProtocolUserValueSecurity ProtocolUserValue = "security"
	// ProtocolUserValueIncidentDebug claims incident-debugging value.
	ProtocolUserValueIncidentDebug ProtocolUserValue = "incident_debug"
)

// ProtocolImpactDirection records the direction of a measured or deferred impact.
type ProtocolImpactDirection string

const (
	// ProtocolImpactImproved records an improvement.
	ProtocolImpactImproved ProtocolImpactDirection = "improved"
	// ProtocolImpactRegressed records a regression.
	ProtocolImpactRegressed ProtocolImpactDirection = "regressed"
	// ProtocolImpactNeutral records no expected change.
	ProtocolImpactNeutral ProtocolImpactDirection = "neutral"
	// ProtocolImpactUnknown records deferred impact proof.
	ProtocolImpactUnknown ProtocolImpactDirection = "unknown"
)

// ProtocolAssessmentCategory records a low-cardinality risk or burden level.
type ProtocolAssessmentCategory string

const (
	// ProtocolAssessmentNone records no expected risk or burden.
	ProtocolAssessmentNone ProtocolAssessmentCategory = "none"
	// ProtocolAssessmentLow records low expected risk or burden.
	ProtocolAssessmentLow ProtocolAssessmentCategory = "low"
	// ProtocolAssessmentMedium records medium expected risk or burden.
	ProtocolAssessmentMedium ProtocolAssessmentCategory = "medium"
	// ProtocolAssessmentHigh records high expected risk or burden.
	ProtocolAssessmentHigh ProtocolAssessmentCategory = "high"
	// ProtocolAssessmentUnknown records deferred or unknown risk proof.
	ProtocolAssessmentUnknown ProtocolAssessmentCategory = "unknown"
)

// ProtocolRecommendation records a bounded search protocol decision.
type ProtocolRecommendation struct {
	BaselineHybridEvidence       RerankBaselineEvidence     `json:"baseline_hybrid_evidence"`
	CandidateProtocol            ProtocolCandidate          `json:"candidate_protocol"`
	Decision                     ProtocolDecision           `json:"decision"`
	Rationale                    string                     `json:"rationale"`
	ExpectedUserValue            []ProtocolValueEvidence    `json:"expected_user_value"`
	MigrationRisk                ProtocolAssessmentCategory `json:"migration_risk"`
	SecurityRisk                 ProtocolAssessmentCategory `json:"security_risk"`
	OperatorBurden               ProtocolAssessmentCategory `json:"operator_burden"`
	LatencyImpact                ProtocolImpact             `json:"latency_impact"`
	CostImpact                   ProtocolImpact             `json:"cost_impact"`
	FallbackBehavior             string                     `json:"fallback_behavior"`
	APIMCPAuthorizationPreserved bool                       `json:"api_mcp_authorization_preserved"`
}

// ProtocolValueEvidence records proof for one claimed user value.
type ProtocolValueEvidence struct {
	Value          ProtocolUserValue `json:"value"`
	Evidence       string            `json:"evidence,omitempty"`
	DeferredReason string            `json:"deferred_reason,omitempty"`
}

// ProtocolImpact records measured or deferred latency and cost impact.
type ProtocolImpact struct {
	Direction      ProtocolImpactDirection `json:"direction"`
	Evidence       string                  `json:"evidence,omitempty"`
	DeferredReason string                  `json:"deferred_reason,omitempty"`
}

// QueryScore records metrics for one query in a query-suite run.
type QueryScore struct {
	QueryID string           `json:"query_id"`
	Metrics RetrievalMetrics `json:"metrics"`
}

// QuerySuiteScore records aggregate and per-query semantic retrieval metrics.
type QuerySuiteScore struct {
	QueryCount int              `json:"query_count"`
	Metrics    RetrievalMetrics `json:"metrics"`
	PerQuery   []QueryScore     `json:"per_query"`
}
