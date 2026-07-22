// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// SupplyChainImpactWinnersReadEnv is the operator gate (#3389 Phase 2) that
// switches the impact-findings list read onto the maintained
// supply_chain_impact_canonical_winners read model. Default (unset/anything but
// "true") keeps the legacy read-time dedup. Enable only after confirming the
// reducer maintainer has populated the winners table.
const SupplyChainImpactWinnersReadEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ"

// SupplyChainImpactWinnersReadEnabled parses the Phase 2 read gate value using
// the same bool semantics the env registry validates with (strconv.ParseBool),
// so values like "1", "t", and "TRUE" enable the gate consistently with
// `eshu config validate`. Unparseable or empty values leave the gate off.
func SupplyChainImpactWinnersReadEnabled(value string) bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && enabled
}

// SupplyChainImpactFindingStore reads reducer-owned vulnerability impact
// findings.
type SupplyChainImpactFindingStore interface {
	ListSupplyChainImpactFindings(context.Context, SupplyChainImpactFindingFilter) ([]SupplyChainImpactFindingRow, error)
}

// SupplyChainImpactFindingFilter bounds impact reads to concrete evidence,
// impact truth, triage priority, or VEX/operator suppression state.
// DetectionProfile only narrows an already scoped read. SuppressionState
// filters by the reducer suppression decision (active, not_affected,
// accepted_risk, false_positive, ignored, expired, provider_dismissed,
// scope_mismatch); IncludeSuppressed admits findings whose suppression
// state hides them from the default view (not_affected, accepted_risk,
// false_positive, ignored). Expired, provider_dismissed, and
// scope_mismatch findings remain visible regardless because they keep
// operator audit signal.
type SupplyChainImpactFindingFilter struct {
	CVEID, AdvisoryID                      string
	PackageID, RepositoryID, SubjectDigest string
	ImageRef                               string
	ImpactStatus, Ecosystem, Severity      string
	WorkloadID, ServiceID, Environment     string
	DetectionProfile, PriorityBucket, Sort string
	SuppressionState, AfterFindingID       string
	MinPriorityScore, Limit                int
	IncludeSuppressed                      bool
	// AllowedRepositoryIDs and AllowedScopeIDs carry scoped-token repository
	// and ingestion-scope grants. When both are empty the read is
	// unrestricted (shared token, all-scope admin, or local dev mode). When
	// either is populated the query intersects impact facts with the granted
	// set before ordering, limits, truncation, and cursor metadata so a
	// scoped caller never observes or counts out-of-grant findings — even
	// when the request anchors on a non-repository selector such as CVE,
	// advisory, package, image, service, workload, or environment.
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// SupplyChainImpactFindingRow is one durable impact finding decoded from
// reducer-owned facts.
type SupplyChainImpactFindingRow struct {
	FindingID       string
	CVEID           string
	AdvisoryID      string
	PackageID       string
	Ecosystem       string
	PackageName     string
	PURL            string
	ProductCriteria string
	MatchCriteriaID string
	ObservedVersion string
	RequestedRange  string
	FixedVersion    string
	// VulnerableRange is the source-reported affected range expression
	// copied from the advisory the reducer's provenance selector picked.
	// Persisted on the canonical finding payload so list responses expose
	// the same value as the explain route. Older rows that predate the
	// reducer capturing the range may return blank.
	VulnerableRange       string
	MatchReason           string
	ImpactStatus          string
	Confidence            string
	CVSSScore             float64
	AdvisoryPublishedAt   string
	AdvisoryUpdatedAt     string
	EPSSProbability       string
	EPSSPercentile        string
	KnownExploited        bool
	PriorityReason        string
	PriorityScore         int
	PriorityBucket        string
	PriorityReasonCodes   []string
	PriorityContributions []SupplyChainImpactPriorityContribution
	RuntimeReachability   string
	Reachability          *SupplyChainReachabilityResult
	RepositoryID          string
	SubjectDigest         string
	ImageRef              string
	DependencyScope       string
	WorkloadIDs           []string
	DeploymentIDs         []string
	ServiceIDs            []string
	Environments          []string
	CatalogEntityRefs     []string
	CatalogOwnerRefs      []string
	DependencyPath        []string
	DependencyDepth       int
	DirectDependency      *bool
	MissingEvidence       []string
	EvidencePath          []string
	EvidenceFactIDs       []string
	SourceFreshness       string
	SourceConfidence      string
	Provenance            *SupplyChainImpactProvenance
	// Suppression carries the reducer VEX/operator-policy decision; it is
	// always populated (state=active when no suppression matched) so callers
	// can audit suppression provenance even when the finding is hidden from
	// the default view.
	Suppression *SupplyChainSuppressionDecisionRow
	// DetectionProfile records which evidence tier produced the row:
	// precise for exact installed-version anchors, comprehensive for
	// range-only, SBOM-derived, CPE-derived, malformed, or missing-version
	// evidence. Unsupported matcher ecosystems are readiness gaps, not
	// findings.
	// Older rows written before profile tagging may return blank.
	DetectionProfile string
	// Remediation is the reducer-owned advisory-only safe-upgrade
	// recommendation for this finding. Older rows written before #595
	// landed will leave this nil; callers must treat that as "no
	// remediation computed" rather than "no fix available".
	Remediation *SupplyChainImpactRemediation
	// DeploymentTruthTier classifies the strongest deployment evidence
	// available for this finding's repository or workload context (#5471).
	DeploymentTruthTier string
}

// SupplyChainSuppressionDecisionRow is the API-shaped suppression decision
// attached to one impact finding. The reducer produces an "active" row when
// no suppression matched.
type SupplyChainSuppressionDecisionRow struct {
	State          string `json:"state"`
	SuppressionID  string `json:"suppression_id,omitempty"`
	Source         string `json:"source,omitempty"`
	Justification  string `json:"justification,omitempty"`
	Author         string `json:"author,omitempty"`
	AuthoredAt     string `json:"authored_at,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	Reason         string `json:"reason,omitempty"`
	EvidenceRef    string `json:"evidence_ref,omitempty"`
	VEXDocumentID  string `json:"vex_document_id,omitempty"`
	VEXStatementID string `json:"vex_statement_id,omitempty"`
}

// SupplyChainImpactProvenance preserves per-source advisory provenance for one
// supply-chain impact finding. Reducers select one severity and one
// fixed-version using documented ecosystem-aware priority while keeping every
// other source observation as an alternate so callers can explain selection
// and detect vendor/upstream disagreement.
type SupplyChainImpactProvenance struct {
	SelectedSeveritySource     string                          `json:"selected_severity_source,omitempty"`
	SelectedSeverityScore      float64                         `json:"selected_severity_score,omitempty"`
	SelectedSeverityVector     string                          `json:"selected_severity_vector,omitempty"`
	SelectedSeverityLabel      string                          `json:"selected_severity_label,omitempty"`
	SelectedFixedVersionSource string                          `json:"selected_fixed_version_source,omitempty"`
	SelectedRangeSource        string                          `json:"selected_range_source,omitempty"`
	AlternateSeverities        []SupplyChainAlternateSeverity  `json:"alternate_severities,omitempty"`
	FixedVersionBranches       []SupplyChainFixedVersionBranch `json:"fixed_version_branches,omitempty"`
	AdvisorySources            []SupplyChainAdvisorySource     `json:"advisory_sources,omitempty"`
}

// SupplyChainAlternateSeverity is one non-selected source severity preserved
// for explainability.
type SupplyChainAlternateSeverity struct {
	Source string  `json:"source"`
	Score  float64 `json:"score,omitempty"`
	Vector string  `json:"vector,omitempty"`
	Label  string  `json:"label,omitempty"`
}

// SupplyChainFixedVersionBranch is one source-attributed fixed-version branch
// for one impact finding.
type SupplyChainFixedVersionBranch struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

// SupplyChainAdvisorySource is one source-attributed advisory observation
// behind a finding, including when the source last updated it and when it
// was withdrawn.
type SupplyChainAdvisorySource struct {
	Source          string `json:"source"`
	AdvisoryID      string `json:"advisory_id,omitempty"`
	SourceUpdatedAt string `json:"source_updated_at,omitempty"`
	WithdrawnAt     string `json:"withdrawn_at,omitempty"`
}

type supplyChainImpactFindingQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresSupplyChainImpactFindingStore reads active impact finding facts from
// Postgres using scoped payload predicates.
type PostgresSupplyChainImpactFindingStore struct {
	DB supplyChainImpactFindingQueryer
	// ReadFromWinners switches the list read to the maintained
	// supply_chain_impact_canonical_winners read model (#3389 Phase 2). When
	// false (the default) the read deduplicates at query time with the legacy
	// ROW_NUMBER query. The wiring sets this from an operator env gate so the
	// cutover is reversible and is only enabled once the reducer maintainer has
	// populated the winners table. Output is byte-identical either way; the
	// winners read is bounded O(page) instead of an O(filtered set) dedup sort.
	ReadFromWinners bool
}

// NewPostgresSupplyChainImpactFindingStore creates the Postgres-backed impact
// finding read model.
func NewPostgresSupplyChainImpactFindingStore(
	db supplyChainImpactFindingQueryer,
) PostgresSupplyChainImpactFindingStore {
	return PostgresSupplyChainImpactFindingStore{DB: db}
}

// NewPostgresSupplyChainImpactFindingStoreWithReadModel creates the store with
// the #3389 Phase 2 read gate set. readFromWinners=true serves the list from the
// maintained supply_chain_impact_canonical_winners read model; false keeps the
// legacy read-time dedup. The wiring resolves readFromWinners from an operator
// env gate.
func NewPostgresSupplyChainImpactFindingStoreWithReadModel(
	db supplyChainImpactFindingQueryer,
	readFromWinners bool,
) PostgresSupplyChainImpactFindingStore {
	return PostgresSupplyChainImpactFindingStore{DB: db, ReadFromWinners: readFromWinners}
}

// selectSupplyChainImpactWinnersWatermarkQuery reads the maintainer watermark
// from the singleton supply_chain_impact_winners_materialization row. The
// watermark is upserted by the same atomic resweep that reconciles the winners
// table, so it survives a resweep that produced zero active winners. Reading the
// watermark (not winner-row presence) lets the caller distinguish "never
// populated" (no row) from "reswept to zero findings" (row present, winners table
// empty) — the latter is a legitimate fresh empty result, not a building state.
const selectSupplyChainImpactWinnersWatermarkQuery = `
SELECT materialized_at
FROM supply_chain_impact_winners_materialization
LIMIT 1`

// SupplyChainImpactWinnersFreshness reports the freshness of the maintained
// winners read model for the impact-findings list. ServingFromWinners is false
// when the store reads live impact facts (legacy path), where the answer is
// always current and the caller MUST leave the truth envelope fresh. When
// ServingFromWinners is true, Present reports whether the maintainer watermark
// row exists (i.e. the maintainer has reswept at least once, independent of how
// many winners that resweep produced) and MaterializedAt carries the last
// resweep watermark (valid only when Present is true).
type SupplyChainImpactWinnersFreshness struct {
	ServingFromWinners bool
	Present            bool
	MaterializedAt     time.Time
}

// SupplyChainImpactWinnersWatermark probes the winners read-model watermark for
// freshness reporting. It is a no-op (ServingFromWinners=false, no query) on the
// legacy live read, so the freshness probe costs nothing until the read gate is
// enabled. On the winners path it issues the bounded LIMIT 1 watermark query;
// ServingFromWinners is reported true even when the probe errors so the handler
// reports the freshness unavailable rather than falsely fresh.
func (s PostgresSupplyChainImpactFindingStore) SupplyChainImpactWinnersWatermark(
	ctx context.Context,
) (SupplyChainImpactWinnersFreshness, error) {
	if !s.ReadFromWinners {
		return SupplyChainImpactWinnersFreshness{}, nil
	}
	if s.DB == nil {
		return SupplyChainImpactWinnersFreshness{ServingFromWinners: true}, fmt.Errorf("supply chain impact finding database is required")
	}
	rows, err := s.DB.QueryContext(ctx, selectSupplyChainImpactWinnersWatermarkQuery)
	if err != nil {
		return SupplyChainImpactWinnersFreshness{ServingFromWinners: true}, fmt.Errorf("read supply chain impact winners watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return SupplyChainImpactWinnersFreshness{ServingFromWinners: true}, fmt.Errorf("read supply chain impact winners watermark: %w", err)
		}
		// No rows: the maintainer has not populated the read model yet (building).
		return SupplyChainImpactWinnersFreshness{ServingFromWinners: true, Present: false}, nil
	}
	var materializedAt time.Time
	if err := rows.Scan(&materializedAt); err != nil {
		return SupplyChainImpactWinnersFreshness{ServingFromWinners: true}, fmt.Errorf("scan supply chain impact winners watermark: %w", err)
	}
	return SupplyChainImpactWinnersFreshness{ServingFromWinners: true, Present: true, MaterializedAt: materializedAt}, nil
}

// ListSupplyChainImpactFindings returns one bounded page of active reducer
// impact findings.
func (s PostgresSupplyChainImpactFindingStore) ListSupplyChainImpactFindings(
	ctx context.Context,
	filter SupplyChainImpactFindingFilter,
) ([]SupplyChainImpactFindingRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("supply chain impact finding database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("cve_id, advisory_id, package_id, repository_id, subject_digest, image_ref, impact_status, ecosystem, workload_id, service_id, environment, severity, priority_bucket, or min_priority_score > 0 is required")
	}
	if filter.Limit <= 0 || filter.Limit > supplyChainImpactFindingMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", supplyChainImpactFindingMaxLimit+1)
	}

	query := listSupplyChainImpactFindingsQuery
	if s.ReadFromWinners {
		query = listSupplyChainImpactFindingsFromWinnersQuery
	}
	rows, err := s.DB.QueryContext(
		ctx,
		query,
		supplyChainImpactFindingFactKind,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.ImageRef,
		filter.AfterFindingID,
		normalizeSupplyChainImpactSort(filter.Sort),
		filter.Limit,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		pq.Array(filter.AllowedRepositoryIDs),
		pq.Array(filter.AllowedScopeIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]SupplyChainImpactFindingRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list supply chain impact findings: %w", err)
		}
		row, err := decodeSupplyChainImpactFindingRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list supply chain impact findings: %w", err)
	}
	return out, nil
}

func (f SupplyChainImpactFindingFilter) hasScope() bool {
	return f.CVEID != "" || f.AdvisoryID != "" || f.PackageID != "" ||
		f.RepositoryID != "" || f.SubjectDigest != "" || f.ImageRef != "" || f.ImpactStatus != "" ||
		f.Ecosystem != "" || f.WorkloadID != "" || f.ServiceID != "" ||
		f.Environment != "" || f.Severity != "" || f.PriorityBucket != "" ||
		f.MinPriorityScore > 0
}
