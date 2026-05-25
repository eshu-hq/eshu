package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

const supplyChainImpactFindingFactKind = "reducer_supply_chain_impact_finding"

// SupplyChainImpactFindingStore reads reducer-owned vulnerability impact
// findings.
type SupplyChainImpactFindingStore interface {
	ListSupplyChainImpactFindings(context.Context, SupplyChainImpactFindingFilter) ([]SupplyChainImpactFindingRow, error)
}

// SupplyChainImpactFindingFilter bounds impact reads to a concrete CVE,
// package, repository, image digest, status, or detection profile.
type SupplyChainImpactFindingFilter struct {
	CVEID            string
	PackageID        string
	RepositoryID     string
	SubjectDigest    string
	ImpactStatus     string
	DetectionProfile string
	AfterFindingID   string
	Limit            int
}

// SupplyChainImpactFindingRow is one durable impact finding decoded from
// reducer-owned facts.
type SupplyChainImpactFindingRow struct {
	FindingID           string
	CVEID               string
	AdvisoryID          string
	PackageID           string
	Ecosystem           string
	PackageName         string
	PURL                string
	ProductCriteria     string
	MatchCriteriaID     string
	ObservedVersion     string
	RequestedRange      string
	FixedVersion        string
	MatchReason         string
	ImpactStatus        string
	Confidence          string
	CVSSScore           float64
	EPSSProbability     string
	EPSSPercentile      string
	KnownExploited      bool
	PriorityReason      string
	RuntimeReachability string
	RepositoryID        string
	SubjectDigest       string
	ImageRef            string
	WorkloadIDs         []string
	ServiceIDs          []string
	Environments        []string
	DependencyPath      []string
	DependencyDepth     int
	DirectDependency    *bool
	MissingEvidence     []string
	EvidencePath        []string
	EvidenceFactIDs     []string
	SourceFreshness     string
	SourceConfidence    string
	Provenance          *SupplyChainImpactProvenance
	// DetectionProfile records which evidence tier produced the row:
	// precise for exact installed-version anchors, comprehensive for
	// range-only, SBOM-derived, CPE-derived, malformed,
	// unsupported-ecosystem, or missing-version evidence. Older rows
	// written before profile tagging may return blank.
	DetectionProfile string
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
		return nil, fmt.Errorf("cve_id, package_id, repository_id, subject_digest, or impact_status is required")
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
		filter.DetectionProfile,
		filter.AfterFindingID,
		filter.Limit,
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

const listSupplyChainImpactFindingsQuery = `
SELECT fact.fact_id, fact.source_confidence, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($2 = '' OR fact.payload->>'cve_id' = $2)
  AND ($3 = '' OR fact.payload->>'package_id' = $3)
  AND ($4 = '' OR fact.payload->>'repository_id' = $4)
  AND ($5 = '' OR fact.payload->>'subject_digest' = $5)
  AND ($6 = '' OR fact.payload->>'impact_status' = $6)
  AND (
        $7 = ''
        OR fact.payload->>'detection_profile' = $7
        OR (
              $7 = 'comprehensive'
              AND COALESCE(fact.payload->>'detection_profile', '') = ''
           )
        OR (
              $7 = 'precise'
              AND COALESCE(fact.payload->>'detection_profile', '') = ''
              AND fact.payload->>'impact_status' IN (
                    'affected_exact',
                    'not_affected_known_fixed'
                  )
              AND COALESCE(fact.payload->>'observed_version', '') <> ''
              AND fact.payload->>'match_reason' IN (
                    'npm_semver_affected_range',
                    'npm_semver_known_fixed',
                    'maven_range_match',
                    'maven_known_fixed'
                  )
           )
      )
  AND ($8 = '' OR fact.fact_id > $8)
ORDER BY fact.fact_id ASC
LIMIT $9
`

func (f SupplyChainImpactFindingFilter) hasScope() bool {
	return f.CVEID != "" || f.PackageID != "" || f.RepositoryID != "" ||
		f.SubjectDigest != "" || f.ImpactStatus != ""
}

func decodeSupplyChainImpactFindingRow(
	factID string,
	sourceConfidence string,
	payloadBytes []byte,
) (SupplyChainImpactFindingRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return SupplyChainImpactFindingRow{}, fmt.Errorf("decode supply chain impact finding: %w", err)
	}
	row := SupplyChainImpactFindingRow{
		FindingID:           factID,
		CVEID:               StringVal(payload, "cve_id"),
		AdvisoryID:          StringVal(payload, "advisory_id"),
		PackageID:           StringVal(payload, "package_id"),
		Ecosystem:           StringVal(payload, "ecosystem"),
		PackageName:         StringVal(payload, "package_name"),
		PURL:                StringVal(payload, "purl"),
		ProductCriteria:     StringVal(payload, "product_criteria"),
		MatchCriteriaID:     StringVal(payload, "match_criteria_id"),
		ObservedVersion:     StringVal(payload, "observed_version"),
		RequestedRange:      StringVal(payload, "requested_range"),
		FixedVersion:        StringVal(payload, "fixed_version"),
		MatchReason:         StringVal(payload, "match_reason"),
		ImpactStatus:        StringVal(payload, "impact_status"),
		Confidence:          StringVal(payload, "confidence"),
		CVSSScore:           floatVal(payload, "cvss_score"),
		EPSSProbability:     StringVal(payload, "epss_probability"),
		EPSSPercentile:      StringVal(payload, "epss_percentile"),
		KnownExploited:      BoolVal(payload, "known_exploited"),
		PriorityReason:      StringVal(payload, "priority_reason"),
		RuntimeReachability: StringVal(payload, "runtime_reachability"),
		RepositoryID:        StringVal(payload, "repository_id"),
		SubjectDigest:       StringVal(payload, "subject_digest"),
		ImageRef:            StringVal(payload, "image_ref"),
		WorkloadIDs:         StringSliceVal(payload, "workload_ids"),
		ServiceIDs:          StringSliceVal(payload, "service_ids"),
		Environments:        StringSliceVal(payload, "environments"),
		DependencyPath:      StringSliceVal(payload, "dependency_path"),
		DependencyDepth:     int(floatVal(payload, "dependency_depth")),
		DirectDependency:    boolPointerVal(payload, "direct_dependency"),
		MissingEvidence:     StringSliceVal(payload, "missing_evidence"),
		EvidencePath:        StringSliceVal(payload, "evidence_path"),
		EvidenceFactIDs:     StringSliceVal(payload, "evidence_fact_ids"),
		SourceFreshness:     "active",
		SourceConfidence:    sourceConfidence,
		Provenance:          decodeSupplyChainImpactProvenance(payload),
		DetectionProfile:    StringVal(payload, "detection_profile"),
	}
	if row.DetectionProfile == "" {
		row.DetectionProfile = inferLegacyDetectionProfile(row.ImpactStatus, row.ObservedVersion, row.MatchReason)
	}
	return row, nil
}

