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
)

const (
	evidenceKeyARN         = "arn"
	evidenceKeyAddress     = "resource_address"
	evidenceKeyFindingKind = "finding_kind"
	driftSourceSystem      = "reducer/aws_cloud_runtime_drift"
	driftConfidence        = 1.0
)

// AddressedRow couples one ARN with the optional AWS cloud, Terraform-state,
// and Terraform-config views the classifier needs.
type AddressedRow struct {
	ARN          string
	ResourceType string
	Cloud        *ResourceRow
	State        *ResourceRow
	Config       *ResourceRow
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
		kind := Classify(row.Cloud, row.State, row.Config)
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

	return model.Candidate{
		ID:             candidateID,
		Kind:           rules.AWSCloudRuntimeDriftPackName,
		CorrelationKey: arn,
		Confidence:     driftConfidence,
		State:          model.CandidateStateProvisional,
		Evidence:       evidence,
	}
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
