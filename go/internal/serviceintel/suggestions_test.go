// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintel

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func investigationsByBasis(r Report, basis InvestigationBasis) []SuggestedInvestigation {
	var out []SuggestedInvestigation
	for _, inv := range r.Investigations {
		if inv.Basis == basis {
			out = append(out, inv)
		}
	}
	return out
}

func TestSuggestAbsentWhenNoBasis(t *testing.T) {
	// A fully supported report with fresh evidence, no missing handles, no
	// ambiguity, and no flagged high-impact relationship has nothing to suggest.
	r := Compose(fullInput())
	if len(r.Investigations) != 0 {
		t.Fatalf("complete report should suggest no investigations, got %d: %+v", len(r.Investigations), r.Investigations)
	}
}

func TestSuggestMissingEvidence(t *testing.T) {
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionCodeToRuntime {
			in.Sections[i].MissingEvidence = []query.EvidenceCitationHandle{
				{Kind: "source", RepoID: "repo:checkout", RelativePath: "router.go", EntityID: "entity:route-1"},
			}
		}
	}
	r := Compose(in)
	got := investigationsByBasis(r, BasisMissingEvidence)
	if len(got) != 1 {
		t.Fatalf("want 1 missing-evidence investigation, got %d", len(got))
	}
	inv := got[0]
	if inv.NextCall.Tool != "build_evidence_citation_packet" || inv.NextCall.Route != "/api/v0/evidence/citations" {
		t.Fatalf("missing-evidence next call wrong: %+v", inv.NextCall)
	}
	if len(inv.EvidenceBasis) == 0 || inv.EvidenceBasis[0] != "entity:route-1" {
		t.Fatalf("missing-evidence basis should reference the unresolved handle, got %v", inv.EvidenceBasis)
	}
	// Section truth is deterministic, so completing it is expected deterministic.
	if inv.ExpectedTruthClass != query.AnswerTruthDeterministic {
		t.Fatalf("expected truth class = %q, want deterministic", inv.ExpectedTruthClass)
	}
	// The hydration call must carry the unresolved handles, or it dispatches 400.
	handles, ok := inv.NextCall.Arguments["handles"].([]map[string]any)
	if !ok || len(handles) != 1 || handles[0]["entity_id"] != "entity:route-1" {
		t.Fatalf("missing-evidence call must carry the unresolved handle, got %#v", inv.NextCall.Arguments)
	}
}

func TestSuggestStaleFreshness(t *testing.T) {
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionDeploymentConfig {
			in.Sections[i].Truth = &query.TruthEnvelope{
				Level: query.TruthLevelExact,
				Basis: query.TruthBasisAuthoritativeGraph,
				Freshness: query.TruthFreshness{
					State: query.FreshnessStale,
					Cause: query.FreshnessCauseReducerBacklog,
					NextCheck: &query.FreshnessNextCheck{
						Tool:   "get_reducer_status",
						Reason: "check reducer backlog drain",
						Params: map[string]string{"domain": "code_graph"},
					},
				},
			}
		}
	}
	r := Compose(in)
	got := investigationsByBasis(r, BasisStaleFreshness)
	if len(got) != 1 {
		t.Fatalf("want 1 stale-freshness investigation, got %d", len(got))
	}
	inv := got[0]
	if inv.NextCall.Tool != "get_reducer_status" {
		t.Fatalf("stale investigation should use the freshness next check, got %+v", inv.NextCall)
	}
	if len(inv.EvidenceBasis) != 1 || inv.EvidenceBasis[0] != string(query.FreshnessCauseReducerBacklog) {
		t.Fatalf("stale evidence basis should be the cause, got %v", inv.EvidenceBasis)
	}
	// The scoped freshness params must survive onto the suggestion's call.
	if inv.NextCall.Arguments["domain"] != "code_graph" {
		t.Fatalf("stale call must preserve freshness next-check params, got %#v", inv.NextCall.Arguments)
	}
}

func TestSuggestStaleFreshnessAbsentWithoutCause(t *testing.T) {
	// Stale without a proven cause has no bounded next check, so no suggestion.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionDeploymentConfig {
			in.Sections[i].Truth = &query.TruthEnvelope{
				Level:     query.TruthLevelExact,
				Basis:     query.TruthBasisAuthoritativeGraph,
				Freshness: query.TruthFreshness{State: query.FreshnessStale},
			}
		}
	}
	r := Compose(in)
	if len(investigationsByBasis(r, BasisStaleFreshness)) != 0 {
		t.Fatalf("stale without cause must not suggest an investigation")
	}
}

