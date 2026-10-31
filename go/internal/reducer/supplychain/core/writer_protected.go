// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// ScanImpactPublicationWitness pins a complete target result to the protected
// scanner completion receipt. The database rechecks this witness and current
// source/advisory authority before accepting the publication.
type ScanImpactPublicationWitness struct {
	ScanTargetID             string
	ScanGenerationID         string
	BindingID                string
	WorkItemID               string
	AnalysisVersion          string
	ClaimID                  string
	ClaimFencingToken        int64
	ClaimOwnerID             string
	TenantID                 string
	WorkspaceID              string
	IntrinsicEvidenceFactIDs []string
	AdvisoryEvidence         []ProtectedScanImpactAdvisoryEvidence
	AuthorityDependencies    []AdvisoryAuthorityDependency
	Complete                 bool
}

// ProtectedScanImpactAdvisoryEvidence binds one advisory fact to the exact
// protected registration or private binding that authorized its use.
type ProtectedScanImpactAdvisoryEvidence struct {
	EvidenceFactID string
	AuthorityKind  string
	AuthorityID    string
	AuthorityEpoch int64
}

// ProtectedScanImpactEvidenceAssociation links a derived finding to one
// contributing fact without placing the target association in public payload.
type ProtectedScanImpactEvidenceAssociation struct {
	FindingFactID  string
	EvidenceFactID string
	EvidenceClass  string
	AuthorityKind  string
	AuthorityID    string
	AuthorityEpoch int64
}

// ProtectedScanImpactFindingPayload is the canonical public finding content
// pinned by one immutable protected publication revision. The database hashes
// the full sorted set before fact upsert, then rejects a conflicting retry at
// the same impact fence even when the logical finding fact IDs are unchanged.
type ProtectedScanImpactFindingPayload struct {
	FactID      string
	PayloadJSON string
}

// ProtectedScanImpactTargetPublication is the complete derived result for one
// boundary-local scan target and its original scanner attempt.
type ProtectedScanImpactTargetPublication struct {
	Witness              ScanImpactPublicationWitness
	FindingFactIDs       []string
	FindingPayloads      []ProtectedScanImpactFindingPayload
	EvidenceAssociations []ProtectedScanImpactEvidenceAssociation
}

// ProtectedScanImpactPublicationSet accompanies one complete union of static
// and derived findings in the same fenced reducer write transaction.
type ProtectedScanImpactPublicationSet struct {
	CompleteSet          bool
	ImpactScopeID        string
	ImpactGenerationID   string
	ImpactFencingToken   int64
	StaticFindingFactIDs []string
	StaticFindings       []ProtectedStaticImpactFinding
	TargetPublications   []ProtectedScanImpactTargetPublication
}

// ProtectedScanImpactPublisher atomically persists and rechecks protected
// target publications inside the transaction that writes their finding facts.
type ProtectedScanImpactPublisher interface {
	PublishProtectedScanImpactResults(context.Context, ProtectedScanImpactPublicationSet) error
}

// ProtectedScanImpactAuthorizer locks and validates the complete authorized
// source and advisory set before the impact conflict lock or any fact write.
// The transaction must retain these locks until publication commits or rolls
// back, and reject a later publish call with a different set.
type ProtectedScanImpactAuthorizer interface {
	AuthorizeProtectedScanImpactResults(context.Context, ProtectedScanImpactPublicationSet) error
}

