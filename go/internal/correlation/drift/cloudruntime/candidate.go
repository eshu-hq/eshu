package cloudruntime

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

const (
	// EvidenceTypeCloudResourceARN marks the ARN atom required by the AWS
	// cloud-runtime drift rule pack.
	EvidenceTypeCloudResourceARN = "aws_cloud_resource_arn"
	// EvidenceTypeCloudResource marks the AWS-observed resource side.
	EvidenceTypeCloudResource = "aws_cloud_resource"
	// EvidenceTypeStateResource marks the Terraform-state side.
	EvidenceTypeStateResource = "terraform_state_resource"
	// EvidenceTypeConfigResource marks the Terraform-config side.
	EvidenceTypeConfigResource = "terraform_config_resource"
	// EvidenceTypeFindingKind marks the classifier output atom.
	EvidenceTypeFindingKind = "aws_cloud_runtime_finding_kind"
	// EvidenceTypeRawTag marks raw AWS tag evidence for DSL normalization.
	EvidenceTypeRawTag = "aws_raw_tag"
	// EvidenceTypeManagementStatus marks the query-facing management status
	// derived from deterministic evidence matching.
	EvidenceTypeManagementStatus = "iac_management_status"
	// EvidenceTypeCoverageGap marks missing collector coverage or permissions
	// that prevent ownership proof.
	EvidenceTypeCoverageGap = "collector_coverage_gap"
	// EvidenceTypeAmbiguousManagement marks conflicting deterministic ownership
	// evidence for one stable cloud identity.
	EvidenceTypeAmbiguousManagement = "ambiguous_management_conflict"
	// EvidenceTypeWarningFlag carries reducer warning flags for the read model.
	EvidenceTypeWarningFlag = "iac_management_warning"
)

const (
	evidenceKeyARN         = "arn"
	evidenceKeyAddress     = "resource_address"
	evidenceKeyFindingKind = "finding_kind"
	evidenceKeyStatus      = "management_status"
	driftSourceSystem      = "reducer/aws_cloud_runtime_drift"
	driftConfidence        = 1.0
)

// AddressedRow couples one ARN with the optional AWS cloud, Terraform-state,
// and Terraform-config views the classifier needs.
type AddressedRow struct {
	ARN               string
	ResourceType      string
	Cloud             *ResourceRow
	State             *ResourceRow
	Config            *ResourceRow
	FindingKind       FindingKind
	ManagementStatus  string
	MissingEvidence   []string
	WarningFlags      []string
	RecommendedAction string
}

// BuildCandidates produces one candidate per AWS runtime finding, keyed by
// canonical ARN. Rows where cloud, state, and config converge produce no
// candidate because there is no runtime drift to admit.
func BuildCandidates(rows []AddressedRow, scopeID string) []model.Candidate {
	if len(rows) == 0 {
		return nil
	}

	ordered := slices.Clone(rows)
	slices.SortFunc(ordered, func(a, b AddressedRow) int {
		return cmp.Compare(rowARN(a), rowARN(b))
	})

	candidates := make([]model.Candidate, 0, len(ordered))
	for _, row := range ordered {
		arn := rowARN(row)
		if arn == "" {
			continue
		}
		kind := row.FindingKind
		if kind == "" {
			kind = Classify(row.Cloud, row.State, row.Config)
		}
		if kind == "" {
			continue
		}
		candidates = append(candidates, buildOneCandidate(row, arn, kind, scopeID))
	}
	return candidates
}

func buildOneCandidate(row AddressedRow, arn string, kind FindingKind, scopeID string) model.Candidate {
	candidateID := fmt.Sprintf("aws_cloud_runtime_drift:%s:%s", arn, kind)
	evidence := []model.EvidenceAtom{
		{
			ID:           candidateID + "/arn",
			SourceSystem: driftSourceSystem,
			EvidenceType: EvidenceTypeCloudResourceARN,
			ScopeID:      scopeFor(row.Cloud, scopeID),
			Key:          evidenceKeyARN,
			Value:        arn,
			Confidence:   driftConfidence,
		},
		{
			ID:           candidateID + "/finding_kind",
			SourceSystem: driftSourceSystem,
			EvidenceType: EvidenceTypeFindingKind,
			ScopeID:      scopeFor(row.Cloud, scopeID),
			Key:          evidenceKeyFindingKind,
			Value:        string(kind),
			Confidence:   driftConfidence,
		},
	}
	evidence = appendResourceEvidence(evidence, candidateID, "/cloud", row.Cloud, EvidenceTypeCloudResource, scopeID)
	evidence = appendResourceEvidence(evidence, candidateID, "/state", row.State, EvidenceTypeStateResource, scopeID)
	evidence = appendResourceEvidence(evidence, candidateID, "/config", row.Config, EvidenceTypeConfigResource, scopeID)
	evidence = appendRawTagEvidence(evidence, candidateID, row.Cloud, scopeID)
	evidence = appendManagementEvidence(evidence, candidateID, row, kind, scopeID)

	return model.Candidate{
		ID:             candidateID,
		Kind:           rules.AWSCloudRuntimeDriftPackName,
		CorrelationKey: arn,
		Confidence:     driftConfidence,
		State:          model.CandidateStateProvisional,
		Evidence:       evidence,
	}
}