func TestSuggestAmbiguousTargetWithoutChoosingWinner(t *testing.T) {
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionIdentity {
			in.Sections[i] = SectionInput{
				Kind: SectionIdentity,
				Err:  &query.ErrorEnvelope{Code: query.ErrorCodeAmbiguous, Message: "3 services match 'checkout'"},
			}
		}
	}
	r := Compose(in)
	got := investigationsByBasis(r, BasisAmbiguousTarget)
	if len(got) != 1 {
		t.Fatalf("want 1 ambiguous-target investigation, got %d", len(got))
	}
	inv := got[0]
	if inv.NextCall.Tool != "resolve_entity" || inv.NextCall.Route != "/api/v0/entities/resolve" {
		t.Fatalf("ambiguous next call should be resolve_entity, got %+v", inv.NextCall)
	}
	if len(inv.EvidenceBasis) == 0 || inv.EvidenceBasis[0] != "3 services match 'checkout'" {
		t.Fatalf("ambiguous evidence basis should carry the ambiguity message, got %v", inv.EvidenceBasis)
	}
	// resolve_entity needs a name, so the subject name must be a bounded argument.
	if inv.NextCall.Arguments["name"] != "checkout" {
		t.Fatalf("ambiguous call must pass the subject name to resolve_entity, got %#v", inv.NextCall.Arguments)
	}
	// An ambiguous section must NOT also produce an unsupported-lane suggestion;
	// the report must not collapse ambiguity into a single guessed lane.
	if len(investigationsByBasis(r, BasisUnsupportedLane)) != 0 {
		for _, lane := range investigationsByBasis(r, BasisUnsupportedLane) {
			if lane.Section == SectionIdentity {
				t.Fatalf("ambiguous identity must not also yield an unsupported-lane suggestion")
			}
		}
	}
}

func TestSuggestUnsupportedLane(t *testing.T) {
	// Supply-chain lane errors with an unsupported capability: suggest gathering
	// it via the section fallback.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionSupplyChain {
			in.Sections[i] = SectionInput{
				Kind: SectionSupplyChain,
				Err:  &query.ErrorEnvelope{Code: query.ErrorCodeUnsupportedCapability, Message: "supply-chain lane not enabled"},
			}
		}
	}
	r := Compose(in)
	got := investigationsByBasis(r, BasisUnsupportedLane)
	if len(got) != 1 {
		t.Fatalf("want 1 unsupported-lane investigation, got %d", len(got))
	}
	if got[0].NextCall.Tool != "get_supply_chain_impact_inventory" {
		t.Fatalf("unsupported-lane next call should be the supply-chain fallback, got %+v", got[0].NextCall)
	}
	if got[0].Section != SectionSupplyChain {
		t.Fatalf("unsupported-lane section = %q, want supply_chain", got[0].Section)
	}
}

func TestSuggestHighImpactRelationshipOnlyWhenFlagged(t *testing.T) {
	// Without the flag, a supported section yields no high-impact suggestion.
	if len(investigationsByBasis(Compose(fullInput()), BasisHighImpactRelationship)) != 0 {
		t.Fatalf("unflagged section must not yield a high-impact suggestion")
	}
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionCodeToRuntime {
			in.Sections[i].HighImpact = true
			in.Sections[i].Evidence = []query.EvidenceCitationHandle{{Kind: "entity", EntityID: "entity:edge-1"}}
		}
	}
	r := Compose(in)
	got := investigationsByBasis(r, BasisHighImpactRelationship)
	if len(got) != 1 {
		t.Fatalf("want 1 high-impact investigation when flagged, got %d", len(got))
	}
	if got[0].NextCall.Tool != "get_relationship_evidence" {
		t.Fatalf("high-impact next call should be get_relationship_evidence, got %+v", got[0].NextCall)
	}
	// get_relationship_evidence needs resolved_id, taken from the section evidence.
	if got[0].NextCall.Arguments["resolved_id"] != "entity:edge-1" {
		t.Fatalf("high-impact call must carry resolved_id, got %#v", got[0].NextCall.Arguments)
	}
}

func TestSuggestHighImpactSuppressedWithoutResolvableID(t *testing.T) {
	// Flagged and supported, but the evidence carries no entity id to follow, so
	// get_relationship_evidence would be non-executable: suppress the suggestion.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionCodeToRuntime {
			in.Sections[i].HighImpact = true
			in.Sections[i].Evidence = []query.EvidenceCitationHandle{{Kind: "file", RepoID: "repo:checkout", RelativePath: "main.go"}}
		}
	}
	if len(investigationsByBasis(Compose(in), BasisHighImpactRelationship)) != 0 {
		t.Fatalf("high-impact must be suppressed when no resolvable entity id is present")
	}
}

