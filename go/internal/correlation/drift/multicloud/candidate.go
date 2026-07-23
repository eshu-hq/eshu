// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloud

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

const (
	// EvidenceTypeCloudResourceUID marks the canonical key atom the
	// multi_cloud_runtime_drift rule pack requires.
	EvidenceTypeCloudResourceUID = rules.MultiCloudRuntimeDriftEvidenceType
	// EvidenceTypeProvider marks the normalized provider token atom.
	EvidenceTypeProvider = "cloud_provider"
	// EvidenceTypeRawIdentity preserves the provider raw identity as source
	// evidence; it is never used as a canonical key.
	EvidenceTypeRawIdentity = "cloud_raw_identity"
	// EvidenceTypeCloudResource marks the provider-observed resource side.
	EvidenceTypeCloudResource = "cloud_observed_resource"
	// EvidenceTypeStateResource marks the Terraform-state side.
	EvidenceTypeStateResource = "terraform_state_resource"
	// EvidenceTypeConfigResource marks the Terraform-config side.
	EvidenceTypeConfigResource = "terraform_config_resource"
	// EvidenceTypeFindingKind marks the classifier output atom.
	EvidenceTypeFindingKind = "cloud_runtime_finding_kind"
	// EvidenceTypeManagementStatus marks the query-facing management status.
	EvidenceTypeManagementStatus = "iac_management_status"
	// EvidenceTypeCoverageGap marks a collector coverage or permission gap.
	EvidenceTypeCoverageGap = "collector_coverage_gap"
	// EvidenceTypeAmbiguousManagement marks conflicting deterministic ownership.
	EvidenceTypeAmbiguousManagement = "ambiguous_management_conflict"
	// EvidenceTypeWarningFlag carries reducer warning flags for the read model.
	EvidenceTypeWarningFlag = "iac_management_warning"
)

const (
	evidenceKeyUID         = "cloud_resource_uid"
	evidenceKeyProvider    = "provider"
	evidenceKeyRawIdentity = "raw_identity"
	evidenceKeyARN         = "arn"
	evidenceKeyAddress     = "resource_address"
	evidenceKeyFindingKind = "finding_kind"
	evidenceKeyStatus      = "management_status"
	driftSourceSystem      = "reducer/multi_cloud_runtime_drift"
	driftConfidence        = 1.0
)

// BuildCandidates produces one candidate per multi-cloud runtime finding, keyed
// by canonical cloud_resource_uid. Rows whose observed, state, and config layers
// converge produce no candidate because there is no runtime drift to admit. Rows
// whose identity does not resolve to a canonical uid are skipped here and counted
// by the reducer as unresolved, never fabricated into findings.
func BuildCandidates(rows []Row, scopeID string) []model.Candidate {
	if len(rows) == 0 {
		return nil
	}

	type keyed struct {
		row Row
		uid string
	}
	resolved := make([]keyed, 0, len(rows))
	for _, row := range rows {
		uid, ok := row.ResolveUID()
		if !ok {
			continue
		}
		kind := row.EffectiveFindingKind()
		if kind == "" {
			continue
		}
		resolved = append(resolved, keyed{row: row, uid: uid})
	}

	slices.SortFunc(resolved, func(a, b keyed) int {
		return cmp.Compare(a.uid, b.uid)
	})

	candidates := make([]model.Candidate, 0, len(resolved))
	for _, item := range resolved {
		candidates = append(candidates, buildOneCandidate(item.row, item.uid, item.row.EffectiveFindingKind(), scopeID))
	}
	return candidates
}

