package query

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	findingKindUnmanagedCloudResource = "unmanaged_cloud_resource"
	findingKindOrphanedCloudResource  = "orphaned_cloud_resource"
	findingKindUnknownCloudResource   = "unknown_cloud_resource"
	findingKindAmbiguousCloudResource = "ambiguous_cloud_resource"
)

// IaCManagementStore reads reducer-materialized cloud management findings.
type IaCManagementStore interface {
	ListUnmanagedCloudResources(ctx context.Context, filter IaCManagementFilter) ([]IaCManagementFindingRow, error)
	CountUnmanagedCloudResources(ctx context.Context, filter IaCManagementFilter) (int, error)
}

// IaCManagementFilter bounds cloud management reads to one AWS scope or account.
type IaCManagementFilter struct {
	ScopeID      string
	AccountID    string
	Region       string
	ARN          string
	FindingKinds []string
	Limit        int
	Offset       int
}

// IaCManagementFindingRow is one query-facing unmanaged cloud resource finding.
type IaCManagementFindingRow struct {
	ID                           string                     `json:"id"`
	Provider                     string                     `json:"provider"`
	AccountID                    string                     `json:"account_id,omitempty"`
	Region                       string                     `json:"region,omitempty"`
	ResourceType                 string                     `json:"resource_type,omitempty"`
	ResourceID                   string                     `json:"resource_id,omitempty"`
	ARN                          string                     `json:"arn,omitempty"`
	Tags                         map[string]string          `json:"tags,omitempty"`
	FindingKind                  string                     `json:"finding_kind"`
	ManagementStatus             string                     `json:"management_status"`
	Confidence                   float64                    `json:"confidence"`
	ScopeID                      string                     `json:"scope_id"`
	GenerationID                 string                     `json:"generation_id"`
	SourceSystem                 string                     `json:"source_system"`
	CandidateID                  string                     `json:"candidate_id,omitempty"`
	MatchedTerraformStateAddress string                     `json:"matched_terraform_state_address,omitempty"`
	MatchedTerraformConfigFile   string                     `json:"matched_terraform_config_file,omitempty"`
	MatchedTerraformModulePath   string                     `json:"matched_terraform_module_path,omitempty"`
	MatchedOtherIaCSource        string                     `json:"matched_other_iac_source,omitempty"`
	ServiceCandidates            []string                   `json:"service_candidates,omitempty"`
	EnvironmentCandidates        []string                   `json:"environment_candidates,omitempty"`
	DependencyPaths              []string                   `json:"dependency_paths,omitempty"`
	RecommendedAction            string                     `json:"recommended_action"`
	MissingEvidence              []string                   `json:"missing_evidence,omitempty"`
	WarningFlags                 []string                   `json:"warning_flags,omitempty"`
	Evidence                     []IaCManagementEvidenceRow `json:"evidence"`
}

// IaCManagementEvidenceRow is one evidence atom explaining a cloud management
// finding.
type IaCManagementEvidenceRow struct {
	ID             string  `json:"id"`
	SourceSystem   string  `json:"source_system"`
	EvidenceType   string  `json:"evidence_type"`
	ScopeID        string  `json:"scope_id"`
	Key            string  `json:"key"`
	Value          string  `json:"value"`
	Confidence     float64 `json:"confidence"`
	ProvenanceOnly bool    `json:"provenance_only"`
}

type iacManagementRequest struct {
	ScopeID      string   `json:"scope_id"`
	AccountID    string   `json:"account_id"`
	Region       string   `json:"region"`
	ARN          string   `json:"arn"`
	ResourceID   string   `json:"resource_id"`
	FindingKinds []string `json:"finding_kinds"`
	Limit        int      `json:"limit"`
	Offset       int      `json:"offset"`
}

// PostgresIaCManagementStore adapts active AWS runtime drift facts to the
// query package's stable IaC management response contract.
type PostgresIaCManagementStore struct {
	store postgres.AWSCloudRuntimeDriftFindingStore
}

// NewPostgresIaCManagementStore creates a query adapter over AWS runtime drift
// reducer facts in Postgres.
func NewPostgresIaCManagementStore(db *sql.DB) *PostgresIaCManagementStore {
	storeDB := &postgres.InstrumentedDB{
		Inner:     postgres.SQLDB{DB: db},
		Tracer:    otel.Tracer(telemetry.DefaultSignalName),
		StoreName: "iac_management",
	}
	return &PostgresIaCManagementStore{store: postgres.NewAWSCloudRuntimeDriftFindingStore(storeDB)}
}

// ListUnmanagedCloudResources returns the active reducer findings matching the
// bounded IaC management filter.
func (s *PostgresIaCManagementStore) ListUnmanagedCloudResources(
	ctx context.Context,
	filter IaCManagementFilter,
) ([]IaCManagementFindingRow, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.store.ListActiveFindings(ctx, postgres.AWSCloudRuntimeDriftFindingFilter{
		ScopeID:      filter.ScopeID,
		AccountID:    filter.AccountID,
		Region:       filter.Region,
		ARN:          filter.ARN,
		FindingKinds: filter.FindingKinds,
		Limit:        filter.Limit,
		Offset:       filter.Offset,
	})
	if err != nil {
		return nil, err
	}
	findings := make([]IaCManagementFindingRow, 0, len(rows))
	for _, row := range rows {
		findings = append(findings, awsRuntimeDriftRowToIaCManagement(row))
	}
	return findings, nil
}