func TestSuggestHighImpactSuppressedWhenSectionNotSupported(t *testing.T) {
	// A flagged but empty section must not produce a high-impact "verify the
	// relationship" suggestion; there is no resolved relationship to verify.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionCodeToRuntime {
			in.Sections[i] = SectionInput{
				Kind:       SectionCodeToRuntime,
				Truth:      freshExactTruth("service.story"),
				NoEvidence: true,
				HighImpact: true,
			}
		}
	}
	r := Compose(in)
	if len(investigationsByBasis(r, BasisHighImpactRelationship)) != 0 {
		t.Fatalf("high-impact suggestion must be suppressed for a non-supported section")
	}
}

func TestSuggestIsDeterministicAndDeduped(t *testing.T) {
	build := func() ReportInput {
		in := fullInput()
		for i := range in.Sections {
			if in.Sections[i].Kind == SectionCodeToRuntime {
				in.Sections[i].MissingEvidence = []query.EvidenceCitationHandle{
					{EntityID: "entity:a"}, {EntityID: "entity:b"},
				}
			}
		}
		return in
	}
	a := Compose(build())
	b := Compose(build())
	if !reflect.DeepEqual(a.Investigations, b.Investigations) {
		t.Fatalf("suggested investigations are not deterministic")
	}
	// One section with multiple missing handles yields a single missing-evidence
	// suggestion (de-duplicated by id), not one per handle.
	if got := len(investigationsByBasis(a, BasisMissingEvidence)); got != 1 {
		t.Fatalf("missing-evidence investigations = %d, want 1 (deduped)", got)
	}
	ids := map[string]int{}
	for _, inv := range a.Investigations {
		ids[inv.ID]++
	}
	for id, n := range ids {
		if n != 1 {
			t.Fatalf("investigation id %q appeared %d times, want 1", id, n)
		}
	}
}

func TestSuggestBounded(t *testing.T) {
	// Drive the realistic maximum: every section unsupported AND carrying missing
	// evidence, i.e. two signals per section. The list must stay bounded and
	// surface every signal (none silently dropped under the backstop cap).
	in := ReportInput{Subject: ReportSubject{ServiceName: "checkout"}}
	for _, spec := range sectionCatalog {
		in.Sections = append(in.Sections, SectionInput{
			Kind:            spec.Kind,
			Err:             &query.ErrorEnvelope{Code: query.ErrorCodeUnsupportedCapability, Message: "lane down"},
			MissingEvidence: []query.EvidenceCitationHandle{{EntityID: "e:" + string(spec.Kind)}},
		})
	}
	r := Compose(in)
	if len(r.Investigations) > maxInvestigations {
		t.Fatalf("investigations = %d, exceeds bound %d", len(r.Investigations), maxInvestigations)
	}
	if want := 2 * len(sectionCatalog); len(r.Investigations) != want {
		t.Fatalf("investigations = %d, want %d (unsupported + missing per section)", len(r.Investigations), want)
	}
}

func TestSectionFallbacksLinkAPlaybook(t *testing.T) {
	// expectedTruthClass relies on every unsupported-lane fallback carrying a
	// playbook so it never defaults to an invented truth class for a section with
	// no source truth. Lock that invariant against catalog drift.
	for _, spec := range sectionCatalog {
		if spec.Fallback.Playbook == "" {
			t.Fatalf("section %q fallback must link a query playbook", spec.Kind)
		}
		if _, ok := query.LookupPlaybook(spec.Fallback.Playbook); !ok {
			t.Fatalf("section %q fallback playbook %q does not exist", spec.Kind, spec.Fallback.Playbook)
		}
	}
}

func TestSuggestExpectedTruthFromPlaybookWhenSectionHasNoTruth(t *testing.T) {
	// Unsupported supply-chain lane: the section carries no truth, so expected
	// truth class comes from the linked playbook's terminal step, not invented.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionSupplyChain {
			in.Sections[i] = SectionInput{
				Kind: SectionSupplyChain,
				Err:  &query.ErrorEnvelope{Code: query.ErrorCodeUnsupportedCapability, Message: "lane down"},
			}
		}
	}
	r := Compose(in)
	got := investigationsByBasis(r, BasisUnsupportedLane)
	if len(got) != 1 {
		t.Fatalf("want 1 unsupported-lane investigation, got %d", len(got))
	}
	want, ok := playbookTerminalTruth("supply_chain_impact_explanation")
	if !ok {
		t.Fatalf("expected the supply-chain playbook to declare a terminal truth")
	}
	if got[0].ExpectedTruthClass != want {
		t.Fatalf("expected truth class = %q, want playbook terminal %q", got[0].ExpectedTruthClass, want)
	}
}