// inferLegacyDetectionProfile classifies pre-profile findings (written
// before the reducer tagged detection_profile) using the same rule the
// reducer applies live. Rolling-upgrade scenarios — query service updated
// before the reducer reruns — would otherwise return zero precise rows
// for findings whose payload qualifies. Range-only, derived,
// product-only, malformed, unsupported, and missing-version rows still
// land in comprehensive.
func inferLegacyDetectionProfile(impactStatus string, observedVersion string, matchReason string) string {
	switch impactStatus {
	case "affected_exact", "not_affected_known_fixed":
	default:
		return SupplyChainImpactProfileComprehensive
	}
	if strings.TrimSpace(observedVersion) == "" {
		return SupplyChainImpactProfileComprehensive
	}
	switch matchReason {
	case "npm_semver_affected_range",
		"npm_semver_known_fixed",
		"maven_range_match",
		"maven_known_fixed":
		return SupplyChainImpactProfilePrecise
	default:
		return SupplyChainImpactProfileComprehensive
	}
}

func decodeSupplyChainImpactProvenance(payload map[string]any) *SupplyChainImpactProvenance {
	raw, ok := payload["provenance"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	provenance := SupplyChainImpactProvenance{
		SelectedSeveritySource:     StringVal(raw, "selected_severity_source"),
		SelectedSeverityScore:      floatVal(raw, "selected_severity_score"),
		SelectedSeverityVector:     StringVal(raw, "selected_severity_vector"),
		SelectedSeverityLabel:      StringVal(raw, "selected_severity_label"),
		SelectedFixedVersionSource: StringVal(raw, "selected_fixed_version_source"),
		SelectedRangeSource:        StringVal(raw, "selected_range_source"),
	}
	provenance.AlternateSeverities = decodeAlternateSeverities(raw["alternate_severities"])
	provenance.FixedVersionBranches = decodeFixedVersionBranches(raw["fixed_version_branches"])
	provenance.AdvisorySources = decodeAdvisorySources(raw["advisory_sources"])
	if provenance.isEmpty() {
		return nil
	}
	return &provenance
}

func (p SupplyChainImpactProvenance) isEmpty() bool {
	return p.SelectedSeveritySource == "" &&
		p.SelectedSeverityScore == 0 &&
		p.SelectedSeverityVector == "" &&
		p.SelectedSeverityLabel == "" &&
		p.SelectedFixedVersionSource == "" &&
		p.SelectedRangeSource == "" &&
		len(p.AlternateSeverities) == 0 &&
		len(p.FixedVersionBranches) == 0 &&
		len(p.AdvisorySources) == 0
}

func decodeAlternateSeverities(raw any) []SupplyChainAlternateSeverity {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]SupplyChainAlternateSeverity, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, SupplyChainAlternateSeverity{
			Source: StringVal(row, "source"),
			Score:  floatVal(row, "score"),
			Vector: StringVal(row, "vector"),
			Label:  StringVal(row, "label"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeFixedVersionBranches(raw any) []SupplyChainFixedVersionBranch {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]SupplyChainFixedVersionBranch, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, SupplyChainFixedVersionBranch{
			Version: StringVal(row, "version"),
			Source:  StringVal(row, "source"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeAdvisorySources(raw any) []SupplyChainAdvisorySource {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]SupplyChainAdvisorySource, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, SupplyChainAdvisorySource{
			Source:          StringVal(row, "source"),
			AdvisoryID:      StringVal(row, "advisory_id"),
			SourceUpdatedAt: StringVal(row, "source_updated_at"),
			WithdrawnAt:     StringVal(row, "withdrawn_at"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func boolPointerVal(payload map[string]any, key string) *bool {
	value, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case bool:
		return &typed
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		parsed := strings.EqualFold(trimmed, "true")
		return &parsed
	default:
		return nil
	}
}