func buildProtectedScanImpactPublicationSet(
	write SupplyChainImpactWrite,
	rows []factwrite.VersionedRow,
) (ProtectedScanImpactPublicationSet, bool, error) {
	if len(write.Findings) != len(rows) {
		return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("impact finding and fact row counts differ")
	}
	set := ProtectedScanImpactPublicationSet{
		CompleteSet:        write.ProtectedCompleteSet,
		ImpactScopeID:      write.ScopeID,
		ImpactGenerationID: write.GenerationID,
		ImpactFencingToken: write.FencingToken,
	}
	byTarget := make(map[string]int, len(write.ScanPublications))
	evidenceByTarget := make([]map[string]ProtectedScanImpactEvidenceAssociation, 0, len(write.ScanPublications))
	for _, witness := range write.ScanPublications {
		if err := validateScanImpactPublicationWitness(witness); err != nil {
			return ProtectedScanImpactPublicationSet{}, false, err
		}
		if _, exists := byTarget[witness.ScanTargetID]; exists {
			return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("duplicate protected scan publication for %q", witness.ScanTargetID)
		}
		byTarget[witness.ScanTargetID] = len(set.TargetPublications)
		set.TargetPublications = append(set.TargetPublications, ProtectedScanImpactTargetPublication{Witness: witness})
		evidenceByTarget = append(evidenceByTarget, protectedScanImpactEvidenceClasses(witness))
	}
	if len(set.TargetPublications) > 0 && !write.ProtectedCompleteSet {
		return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("derived scan publications require a complete protected input set")
	}
	if write.ProtectedCompleteSet && write.PartialEvidence {
		return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("partial evidence cannot publish a complete derived scan result")
	}
	staticAdvisory, staticIndependent, err := protectedStaticImpactEvidenceClasses(write)
	if err != nil && write.ProtectedCompleteSet {
		return ProtectedScanImpactPublicationSet{}, false, err
	}
	seenDerivedFacts := make(map[string]struct{})
	for i, finding := range write.Findings {
		factID := rows[i].FactID
		if factID == "" {
			return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("impact finding has no fact ID")
		}
		if finding.ScanTargetID == "" {
			if finding.ScanGenerationID != "" || len(finding.AuthorityDependencies) > 0 {
				return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("protected derived metadata on static finding %q", factID)
			}
			set.StaticFindingFactIDs = append(set.StaticFindingFactIDs, factID)
			if write.ProtectedCompleteSet {
				staticFinding, err := protectedStaticImpactFinding(factID, rows[i].Payload,
					finding.EvidenceFactIDs, staticAdvisory, staticIndependent)
				if err != nil {
					return ProtectedScanImpactPublicationSet{}, false, err
				}
				set.StaticFindings = append(set.StaticFindings, staticFinding)
			}
			continue
		}
		index, exists := byTarget[finding.ScanTargetID]
		if !exists {
			return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("derived finding %q lacks protected publication witness", factID)
		}
		publication := &set.TargetPublications[index]
		if finding.ScanGenerationID != publication.Witness.ScanGenerationID || len(finding.AuthorityDependencies) == 0 ||
			len(finding.EvidenceFactIDs) == 0 {
			return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("derived finding %q has incomplete protected evidence", factID)
		}
		for _, dependency := range finding.AuthorityDependencies {
			if !slices.Contains(publication.Witness.AuthorityDependencies, dependency) {
				return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("derived finding %q uses an unbound advisory authority", factID)
			}
		}
		if _, duplicate := seenDerivedFacts[factID]; duplicate {
			return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("duplicate derived finding fact %q", factID)
		}
		seenDerivedFacts[factID] = struct{}{}
		publication.FindingFactIDs = append(publication.FindingFactIDs, factID)
		publication.FindingPayloads = append(publication.FindingPayloads, ProtectedScanImpactFindingPayload{
			FactID: factID, PayloadJSON: rows[i].Payload,
		})
		for _, evidenceFactID := range finding.EvidenceFactIDs {
			association, classified := evidenceByTarget[index][evidenceFactID]
			if !classified {
				return ProtectedScanImpactPublicationSet{}, false, fmt.Errorf("derived finding %q has unclassified evidence fact %q", factID, evidenceFactID)
			}
			association.FindingFactID = factID
			publication.EvidenceAssociations = append(publication.EvidenceAssociations, association)
		}
	}
	if !write.ProtectedCompleteSet {
		return set, false, nil
	}
	slices.Sort(set.StaticFindingFactIDs)
	sort.Slice(set.StaticFindings, func(i, j int) bool {
		return set.StaticFindings[i].FactID < set.StaticFindings[j].FactID
	})
	for i := range set.TargetPublications {
		publication := &set.TargetPublications[i]
		slices.Sort(publication.FindingFactIDs)
		sort.Slice(publication.FindingPayloads, func(i, j int) bool {
			return publication.FindingPayloads[i].FactID < publication.FindingPayloads[j].FactID
		})
		sort.Slice(publication.EvidenceAssociations, func(i, j int) bool {
			a, b := publication.EvidenceAssociations[i], publication.EvidenceAssociations[j]
			if a.FindingFactID != b.FindingFactID {
				return a.FindingFactID < b.FindingFactID
			}
			return a.EvidenceFactID < b.EvidenceFactID
		})
		publication.EvidenceAssociations = slices.Compact(publication.EvidenceAssociations)
	}
	sort.Slice(set.TargetPublications, func(i, j int) bool {
		return set.TargetPublications[i].Witness.ScanTargetID < set.TargetPublications[j].Witness.ScanTargetID
	})
	return set, true, nil
}

