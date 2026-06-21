package status

import (
	"context"
	"time"
)

// RawSnapshot is the read-only substrate snapshot gathered from Postgres.
type RawSnapshot struct {
	AsOf                  time.Time
	ScopeCounts           []NamedCount
	GenerationCounts      []NamedCount
	ScopeActivity         ScopeActivitySnapshot
	GenerationHistory     GenerationHistorySnapshot
	GenerationTransitions []GenerationTransitionSnapshot
	StageCounts           []StageStatusCount
	DomainBacklogs        []DomainBacklog
	ProducerActivity      ProducerActivitySnapshot
	QueueBlockages        []QueueBlockage
	RetryPolicies         []RetryPolicySummary
	Queue                 QueueSnapshot
	LatestQueueFailure    *QueueFailureSnapshot
	Coordinator           *CoordinatorSnapshot
	RegistryCollectors    []RegistryCollectorSnapshot
	AWSCloudScans         []AWSCloudScanStatus
	AWSFreshness          AWSFreshnessSnapshot
	VulnerabilitySources  []VulnerabilitySourceState
	SemanticExtraction    SemanticExtractionStatus
	AnswerNarration       AnswerNarrationStatus
	// CollectorGenerationDeadLetters captures commit failures that happened
	// before normal projector/reducer queue rows existed.
	CollectorGenerationDeadLetters CollectorGenerationDeadLetterSnapshot
	CollectorFactEvidence          []CollectorFactEvidence
	// AWSCloudScansTruncated reports that the reader returned the configured
	// row cap instead of every AWS scan tuple.
	AWSCloudScansTruncated bool
	// AWSCloudScanLimit is the row cap used when AWSCloudScansTruncated is true.
	AWSCloudScanLimit int
	// TerraformStateLastSerials carries the most recent observed serial per
	// active state_snapshot scope, keyed by safe_locator_hash. Empty when the
	// reader does not surface tfstate evidence.
	TerraformStateLastSerials []TerraformStateLocatorSerial
	// TerraformStateRecentWarnings carries up to MaxTerraformStateRecentWarnings
	// warning_fact rows per safe_locator_hash so operators can see recent
	// warnings without scanning the fact stream.
	TerraformStateRecentWarnings []TerraformStateLocatorWarning
}

// SnapshotSelection controls which optional, expensive sections a status reader
// gathers. The index status surface excludes the fact_records-derived sections
// it never renders so it avoids full-table aggregates that dominate read time at
// repository scale.
type SnapshotSelection struct {
	// IncludeCollectorFactEvidence requests RawSnapshot.CollectorFactEvidence,
	// which aggregates the active fact_records table.
	IncludeCollectorFactEvidence bool
	// IncludeRegistryCollectors requests RawSnapshot.RegistryCollectors, which
	// reads registry collector status from fact_records.
	IncludeRegistryCollectors bool
}

// FullSnapshotSelection returns the selection that includes every optional
// section. It is the back-compatible default used by full status-report
// surfaces (CLI status, admin status, collector readiness).
func FullSnapshotSelection() SnapshotSelection {
	return SnapshotSelection{
		IncludeCollectorFactEvidence: true,
		IncludeRegistryCollectors:    true,
	}
}

// Reader loads the raw status snapshot from an underlying storage backend.
type Reader interface {
	// ReadStatusSnapshot returns the full snapshot. It behaves as
	// ReadStatusSnapshotFiltered called with FullSnapshotSelection().
	ReadStatusSnapshot(context.Context, time.Time) (RawSnapshot, error)
	// ReadStatusSnapshotFiltered returns a snapshot whose optional sections are
	// gathered according to the selection. Excluded sections are left at their
	// zero value so callers that never render them avoid the underlying cost.
	ReadStatusSnapshotFiltered(context.Context, time.Time, SnapshotSelection) (RawSnapshot, error)
}

// Options controls operator-health projection behavior.
type Options struct {
	StallAfter  time.Duration
	DomainLimit int
}

// HealthSummary captures the operator-facing health verdict and reasons.
type HealthSummary struct {
	State   string   `json:"state"`
	Reasons []string `json:"reasons"`
}

// StageSummary collapses queue counts into one row per stage.
type StageSummary struct {
	Stage      string `json:"stage"`
	Pending    int    `json:"pending"`
	Claimed    int    `json:"claimed"`
	Running    int    `json:"running"`
	Retrying   int    `json:"retrying"`
	Succeeded  int    `json:"succeeded"`
	Failed     int    `json:"failed"`
	DeadLetter int    `json:"dead_letter"`
}

// Report is the operator-facing summary rendered by CLI and future admin APIs.
type Report struct {
	AsOf                           time.Time
	Health                         HealthSummary
	FlowSummaries                  []FlowSummary
	Queue                          QueueSnapshot
	RetryPolicies                  []RetryPolicySummary
	ScopeActivity                  ScopeActivitySnapshot
	GenerationHistory              GenerationHistorySnapshot
	GenerationTransitions          []GenerationTransitionSnapshot
	ScopeTotals                    map[string]int
	GenerationTotals               map[string]int
	StageSummaries                 []StageSummary
	DomainBacklogs                 []DomainBacklog
	QueueBlockages                 []QueueBlockage
	LatestQueueFailure             *QueueFailureSnapshot
	Coordinator                    *CoordinatorSnapshot
	RegistryCollectors             []RegistryCollectorSnapshot
	AWSCloudScans                  []AWSCloudScanStatus
	AWSFreshness                   AWSFreshnessSnapshot
	VulnerabilitySources           []VulnerabilitySourceState
	SemanticExtraction             SemanticExtractionStatus
	AnswerNarration                AnswerNarrationStatus
	CollectorGenerationDeadLetters CollectorGenerationDeadLetterSnapshot
	CollectorFactEvidence          []CollectorFactEvidence
	AWSCloudScansTruncated         bool
	AWSCloudScanLimit              int
	// TerraformState carries the operator-facing tfstate admin status section
	// derived from RawSnapshot.TerraformStateLastSerials and
	// RawSnapshot.TerraformStateRecentWarnings. Empty when the reader did not
	// surface tfstate evidence.
	TerraformState TerraformStateReport
}

// DefaultOptions returns the baseline operator heuristics for this first live
// status surface.
func DefaultOptions() Options {
	return Options{
		StallAfter:  10 * time.Minute,
		DomainLimit: 5,
	}
}