func buildOneCandidate(row Row, uid string, kind cloudruntime.FindingKind, scopeID string) model.Candidate {
	candidateID := fmt.Sprintf("multi_cloud_runtime_drift:%s:%s", uid, kind)
	scope := rowScope(row, scopeID)
	evidence := []model.EvidenceAtom{
		atom(candidateID+"/uid", EvidenceTypeCloudResourceUID, scope, evidenceKeyUID, uid),
		atom(candidateID+"/finding_kind", EvidenceTypeFindingKind, scope, evidenceKeyFindingKind, string(kind)),
	}
	if provider := strings.TrimSpace(row.Provider); provider != "" {
		evidence = append(evidence, atom(candidateID+"/provider", EvidenceTypeProvider, scope, evidenceKeyProvider, provider))
	}
	if raw := strings.TrimSpace(row.RawIdentity); raw != "" {
		evidence = append(evidence, atom(candidateID+"/raw_identity", EvidenceTypeRawIdentity, scope, evidenceKeyRawIdentity, raw))
	}
	evidence = appendResourceEvidence(evidence, candidateID, "/cloud", row.Cloud, EvidenceTypeCloudResource, scope, preferIdentity)
	evidence = appendResourceEvidence(evidence, candidateID, "/state", row.State, EvidenceTypeStateResource, scope, preferAddress)
	evidence = appendResourceEvidence(evidence, candidateID, "/config", row.Config, EvidenceTypeConfigResource, scope, preferAddress)
	evidence = appendRawTagEvidence(evidence, candidateID, row.Cloud, scope)
	evidence = appendManagementEvidence(evidence, candidateID, row, kind, scope)
	evidence = appendValueDriftEvidence(evidence, candidateID, row.Cloud, row.State, scope)

	return model.Candidate{
		ID:             candidateID,
		Kind:           rules.MultiCloudRuntimeDriftPackName,
		CorrelationKey: uid,
		Confidence:     driftConfidence,
		State:          model.CandidateStateProvisional,
		Evidence:       evidence,
	}
}

func appendManagementEvidence(
	evidence []model.EvidenceAtom,
	candidateID string,
	row Row,
	kind cloudruntime.FindingKind,
	scope string,
) []model.EvidenceAtom {
	status := strings.TrimSpace(row.ManagementStatus)
	if status == "" {
		status = managementStatusForFinding(kind)
	}
	if status != "" {
		evidence = append(evidence, atom(candidateID+"/management_status", EvidenceTypeManagementStatus, scope, evidenceKeyStatus, status))
	}

	for _, missing := range sortedNonEmpty(row.MissingEvidence) {
		evidenceType := "missing_evidence"
		if status == cloudruntime.ManagementStatusUnknown {
			evidenceType = EvidenceTypeCoverageGap
		}
		evidence = append(evidence, atom(candidateID+"/missing/"+missing, evidenceType, scope, "missing_evidence", missing))
	}

	for _, warning := range sortedNonEmpty(row.WarningFlags) {
		evidenceType := EvidenceTypeWarningFlag
		if status == cloudruntime.ManagementStatusAmbiguous {
			evidenceType = EvidenceTypeAmbiguousManagement
		}
		evidence = append(evidence, atom(candidateID+"/warning/"+warning, evidenceType, scope, "warning_flag", warning))
	}
	return evidence
}

// identityPreference selects which field is the primary value/key for one
// resource-layer evidence atom. Observed cloud resources lead with their raw
// identity; Terraform state and config layers lead with their Terraform address
// because that is the meaningful declared/applied identity for the read model.
type identityPreference int

const (
	preferIdentity identityPreference = iota
	preferAddress
)

func appendResourceEvidence(
	evidence []model.EvidenceAtom,
	candidateID string,
	suffix string,
	row *cloudruntime.ResourceRow,
	evidenceType string,
	scope string,
	preference identityPreference,
) []model.EvidenceAtom {
	if row == nil {
		return evidence
	}
	arn := strings.TrimSpace(row.ARN)
	address := strings.TrimSpace(row.Address)
	key, value := evidenceKeyARN, arn
	if preference == preferAddress && address != "" {
		key, value = evidenceKeyAddress, address
	}
	if value == "" && address != "" {
		key, value = evidenceKeyAddress, address
	}
	if value == "" {
		value = arn
		key = evidenceKeyARN
	}
	if value == "" {
		return evidence
	}
	resourceScope := scope
	if rowScopeOverride := strings.TrimSpace(row.ScopeID); rowScopeOverride != "" {
		resourceScope = rowScopeOverride
	}
	return append(evidence, atom(candidateID+suffix, evidenceType, resourceScope, key, value))
}

