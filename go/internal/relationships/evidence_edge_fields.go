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

// settled reports whether no future consider() call could ever change this
// winner's outcome, so the caller can skip computing that fact's (possibly
// expensive, e.g. evidenceFactFirstPartyRefVersion's ExtractTerraformRefPin
// derivation) value entirely rather than compute-then-discard it. Confidence
// is always clamped to at most 1.0 (clampConfidence), so once a winner holds
// a value at confidence 1.0, no later fact can out-rank it, and a later
// same-confidence (1.0) fact would still lose the existing tie-break (first
// recorded wins ties) — so the outcome cannot change either way.
//
// #5441 review round 2, P1-2: measured, not assumed (Prove-The-Theory-First).
// See BenchmarkResolveCandidateAggregation
// (resolver_edge_fields_bench_test.go) and the before/after in
// docs/internal/evidence/5441-edge-node-properties.md — this guard measured
// as a no-op on that benchmark's realistic-confidence fixture (no fact ever
// reaches exactly 1.0), which is expected: it only pays off for the narrow
// case of an explicit, maximum-confidence fact appearing before others in a
// candidate's evidence bucket. Kept because it is a strict, provably safe
// early exit with no measured downside, not because it fixed the regression.
func (w *evidenceFieldWinner) settled() bool {
	return w.has && w.confidence >= 1.0
}

// evidenceFactSourceRevision reads the declared git revision (branch, tag,
// or SHA) an ArgoCD deployment source targets, set directly on the fact's
// Details by structured_family_evidence.go's
// discoverStructuredArgoCDEvidence.
func evidenceFactSourceRevision(fact EvidenceFact) string {
	return toDetailsString(fact.Details["source_revision"])
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