// CountUnmanagedCloudResources returns the total active reducer findings count
// for the same filters used by ListUnmanagedCloudResources.
func (s *PostgresIaCManagementStore) CountUnmanagedCloudResources(
	ctx context.Context,
	filter IaCManagementFilter,
) (int, error) {
	if s == nil {
		return 0, nil
	}
	return s.store.CountActiveFindings(ctx, postgres.AWSCloudRuntimeDriftFindingFilter{
		ScopeID:      filter.ScopeID,
		AccountID:    filter.AccountID,
		Region:       filter.Region,
		ARN:          filter.ARN,
		FindingKinds: filter.FindingKinds,
	})
}

func (h *IaCHandler) handleUnmanagedCloudResources(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryIaCUnmanagedResources,
		"POST /api/v0/iac/unmanaged-resources",
		iacManagementCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), iacManagementCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"unmanaged cloud resource analysis requires reducer-materialized AWS runtime drift findings",
			ErrorCodeUnsupportedCapability,
			iacManagementCapability,
			h.profile(),
			requiredProfile(iacManagementCapability),
		)
		return
	}

	var req iacManagementRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter, err := normalizeIaCManagementRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h == nil || h.Management == nil {
		WriteError(w, http.StatusServiceUnavailable, "IaC management store is required")
		return
	}

	totalFindings, err := h.Management.CountUnmanagedCloudResources(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	findings, err := h.Management.ListUnmanagedCloudResources(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"scope_id":              filter.ScopeID,
		"account_id":            filter.AccountID,
		"region":                filter.Region,
		"arn":                   filter.ARN,
		"story":                 iacManagementListStory(filter, findings, totalFindings),
		"finding_groups":        iacManagementFindingGroups(findings),
		"finding_kinds":         filter.FindingKinds,
		"findings":              findings,
		"findings_count":        len(findings),
		"total_findings_count":  totalFindings,
		"limit":                 filter.Limit,
		"offset":                filter.Offset,
		"truncated":             iacManagementTruncated(filter.Offset, len(findings), totalFindings),
		"next_offset":           iacManagementNextOffset(filter.Offset, len(findings), totalFindings),
		"truth_basis":           "materialized_reducer_rows",
		"analysis_status":       "materialized_aws_runtime_drift",
		"graph_projection_note": "fact-backed read model; graph nodes remain a later ADR-shaped projection",
		"limitations": []string{
			"bounded to active AWS runtime drift reducer facts for the requested scope or account",
			"raw tags remain provenance evidence and do not infer environment or ownership truth",
			"cloud mutation and Terraform import generation are intentionally out of scope",
		},
	}, BuildTruthEnvelope(
		h.profile(),
		iacManagementCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-materialized AWS runtime drift findings",
	))
}