func appendManagementEvidence(
	evidence []model.EvidenceAtom,
	candidateID string,
	row AddressedRow,
	kind FindingKind,
	fallbackScopeID string,
) []model.EvidenceAtom {
	status := strings.TrimSpace(row.ManagementStatus)
	if status == "" {
		status = managementStatusForFinding(kind)
	}
	if status != "" {
		evidence = append(evidence, model.EvidenceAtom{
			ID:           candidateID + "/management_status",
			SourceSystem: driftSourceSystem,
			EvidenceType: EvidenceTypeManagementStatus,
			ScopeID:      scopeFor(row.Cloud, fallbackScopeID),
			Key:          evidenceKeyStatus,
			Value:        status,
			Confidence:   driftConfidence,
		})
	}

	for _, missing := range sortedNonEmpty(row.MissingEvidence) {
		evidenceType := "missing_evidence"
		if status == ManagementStatusUnknown {
			evidenceType = EvidenceTypeCoverageGap
		}
		evidence = append(evidence, model.EvidenceAtom{
			ID:           candidateID + "/missing/" + missing,
			SourceSystem: driftSourceSystem,
			EvidenceType: evidenceType,
			ScopeID:      scopeFor(row.Cloud, fallbackScopeID),
			Key:          "missing_evidence",
			Value:        missing,
			Confidence:   driftConfidence,
		})
	}

	for _, warning := range sortedNonEmpty(row.WarningFlags) {
		evidenceType := EvidenceTypeWarningFlag
		if status == ManagementStatusAmbiguous {
			evidenceType = EvidenceTypeAmbiguousManagement
		}
		evidence = append(evidence, model.EvidenceAtom{
			ID:           candidateID + "/warning/" + warning,
			SourceSystem: driftSourceSystem,
			EvidenceType: evidenceType,
			ScopeID:      scopeFor(row.Cloud, fallbackScopeID),
			Key:          "warning_flag",
			Value:        warning,
			Confidence:   driftConfidence,
		})
	}
	return evidence
}

func appendResourceEvidence(
	evidence []model.EvidenceAtom,
	candidateID string,
	suffix string,
	row *ResourceRow,
	evidenceType string,
	fallbackScopeID string,
) []model.EvidenceAtom {
	if row == nil {
		return evidence
	}
	key, value := evidenceKeyARN, strings.TrimSpace(row.ARN)
	if value == "" && strings.TrimSpace(row.Address) != "" {
		key, value = evidenceKeyAddress, strings.TrimSpace(row.Address)
	}
	if value == "" {
		return evidence
	}
	return append(evidence, model.EvidenceAtom{
		ID:           candidateID + suffix,
		SourceSystem: driftSourceSystem,
		EvidenceType: evidenceType,
		ScopeID:      scopeFor(row, fallbackScopeID),
		Key:          key,
		Value:        value,
		Confidence:   driftConfidence,
	})
}

func appendRawTagEvidence(
	evidence []model.EvidenceAtom,
	candidateID string,
	row *ResourceRow,
	fallbackScopeID string,
) []model.EvidenceAtom {
	if row == nil || len(row.Tags) == 0 {
		return evidence
	}
	keys := make([]string, 0, len(row.Tags))
	for key := range row.Tags {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		evidence = append(evidence, model.EvidenceAtom{
			ID:           candidateID + "/tag/" + key,
			SourceSystem: driftSourceSystem,
			EvidenceType: EvidenceTypeRawTag,
			ScopeID:      scopeFor(row, fallbackScopeID),
			Key:          "tag:" + key,
			Value:        row.Tags[key],
			Confidence:   driftConfidence,
		})
	}
	return evidence
}

func rowARN(row AddressedRow) string {
	if arn := strings.TrimSpace(row.ARN); arn != "" {
		return arn
	}
	for _, resource := range []*ResourceRow{row.Cloud, row.State, row.Config} {
		if resource == nil {
			continue
		}
		if arn := strings.TrimSpace(resource.ARN); arn != "" {
			return arn
		}
	}
	return ""
}

func scopeFor(row *ResourceRow, fallback string) string {
	if row != nil {
		if scopeID := strings.TrimSpace(row.ScopeID); scopeID != "" {
			return scopeID
		}
	}
	return strings.TrimSpace(fallback)
}

func managementStatusForFinding(kind FindingKind) string {
	switch kind {
	case FindingKindOrphanedCloudResource:
		return ManagementStatusCloudOnly
	case FindingKindUnmanagedCloudResource:
		return ManagementStatusTerraformStateOnly
	case FindingKindUnknownCloudResource:
		return ManagementStatusUnknown
	case FindingKindAmbiguousCloudResource:
		return ManagementStatusAmbiguous
	default:
		return ""
	}
}

func sortedNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