// appendValueDriftEvidence mirrors cloudruntime's own appendValueDriftEvidence
// (go/internal/correlation/drift/cloudruntime/candidate.go), reusing the SAME
// cloudruntime.ClassifyValueDrift authority so the provider-neutral path can
// never disagree with the AWS-specific path about which attributes drifted.
// Safe to call for every finding kind: ClassifyValueDrift returns nil
// whenever cloud or state is nil.
func appendValueDriftEvidence(
	evidence []model.EvidenceAtom,
	candidateID string,
	cloud, state *cloudruntime.ResourceRow,
	scope string,
) []model.EvidenceAtom {
	for _, attr := range cloudruntime.ClassifyValueDrift(cloud, state) {
		evidence = append(
			evidence,
			atom(candidateID+"/declared/"+attr.Key, cloudruntime.EvidenceTypeDeclaredValue, scope, "declared_"+attr.Key, attr.Declared),
			atom(candidateID+"/observed/"+attr.Key, cloudruntime.EvidenceTypeObservedValue, scope, "observed_"+attr.Key, attr.Observed),
		)
	}
	return evidence
}

func appendRawTagEvidence(
	evidence []model.EvidenceAtom,
	candidateID string,
	row *cloudruntime.ResourceRow,
	scope string,
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
		evidence = append(evidence, atom(candidateID+"/tag/"+key, cloudruntime.EvidenceTypeRawTag, scope, "tag:"+key, row.Tags[key]))
	}
	return evidence
}

func atom(id, evidenceType, scope, key, value string) model.EvidenceAtom {
	return model.EvidenceAtom{
		ID:           id,
		SourceSystem: driftSourceSystem,
		EvidenceType: evidenceType,
		ScopeID:      scope,
		Key:          key,
		Value:        value,
		Confidence:   driftConfidence,
	}
}

func rowScope(row Row, fallback string) string {
	if scope := strings.TrimSpace(row.ScopeID); scope != "" {
		return scope
	}
	if row.Cloud != nil {
		if scope := strings.TrimSpace(row.Cloud.ScopeID); scope != "" {
			return scope
		}
	}
	return strings.TrimSpace(fallback)
}

func managementStatusForFinding(kind cloudruntime.FindingKind) string {
	switch kind {
	case cloudruntime.FindingKindOrphanedCloudResource:
		return cloudruntime.ManagementStatusCloudOnly
	case cloudruntime.FindingKindUnmanagedCloudResource:
		return cloudruntime.ManagementStatusTerraformStateOnly
	case cloudruntime.FindingKindUnknownCloudResource:
		return cloudruntime.ManagementStatusUnknown
	case cloudruntime.FindingKindAmbiguousCloudResource:
		return cloudruntime.ManagementStatusAmbiguous
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

// FindingKindFromCandidate returns the finding-kind atom value from one admitted
// candidate, or empty when the atom is missing.
func FindingKindFromCandidate(candidate model.Candidate) string {
	for _, a := range candidate.Evidence {
		if a.EvidenceType == EvidenceTypeFindingKind {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}

// ManagementStatusFromCandidate returns the management-status atom value, or the
// finding-derived default when the atom is missing.
func ManagementStatusFromCandidate(candidate model.Candidate) string {
	for _, a := range candidate.Evidence {
		if a.EvidenceType == EvidenceTypeManagementStatus && strings.TrimSpace(a.Value) != "" {
			return strings.TrimSpace(a.Value)
		}
	}
	return managementStatusForFinding(cloudruntime.FindingKind(FindingKindFromCandidate(candidate)))
}

// ProviderFromCandidate returns the provider atom value, or empty when missing.
func ProviderFromCandidate(candidate model.Candidate) string {
	for _, a := range candidate.Evidence {
		if a.EvidenceType == EvidenceTypeProvider {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}