func validateScanImpactPublicationWitness(witness ScanImpactPublicationWitness) error {
	const prefix = "image://derived/"
	if !strings.HasPrefix(witness.ScanTargetID, prefix) || len(witness.ScanTargetID) != len(prefix)+64 {
		return fmt.Errorf("invalid protected scan target %q", witness.ScanTargetID)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(witness.ScanTargetID, prefix)); err != nil {
		return fmt.Errorf("invalid protected scan target digest: %w", err)
	}
	if !witness.Complete || witness.ScanGenerationID == "" || witness.BindingID == "" || witness.WorkItemID == "" || witness.AnalysisVersion == "" ||
		len(witness.AuthorityDependencies) == 0 {
		return fmt.Errorf("incomplete protected scan publication for %q", witness.ScanTargetID)
	}
	for _, dependency := range witness.AuthorityDependencies {
		if !validAdvisoryDependency(dependency) || dependency.AuthorityEpoch <= 0 || dependency.AdvisoryScopeEpoch <= 0 || dependency.ContentProfile == "" ||
			(dependency.Kind == "private_binding" && dependency.ScanTargetID != witness.ScanTargetID) {
			return fmt.Errorf("invalid protected advisory dependency for %q", witness.ScanTargetID)
		}
	}
	seen := make(map[string]struct{}, len(witness.IntrinsicEvidenceFactIDs)+len(witness.AdvisoryEvidence))
	for _, factID := range witness.IntrinsicEvidenceFactIDs {
		if factID == "" {
			return fmt.Errorf("empty intrinsic scan evidence for %q", witness.ScanTargetID)
		}
		if _, duplicate := seen[factID]; duplicate {
			return fmt.Errorf("duplicate scan evidence classification %q", factID)
		}
		seen[factID] = struct{}{}
	}
	for _, evidence := range witness.AdvisoryEvidence {
		if evidence.EvidenceFactID == "" || evidence.AuthorityKind == "" || evidence.AuthorityID == "" || evidence.AuthorityEpoch <= 0 {
			return fmt.Errorf("incomplete advisory evidence classification for %q", witness.ScanTargetID)
		}
		if _, duplicate := seen[evidence.EvidenceFactID]; duplicate {
			return fmt.Errorf("duplicate scan evidence classification %q", evidence.EvidenceFactID)
		}
		seen[evidence.EvidenceFactID] = struct{}{}
		if !slices.ContainsFunc(witness.AuthorityDependencies, func(dependency AdvisoryAuthorityDependency) bool {
			return dependency.Kind == evidence.AuthorityKind && dependency.ID == evidence.AuthorityID &&
				dependency.AuthorityEpoch == evidence.AuthorityEpoch
		}) {
			return fmt.Errorf("advisory evidence %q has no publication authority dependency", evidence.EvidenceFactID)
		}
	}
	return nil
}

// impactPublicationSetDocument is the canonical set encoding the protected
// publication entrypoints consume. Slices are never null (the SQL splits
// them unconditionally), dependency order is pinned for hash stability, and
// no protected identifier beyond the publication witnesses leaves this
// document: finding payloads are the public canonical bytes.
type impactPublicationSetDocument struct {
	StaticFindings     []impactPublicationStaticFindingJSON `json:"static_findings"`
	TargetPublications []impactPublicationTargetJSON        `json:"target_publications"`
}

type impactPublicationStaticFindingJSON struct {
	FindingFactID    string                                `json:"finding_fact_id"`
	PayloadJSON      string                                `json:"payload_json"`
	AdvisoryEvidence []impactPublicationStaticEvidenceJSON `json:"advisory_evidence"`
}