func normalizeIaCManagementRequest(req iacManagementRequest) (IaCManagementFilter, error) {
	filter := IaCManagementFilter{
		ScopeID:   strings.TrimSpace(req.ScopeID),
		AccountID: strings.TrimSpace(req.AccountID),
		Region:    strings.TrimSpace(req.Region),
		ARN:       strings.TrimSpace(iacFirstNonEmpty(req.ARN, req.ResourceID)),
		Limit:     req.Limit,
		Offset:    req.Offset,
	}
	kinds, err := normalizeIaCManagementFindingKinds(req.FindingKinds)
	if err != nil {
		return IaCManagementFilter{}, err
	}
	filter.FindingKinds = kinds
	if filter.ScopeID == "" && filter.AccountID == "" {
		return IaCManagementFilter{}, fmt.Errorf("scope_id or account_id is required")
	}
	if filter.AccountID != "" && !validAWSAccountID(filter.AccountID) {
		return IaCManagementFilter{}, fmt.Errorf("account_id must be a 12-digit AWS account ID")
	}
	if filter.Region != "" && filter.AccountID == "" && filter.ScopeID == "" {
		return IaCManagementFilter{}, fmt.Errorf("region requires account_id or scope_id")
	}
	if filter.AccountID != "" && filter.Region != "" && !validAWSRegion(filter.Region) {
		return IaCManagementFilter{}, fmt.Errorf("region must contain only lowercase letters, digits, and hyphens")
	}
	if filter.Limit <= 0 {
		filter.Limit = iacManagementDefaultLimit
	}
	if filter.Limit > iacManagementMaxLimit {
		filter.Limit = iacManagementMaxLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if len(filter.FindingKinds) == 0 {
		filter.FindingKinds = []string{
			findingKindAmbiguousCloudResource,
			findingKindOrphanedCloudResource,
			findingKindUnmanagedCloudResource,
			findingKindUnknownCloudResource,
		}
	}
	return filter, nil
}

func validAWSAccountID(accountID string) bool {
	if len(accountID) != 12 {
		return false
	}
	for _, r := range accountID {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validAWSRegion(region string) bool {
	for _, r := range region {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return region != ""
}

func normalizeIaCManagementFindingKinds(raw []string) ([]string, error) {
	allowed := map[string]bool{
		findingKindAmbiguousCloudResource: true,
		findingKindOrphanedCloudResource:  true,
		findingKindUnmanagedCloudResource: true,
		findingKindUnknownCloudResource:   true,
	}
	seen := map[string]struct{}{}
	var kinds []string
	for _, kind := range raw {
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind == "" {
			continue
		}
		if !allowed[kind] {
			return nil, fmt.Errorf("unsupported finding_kind %q", kind)
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds, nil
}

func awsRuntimeDriftRowToIaCManagement(
	row postgres.AWSCloudRuntimeDriftFindingRow,
) IaCManagementFindingRow {
	parsed := parseAWSManagementARN(row.ARN)
	evidence := make([]IaCManagementEvidenceRow, 0, len(row.Evidence))
	statusInput := iacManagementStatusInput{
		FindingKind: strings.TrimSpace(row.FindingKind),
	}
	tags := map[string]string{}
	enrichment := iacManagementEvidenceEnrichment{}
	for _, atom := range row.Evidence {
		statusInput.recordEvidence(atom.EvidenceType)
		enrichment.recordEvidence(atom)
		isRawTag := strings.EqualFold(atom.EvidenceType, "aws_raw_tag")
		if isRawTag && strings.HasPrefix(atom.Key, "tag:") {
			tags[strings.TrimPrefix(atom.Key, "tag:")] = atom.Value
		}
		evidence = append(evidence, IaCManagementEvidenceRow{
			ID:             atom.ID,
			SourceSystem:   atom.SourceSystem,
			EvidenceType:   atom.EvidenceType,
			ScopeID:        atom.ScopeID,
			Key:            atom.Key,
			Value:          atom.Value,
			Confidence:     atom.Confidence,
			ProvenanceOnly: isRawTag,
		})
	}
	if len(tags) == 0 {
		tags = nil
	}
	status := normalizeIaCManagementStatus(row.ManagementStatus, deriveIaCManagementStatus(statusInput))
	missingEvidence := firstNonEmptySlice(row.MissingEvidence, missingEvidenceForManagementStatus(status))
	warningFlags := iacMergeStringSets(
		row.WarningFlags,
		warningFlagsForManagementFinding(status, parsed.resourceType, parsed.resourceID, tags),
	)
	return IaCManagementFindingRow{
		ID:                           row.FactID,
		Provider:                     "aws",
		AccountID:                    parsed.accountID,
		Region:                       parsed.region,
		ResourceType:                 parsed.resourceType,
		ResourceID:                   parsed.resourceID,
		ARN:                          row.ARN,
		Tags:                         tags,
		FindingKind:                  row.FindingKind,
		ManagementStatus:             status,
		Confidence:                   row.Confidence,
		ScopeID:                      row.ScopeID,
		GenerationID:                 row.GenerationID,
		SourceSystem:                 row.SourceSystem,
		CandidateID:                  row.CandidateID,
		MatchedTerraformStateAddress: iacFirstNonEmpty(row.MatchedTerraformStateAddress, enrichment.stateAddress),
		MatchedTerraformConfigFile:   iacFirstNonEmpty(row.MatchedTerraformConfigFile, enrichment.configFile),
		MatchedTerraformModulePath:   iacFirstNonEmpty(row.MatchedTerraformModulePath, enrichment.modulePath),
		MatchedOtherIaCSource:        iacFirstNonEmpty(row.MatchedOtherIaCSource, enrichment.otherIaCSource),
		ServiceCandidates:            firstNonEmptySlice(row.ServiceCandidates, enrichment.serviceCandidates),
		EnvironmentCandidates:        firstNonEmptySlice(row.EnvironmentCandidates, enrichment.environmentCandidates),
		DependencyPaths:              firstNonEmptySlice(row.DependencyPaths, enrichment.dependencyPaths),
		RecommendedAction:            iacFirstNonEmpty(row.RecommendedAction, recommendedActionForManagementStatus(status)),
		MissingEvidence:              missingEvidence,
		WarningFlags:                 warningFlags,
		Evidence:                     evidence,
	}
}

type awsManagementARN struct {
	accountID    string
	region       string
	resourceType string
	resourceID   string
}

func parseAWSManagementARN(arn string) awsManagementARN {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" {
		return awsManagementARN{}
	}
	return awsManagementARN{
		accountID:    parts[4],
		region:       parts[3],
		resourceType: parts[2],
		resourceID:   parts[5],
	}
}

func iacManagementTruncated(offset int, returned int, total int) bool {
	return offset+returned < total
}

func iacManagementNextOffset(offset int, returned int, total int) *int {
	if !iacManagementTruncated(offset, returned, total) {
		return nil
	}
	next := offset + returned
	return &next
}
