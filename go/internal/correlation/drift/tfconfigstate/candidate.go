package tfconfigstate

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// Evidence type tokens used on drift EvidenceAtoms. The drift RulePack
// (rules.TerraformConfigStateDriftRulePack) keys its RequiredEvidence selector
// on EvidenceTypeDriftAddress so candidates that lack the cross-scope address
// atom are rejected as structural mismatches by the admission gate.
const (
	// EvidenceTypeDriftAddress marks the address-carrying EvidenceAtom that
	// satisfies the rule pack's structural requirement. Exactly one such atom
	// is attached per Candidate.
	EvidenceTypeDriftAddress = "terraform_drift_address"
	// EvidenceTypeConfigResource marks the config-side resource view (parsed
	// HCL terraform_resources fact).
	EvidenceTypeConfigResource = "terraform_config_resource"
	// EvidenceTypeStateResource marks the current-generation state-side
	// resource view (collector terraform_state_resource fact).
	EvidenceTypeStateResource = "terraform_state_resource"
	// EvidenceTypePriorStateResource marks the prior-generation state-side
	// resource view used by removed_from_state classification.
	EvidenceTypePriorStateResource = "terraform_state_resource_prior_generation"
	// EvidenceTypeDriftKind marks the classifier output atom so the explain
	// trace can read which drift kind admitted the candidate.
	EvidenceTypeDriftKind = "terraform_drift_kind"
)

// EvidenceKeyAddress is the canonical EvidenceAtom.Key for address-bearing
// atoms (config, state, prior). The classifier joins on this key.
const EvidenceKeyAddress = "resource_address"

// EvidenceKeyDriftKind is the EvidenceAtom.Key used on the drift-kind output
// atom. The value carries the DriftKind string.
const EvidenceKeyDriftKind = "drift_kind"

// driftSourceSystem identifies the synthetic source system that owns drift
// EvidenceAtoms. The rules.TerraformConfigStateDriftRulePack does not select
// on source_system today, so any stable, non-blank value works; we use a
// stable string so explain traces can attribute drift atoms.
const driftSourceSystem = "reducer/terraform_config_state_drift"

// driftConfidence is the admission confidence assigned to drift candidates.
// It must be at or above the rule pack's MinAdmissionConfidence
// (rules.TerraformConfigStateDriftRulePack returns 0.80); we use 1.0 because
// the classifier already proved the drift exists — the threshold guards
// against structurally malformed candidates, not semantic uncertainty.
const driftConfidence = 1.0

// AddressedRow couples a Terraform resource address with the optional config,
// state, and prior-state views the classifier needs. The candidate builder
// consumes one AddressedRow per address; addresses without disagreement are
// omitted by the upstream join.
type AddressedRow struct {
	Address      string
	ResourceType string
	Config       *ResourceRow
	State        *ResourceRow
	Prior        *ResourceRow
}

// BuildCandidates produces one correlation Candidate per drifted address. The
// input is the joined address set produced by the resolver and the reducer
// handler; the output is a deterministic slice ordered by address (then by
// drift kind) so the correlation engine produces a stable explain trace.
//
// Candidates whose Classify call returns the empty string are omitted from
// the output; only drifted candidates flow downstream. Each emitted candidate
// carries:
//
//   - one EvidenceAtom of type EvidenceTypeDriftAddress satisfying the rule
//     pack's structural admission gate;
//   - one EvidenceAtom of type EvidenceTypeDriftKind carrying the classifier
//     output for downstream telemetry labeling;
//   - cross-scope provenance atoms for whichever of config/state/prior was
//     non-nil (config scope = anchor.ScopeID; state scope =
//     stateScopeID; prior atoms reuse stateScopeID by design — they belong
//     to the same state lineage).
//
// stateScopeID is the canonical state_snapshot ScopeID for the active
// generation. anchor.ScopeID and anchor.RepoID are the resolved config-side
// scope and owning repo for the join key.
func BuildCandidates(
	addressed []AddressedRow,
	anchor tfstatebackend.CommitAnchor,
	stateScopeID string,
) []model.Candidate {
	if len(addressed) == 0 {
		return nil
	}

	// Deterministic input order: sort by address so candidate IDs are stable
	// across reducer reruns. The engine sorts results by CorrelationKey,
	// but ordering inputs gives us stable Candidate.ID assignment too.
	rows := slices.Clone(addressed)
	slices.SortFunc(rows, func(a, b AddressedRow) int {
		return cmp.Compare(a.Address, b.Address)
	})

	out := make([]model.Candidate, 0, len(rows))
	for _, row := range rows {
		kind := Classify(row.Config, row.State, row.Prior)
		if kind == "" {
			continue
		}
		out = append(out, buildOneCandidate(row, kind, anchor, stateScopeID))
	}
	return out
}

func buildOneCandidate(
	row AddressedRow,
	kind DriftKind,
	anchor tfstatebackend.CommitAnchor,
	stateScopeID string,
) model.Candidate {
	candidateID := fmt.Sprintf("drift:%s:%s:%s", anchor.LocatorHash, row.Address, kind)
	evidence := make([]model.EvidenceAtom, 0, 5)

	// Required address atom — satisfies the rule pack's structural admission
	// requirement (EvidenceFieldEvidenceType == EvidenceTypeDriftAddress).
	evidence = append(evidence, model.EvidenceAtom{
		ID:           candidateID + "/address",
		SourceSystem: driftSourceSystem,
		EvidenceType: EvidenceTypeDriftAddress,
		ScopeID:      stateScopeID,
		Key:          EvidenceKeyAddress,
		Value:        row.Address,
		Confidence:   driftConfidence,
	})

	// Drift-kind atom — read downstream by the explain trace and telemetry
	// emitter to label the eshu_dp_correlation_drift_detected_total counter.
	evidence = append(evidence, model.EvidenceAtom{
		ID:           candidateID + "/drift_kind",
		SourceSystem: driftSourceSystem,
		EvidenceType: EvidenceTypeDriftKind,
		ScopeID:      stateScopeID,
		Key:          EvidenceKeyDriftKind,
		Value:        kind.String(),
		Confidence:   driftConfidence,
	})

	if row.Config != nil {
		evidence = append(evidence, model.EvidenceAtom{
			ID:           candidateID + "/config",
			SourceSystem: driftSourceSystem,
			EvidenceType: EvidenceTypeConfigResource,
			ScopeID:      anchor.ScopeID,
			Key:          EvidenceKeyAddress,
			Value:        row.Config.Address,
			Confidence:   driftConfidence,
		})
	}
	if row.State != nil {
		evidence = append(evidence, model.EvidenceAtom{
			ID:           candidateID + "/state",
			SourceSystem: driftSourceSystem,
			EvidenceType: EvidenceTypeStateResource,
			ScopeID:      stateScopeID,
			Key:          EvidenceKeyAddress,
			Value:        row.State.Address,
			Confidence:   driftConfidence,
		})
	}
	if row.Prior != nil {
		evidence = append(evidence, model.EvidenceAtom{
			ID:           candidateID + "/prior",
			SourceSystem: driftSourceSystem,
			EvidenceType: EvidenceTypePriorStateResource,
			ScopeID:      stateScopeID,
			Key:          EvidenceKeyAddress,
			Value:        row.Prior.Address,
			Confidence:   driftConfidence,
		})
	}

	return model.Candidate{
		ID:             candidateID,
		Kind:           rules.TerraformConfigStateDriftPackName,
		CorrelationKey: row.Address,
		Confidence:     driftConfidence,
		State:          model.CandidateStateProvisional,
		Evidence:       evidence,
	}
}