type impactPublicationStaticEvidenceJSON struct {
	EvidenceFactID      string `json:"evidence_fact_id"`
	RegistrationID      string `json:"registration_id"`
	GenerationReceiptID string `json:"generation_receipt_id"`
	RegistrationEpoch   int64  `json:"registration_epoch"`
	ActivationEpoch     int64  `json:"activation_epoch"`
	EvidencePayloadSHA  string `json:"evidence_payload_sha256"`
}

type impactPublicationTargetJSON struct {
	ScanTargetID             string                            `json:"scan_target_id"`
	ScanGenerationID         string                            `json:"scan_generation_id"`
	BindingID                string                            `json:"binding_id"`
	WorkItemID               string                            `json:"work_item_id"`
	ClaimID                  string                            `json:"claim_id"`
	ClaimFencingToken        int64                             `json:"claim_fencing_token"`
	ClaimOwnerID             string                            `json:"claim_owner_id"`
	AnalysisVersion          string                            `json:"analysis_version"`
	TenantID                 string                            `json:"tenant_id"`
	WorkspaceID              string                            `json:"workspace_id"`
	FindingPayloads          []impactPublicationFindingJSON    `json:"finding_payloads"`
	EvidenceAssociations     []impactPublicationEvidenceJSON   `json:"evidence_associations"`
	IntrinsicEvidenceFactIDs []string                          `json:"intrinsic_evidence_fact_ids"`
	AuthorityDependencies    []impactPublicationDependencyJSON `json:"authority_dependencies"`
}

type impactPublicationFindingJSON struct {
	FactID      string `json:"fact_id"`
	PayloadJSON string `json:"payload_json"`
}

type impactPublicationEvidenceJSON struct {
	FindingFactID  string `json:"finding_fact_id"`
	EvidenceFactID string `json:"evidence_fact_id"`
	EvidenceClass  string `json:"evidence_class"`
	AuthorityKind  string `json:"authority_kind"`
	AuthorityID    string `json:"authority_id"`
	AuthorityEpoch int64  `json:"authority_epoch"`
}

type impactPublicationDependencyJSON struct {
	Kind               string `json:"kind"`
	ScanTargetID       string `json:"scan_target_id"`
	ID                 string `json:"id"`
	RevisionID         string `json:"revision_id"`
	AuthorityEpoch     int64  `json:"authority_epoch"`
	AdvisoryScopeEpoch int64  `json:"advisory_scope_epoch"`
	AdvisoryScopeID    string `json:"advisory_scope_id"`
	AdvisoryGeneration string `json:"advisory_generation_id"`
	ConfigFingerprint  string `json:"config_fingerprint"`
	ProviderID         string `json:"provider_id"`
	TrustedSourceClass string `json:"trusted_source_class"`
	ContentProfile     string `json:"content_profile"`
}

