// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

// evidenceFieldWinner selects, across a candidate's evidence facts, the
// value for one Details-derived field using a defensible, deterministic
// rule: the highest-confidence fact that carries a non-empty value wins; a
// confidence tie keeps whichever fact was considered first (aggregateCandidate
// walks facts in the order buildCandidates grouped them, which is the
// caller's original discovery order — deterministic, not Go map order).
// Facts that carry no value for the field are skipped entirely, so a
// lower-confidence fact can still contribute a value the highest-confidence
// fact in the bucket lacks. Never uses map iteration order: that is
// randomized per process and would make the winning value nondeterministic
// across otherwise-identical runs, a fresh accuracy bug the review that
// found this required guarding against explicitly.
type evidenceFieldWinner struct {
	value      string
	confidence float64
	has        bool
}

// consider offers one fact's value/confidence pair to the winner. A blank
// value never wins regardless of confidence.
func (w *evidenceFieldWinner) consider(value string, confidence float64) {
	if value == "" {
		return
	}
	if !w.has || confidence > w.confidence {
		w.value = value
		w.confidence = confidence
		w.has = true
	}
}

// evidenceFactSourceRevision reads the declared git revision (branch, tag,
// or SHA) an ArgoCD deployment source targets, set directly on the fact's
// Details by structured_family_evidence.go's
// discoverStructuredArgoCDEvidence.
func evidenceFactSourceRevision(fact EvidenceFact) string {
	return toDetailsString(fact.Details["source_revision"])
}

// evidenceFactDestinationNamespace reads the declared Kubernetes namespace an
// ArgoCD deployment targets, set directly on the fact's Details by
// yaml_iac_evidence.go's appendDestinationPlatformEvidence.
//
// IMPORTANT (#5441 P0 follow-up): as of this writing,
// appendDestinationPlatformEvidence is the ONLY producer of
// destination_namespace, and it attaches the value to a RelRunsOn fact
// targeting a Platform entity — a different Candidate bucket (different
// RelationshipType, different TargetEntityID, and even a different
// SourceRepoID: the deployed repo, not the ArgoCD manifest's own repo) than
// any RelDeploysFrom/RelDiscoversConfigIn/RelProvisionsDependencyFor/
// RelUsesModule/RelReadsConfigFrom candidate. This function and the winner
// selection in aggregateCandidate are correct and will populate the field
// the moment a fact of one of the five widened relationship types carries
// destination_namespace directly, but no current evidence producer does
// that, so this reads "" on every real corpus today. Wiring it for real
// ArgoCD data requires a cross-candidate join
// (RelDeploysFrom.TargetRepoID == RelRunsOn.SourceRepoID) that is out of
// scope for this fix — a tracked follow-up, not silently claimed as done.
func evidenceFactDestinationNamespace(fact EvidenceFact) string {
	return toDetailsString(fact.Details["destination_namespace"])
}

// evidenceFactFirstPartyRefVersion reads the pinned module/reference version
// for one evidence fact. Several evidence families set the value
// differently:
//
//   - github_actions_evidence.go sets Details["first_party_ref_version"]
//     directly (the `@ref` pin off a `uses:` reference) for both GitHub
//     Actions reusable-workflow evidence kinds, so that key is preferred
//     first when present.
//   - structured_family_evidence.go's ArgoCD evidence also sets
//     Details["first_party_ref_version"] directly (via
//     withFirstPartyRefDetails, to the same value as source_revision) —
//     preferring that key first naturally covers this family too, without a
//     special case.
//   - terraform_evidence.go, terraform_runtime_service_evidence.go,
//     ansible_evidence.go, and dockerfile_evidence.go instead set
//     Details["source_ref"] to the RAW pinned source string and never set
//     first_party_ref_version, so this falls back to deriving the `ref=`
//     query-parameter pin from source_ref via ExtractTerraformRefPin. That
//     helper only recognizes the go-getter `?ref=` shape, so it safely
//     returns "" for non-Terraform-shaped source_ref values (a Docker image
//     reference, an Ansible role URL) instead of extracting anything
//     incorrect.
func evidenceFactFirstPartyRefVersion(fact EvidenceFact) string {
	if version := toDetailsString(fact.Details["first_party_ref_version"]); version != "" {
		return version
	}
	return ExtractTerraformRefPin(toDetailsString(fact.Details["source_ref"]))
}

// toDetailsString reads a string-typed Details value, returning "" when the
// key is absent, nil, or not a string.
func toDetailsString(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}
