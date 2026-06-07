package query

import (
	"context"
	"database/sql"
	"fmt"
)

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
}

// NewPostgresSupplyChainImpactFindingStore creates the Postgres-backed impact
// finding read model.
func NewPostgresSupplyChainImpactFindingStore(
	db supplyChainImpactFindingQueryer,
) PostgresSupplyChainImpactFindingStore {
	return PostgresSupplyChainImpactFindingStore{DB: db}
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

	rows, err := s.DB.QueryContext(
		ctx,
		listSupplyChainImpactFindingsQuery,
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