// MarshalImpactPublicationSetDocument renders the canonical publication
// document for one validated set. The set is already sorted by its builder;
// dependency order is pinned here so the database set hashes stay stable
// across identical retries.
func MarshalImpactPublicationSetDocument(set ProtectedScanImpactPublicationSet) ([]byte, error) {
	document := impactPublicationSetDocument{
		StaticFindings:     make([]impactPublicationStaticFindingJSON, 0, len(set.StaticFindings)),
		TargetPublications: make([]impactPublicationTargetJSON, 0, len(set.TargetPublications)),
	}
	for _, finding := range set.StaticFindings {
		entry := impactPublicationStaticFindingJSON{
			FindingFactID:    finding.FactID,
			PayloadJSON:      finding.PayloadJSON,
			AdvisoryEvidence: make([]impactPublicationStaticEvidenceJSON, 0, len(finding.AdvisoryEvidence)),
		}
		for _, evidence := range finding.AdvisoryEvidence {
			entry.AdvisoryEvidence = append(entry.AdvisoryEvidence,
				impactPublicationStaticEvidenceJSON(evidence))
		}
		document.StaticFindings = append(document.StaticFindings, entry)
	}
	for _, publication := range set.TargetPublications {
		witness := publication.Witness
		entry := impactPublicationTargetJSON{
			ScanTargetID:             witness.ScanTargetID,
			ScanGenerationID:         witness.ScanGenerationID,
			BindingID:                witness.BindingID,
			WorkItemID:               witness.WorkItemID,
			ClaimID:                  witness.ClaimID,
			ClaimFencingToken:        witness.ClaimFencingToken,
			ClaimOwnerID:             witness.ClaimOwnerID,
			AnalysisVersion:          witness.AnalysisVersion,
			TenantID:                 witness.TenantID,
			WorkspaceID:              witness.WorkspaceID,
			FindingPayloads:          make([]impactPublicationFindingJSON, 0, len(publication.FindingPayloads)),
			EvidenceAssociations:     make([]impactPublicationEvidenceJSON, 0, len(publication.EvidenceAssociations)),
			IntrinsicEvidenceFactIDs: append([]string{}, witness.IntrinsicEvidenceFactIDs...),
			AuthorityDependencies:    make([]impactPublicationDependencyJSON, 0, len(witness.AuthorityDependencies)),
		}
		for _, payload := range publication.FindingPayloads {
			entry.FindingPayloads = append(entry.FindingPayloads,
				impactPublicationFindingJSON(payload))
		}
		for _, association := range publication.EvidenceAssociations {
			entry.EvidenceAssociations = append(entry.EvidenceAssociations,
				impactPublicationEvidenceJSON(association))
		}
		for _, dependency := range witness.AuthorityDependencies {
			entry.AuthorityDependencies = append(entry.AuthorityDependencies, impactPublicationDependencyJSON{
				Kind:               dependency.Kind,
				ScanTargetID:       dependency.ScanTargetID,
				ID:                 dependency.ID,
				RevisionID:         dependency.RevisionID,
				AuthorityEpoch:     dependency.AuthorityEpoch,
				AdvisoryScopeEpoch: dependency.AdvisoryScopeEpoch,
				AdvisoryScopeID:    dependency.AdvisoryScopeID,
				AdvisoryGeneration: dependency.AdvisoryGenerationID,
				ConfigFingerprint:  dependency.ConfigFingerprint,
				ProviderID:         dependency.ProviderID,
				TrustedSourceClass: dependency.TrustedSourceClass,
				ContentProfile:     dependency.ContentProfile,
			})
		}
		slices.Sort(entry.IntrinsicEvidenceFactIDs)
		sort.Slice(entry.EvidenceAssociations, func(i, j int) bool {
			a, b := entry.EvidenceAssociations[i], entry.EvidenceAssociations[j]
			if a.FindingFactID != b.FindingFactID {
				return a.FindingFactID < b.FindingFactID
			}
			return a.EvidenceFactID < b.EvidenceFactID
		})
		sort.Slice(entry.AuthorityDependencies, func(i, j int) bool {
			a, b := entry.AuthorityDependencies[i], entry.AuthorityDependencies[j]
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			if a.ID != b.ID {
				return a.ID < b.ID
			}
			return a.AuthorityEpoch < b.AuthorityEpoch
		})
		document.TargetPublications = append(document.TargetPublications, entry)
	}
	return json.Marshal(document)
}

func protectedScanImpactEvidenceClasses(witness ScanImpactPublicationWitness) map[string]ProtectedScanImpactEvidenceAssociation {
	classes := make(map[string]ProtectedScanImpactEvidenceAssociation, len(witness.IntrinsicEvidenceFactIDs)+len(witness.AdvisoryEvidence))
	for _, factID := range witness.IntrinsicEvidenceFactIDs {
		classes[factID] = ProtectedScanImpactEvidenceAssociation{EvidenceFactID: factID, EvidenceClass: "scan_intrinsic"}
	}
	for _, advisory := range witness.AdvisoryEvidence {
		classes[advisory.EvidenceFactID] = ProtectedScanImpactEvidenceAssociation{
			EvidenceFactID: advisory.EvidenceFactID, EvidenceClass: "advisory",
			AuthorityKind: advisory.AuthorityKind, AuthorityID: advisory.AuthorityID, AuthorityEpoch: advisory.AuthorityEpoch,
		}
	}
	return classes
}
