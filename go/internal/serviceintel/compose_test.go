// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintel

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// freshExactTruth is a supported, fresh, authoritative-graph truth envelope.
func freshExactTruth(capability string) *query.TruthEnvelope {
	return &query.TruthEnvelope{
		Level:      query.TruthLevelExact,
		Basis:      query.TruthBasisAuthoritativeGraph,
		Capability: capability,
		Freshness:  query.TruthFreshness{State: query.FreshnessFresh},
	}
}

func handle(repo, path string) query.EvidenceCitationHandle {
	return query.EvidenceCitationHandle{Kind: "source", RepoID: repo, RelativePath: path}
}

func sectionByKind(t *testing.T, r Report, kind SectionKind) ReportSection {
	t.Helper()
	for _, sec := range r.Sections {
		if sec.Kind == kind {
			return sec
		}
	}
	t.Fatalf("section %q not found in report", kind)
	return ReportSection{}
}

// fullInput builds a complete, fully-supported input for every section.
func fullInput() ReportInput {
	in := ReportInput{Subject: ReportSubject{ServiceName: "checkout", ServiceID: "svc:checkout", RepoID: "repo:checkout"}}
	for _, spec := range sectionCatalog {
		in.Sections = append(in.Sections, SectionInput{
			Kind:     spec.Kind,
			Summary:  "evidence-backed " + string(spec.Kind),
			Truth:    freshExactTruth(spec.PromptFamily),
			Evidence: []query.EvidenceCitationHandle{handle("repo:checkout", string(spec.Kind)+".go")},
		})
	}
	return in
}

func TestComposeAlwaysEmitsEverySectionInOrder(t *testing.T) {
	// Even with no inputs, the report carries the full ordered section catalog.
	r := Compose(ReportInput{Subject: ReportSubject{ServiceName: "checkout"}})
	if r.Schema != ReportSchema {
		t.Fatalf("schema = %q, want %q", r.Schema, ReportSchema)
	}
	if len(r.Sections) != len(sectionCatalog) {
		t.Fatalf("section count = %d, want %d", len(r.Sections), len(sectionCatalog))
	}
	for i, spec := range sectionCatalog {
		if r.Sections[i].Kind != spec.Kind {
			t.Fatalf("section[%d] kind = %q, want %q", i, r.Sections[i].Kind, spec.Kind)
		}
		if r.Sections[i].Title != spec.Title {
			t.Fatalf("section[%d] title = %q, want %q", i, r.Sections[i].Title, spec.Title)
		}
	}
}

func TestComposeCompleteReportIsSupported(t *testing.T) {
	r := Compose(fullInput())
	if !r.Supported {
		t.Fatalf("complete report should be supported")
	}
	if r.Partial {
		t.Fatalf("complete report should not be partial")
	}
	if r.TruthClass != query.AnswerTruthDeterministic {
		t.Fatalf("truth class = %q, want deterministic", r.TruthClass)
	}
	if r.Truth == nil {
		t.Fatalf("complete report should carry an anchor truth envelope")
	}
	for _, sec := range r.Sections {
		if sec.Status != StatusSupported {
			t.Fatalf("section %q status = %q, want supported", sec.Kind, sec.Status)
		}
		if sec.Answer.Summary == "" {
			t.Fatalf("supported section %q should keep its summary", sec.Kind)
		}
		if !sec.Answer.Supported {
			t.Fatalf("supported section %q answer should be supported", sec.Kind)
		}
	}
}

func TestComposeUnsupportedIdentityMakesReportUnsupported(t *testing.T) {
	// Service-not-found on the identity anchor: the whole report is unsupported.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionIdentity {
			in.Sections[i] = SectionInput{
				Kind: SectionIdentity,
				Err:  &query.ErrorEnvelope{Code: query.ErrorCodeServiceNotFound, Message: "service not found"},
			}
		}
	}
	r := Compose(in)
	if r.Supported {
		t.Fatalf("report should be unsupported when identity is unsupported")
	}
	if r.Truth != nil {
		t.Fatalf("unsupported report should not carry an anchor truth")
	}
	if !r.Partial {
		t.Fatalf("a report with an unsupported section must be partial")
	}
	id := sectionByKind(t, r, SectionIdentity)
	if id.Status != StatusUnsupported {
		t.Fatalf("identity status = %q, want unsupported", id.Status)
	}
	if id.Answer.Summary != "" {
		t.Fatalf("unsupported identity must drop its summary, got %q", id.Answer.Summary)
	}
	if len(id.Answer.UnsupportedReasons) == 0 {
		t.Fatalf("unsupported identity must carry a reason")
	}
}

func TestComposeEmptySectionStaysVisibleWithLimitationAndNextCall(t *testing.T) {
	// Supply chain resolved no evidence: section is partial, summary dropped,
	// and it carries a limitation plus the fallback next call.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionSupplyChain {
			in.Sections[i] = SectionInput{
				Kind:       SectionSupplyChain,
				Summary:    "this should be dropped",
				Truth:      freshExactTruth("supply_chain.impact"),
				NoEvidence: true,
			}
		}
	}
	r := Compose(in)
	sc := sectionByKind(t, r, SectionSupplyChain)
	if sc.Status != StatusPartial {
		t.Fatalf("empty supply-chain status = %q, want partial", sc.Status)
	}
	if sc.Answer.Summary != "" {
		t.Fatalf("empty section must drop confident summary, got %q", sc.Answer.Summary)
	}
	if len(sc.Answer.Limitations) == 0 {
		t.Fatalf("empty section must carry an explicit limitation")
	}
	if !hasFallback(sc, "get_supply_chain_impact_inventory") {
		t.Fatalf("empty section must recommend its fallback next call; got %+v", sc.Answer.RecommendedNextCalls)
	}
	if !r.Partial {
		t.Fatalf("report with an empty section should be partial")
	}
}

func TestComposeAbsentSectionIsUnsupportedWithNextCall(t *testing.T) {
	// Only identity supplied: the other four sections are emitted unsupported.
	in := ReportInput{Subject: ReportSubject{ServiceName: "checkout"}}
	in.Sections = append(in.Sections, SectionInput{
		Kind:     SectionIdentity,
		Summary:  "checkout service",
		Truth:    freshExactTruth("service.story"),
		Evidence: []query.EvidenceCitationHandle{handle("repo:checkout", "main.go")},
	})
	r := Compose(in)
	if !r.Supported {
		t.Fatalf("report with supported identity should be supported")
	}
	if !r.Partial {
		t.Fatalf("report missing sections should be partial")
	}
	inc := sectionByKind(t, r, SectionIncidentsSupport)
	if inc.Status != StatusUnsupported {
		t.Fatalf("absent section status = %q, want unsupported", inc.Status)
	}
	if len(inc.Answer.RecommendedNextCalls) == 0 {
		t.Fatalf("absent section must recommend a next call")
	}
	if len(inc.Answer.Limitations) == 0 {
		t.Fatalf("absent section must carry a limitation")
	}
}

func TestComposeStaleSectionIsPartialAndPreservesTruth(t *testing.T) {
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
	dep := sectionByKind(t, r, SectionDeploymentConfig)
	if dep.Status != StatusPartial {
		t.Fatalf("stale section status = %q, want partial", dep.Status)
	}
	if dep.Answer.Truth == nil || dep.Answer.Truth.Freshness.State != query.FreshnessStale {
		t.Fatalf("stale section must preserve the source stale truth")
	}
	// Truth is preserved, not reclassified: still deterministic class.
	if dep.Answer.TruthClass != query.AnswerTruthDeterministic {
		t.Fatalf("stale section truth class = %q, want deterministic (preserved)", dep.Answer.TruthClass)
	}
	if !r.Partial {
		t.Fatalf("report with a stale section should be partial")
	}
}

func TestComposeTruncatedSectionIsPartial(t *testing.T) {
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionCodeToRuntime {
			in.Sections[i].Truncated = true
		}
	}
	r := Compose(in)
	c2r := sectionByKind(t, r, SectionCodeToRuntime)
	if c2r.Status != StatusPartial {
		t.Fatalf("truncated section status = %q, want partial", c2r.Status)
	}
	if !c2r.Answer.Truncated {
		t.Fatalf("truncated section must mark the answer truncated")
	}
}

func TestComposeMissingEvidenceSectionIsPartial(t *testing.T) {
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionCodeToRuntime {
			in.Sections[i].MissingEvidence = []query.EvidenceCitationHandle{handle("repo:checkout", "missing.go")}
		}
	}

	r := Compose(in)
	c2r := sectionByKind(t, r, SectionCodeToRuntime)
	if c2r.Status != StatusPartial {
		t.Fatalf("missing-evidence section status = %q, want partial", c2r.Status)
	}
	if !c2r.Answer.Partial {
		t.Fatalf("missing-evidence section answer must be partial")
	}
	if !r.Partial {
		t.Fatalf("report with missing evidence should be partial")
	}
	if len(c2r.Answer.Limitations) == 0 {
		t.Fatalf("missing-evidence section must carry an explicit limitation")
	}
}

func TestComposeIsDeterministic(t *testing.T) {
	a := Compose(fullInput())
	b := Compose(fullInput())
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("compose is not deterministic for identical input")
	}
}

func TestComposePreservesNextCallArguments(t *testing.T) {
	in := fullInput()
	call := NextCall{
		Tool:      "get_service_story",
		Reason:    "refresh the service story with a bounded page",
		Arguments: map[string]any{"service_name": "checkout", "limit": 25},
	}
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionIdentity {
			in.Sections[i].NextCalls = []NextCall{call}
		}
	}

	r := Compose(in)
	id := sectionByKind(t, r, SectionIdentity)
	args, ok := id.Answer.RecommendedNextCalls[0]["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("section next call arguments missing: %#v", id.Answer.RecommendedNextCalls)
	}
	if got, want := args["service_name"], "checkout"; got != want {
		t.Fatalf("section next call service_name = %#v, want %#v", got, want)
	}
	if len(r.NextCalls) == 0 || r.NextCalls[0].Arguments["limit"] != 25 {
		t.Fatalf("report next call arguments not preserved: %#v", r.NextCalls)
	}
}

func TestComposeFallbackNextCallsUseSubjectArguments(t *testing.T) {
	r := Compose(ReportInput{Subject: ReportSubject{ServiceName: "checkout"}})
	dep := sectionByKind(t, r, SectionDeploymentConfig)
	if len(dep.Answer.RecommendedNextCalls) == 0 {
		t.Fatalf("deployment fallback missing next call")
	}
	call := dep.Answer.RecommendedNextCalls[0]
	if _, hasTool := call["tool"]; hasTool {
		t.Fatalf("deployment fallback should use a service-scoped playbook instead of an unbound tool: %#v", call)
	}
	args, ok := call["arguments"].(map[string]any)
	if !ok || args["service_name"] != "checkout" {
		t.Fatalf("deployment fallback arguments = %#v, want service_name checkout", call["arguments"])
	}
}

func TestComposeAggregatesAndDedupesNextCalls(t *testing.T) {
	in := fullInput()
	// Two sections recommend the same explicit next call; it must appear once.
	dup := NextCall{Tool: "get_relationship_evidence", Reason: "inspect the edge"}
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionCodeToRuntime || in.Sections[i].Kind == SectionDeploymentConfig {
			in.Sections[i].NextCalls = []NextCall{dup}
		}
	}
	r := Compose(in)
	count := 0
	for _, nc := range r.NextCalls {
		if nc.Tool == "get_relationship_evidence" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate next call appeared %d times, want 1", count)
	}
}

func TestComposeDedupesCallerNextCallAgainstSectionFallback(t *testing.T) {
	// When the caller supplies the same next call the empty section would add as
	// its fallback, the section recommends it exactly once.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionSupplyChain {
			spec, _ := specForKind(SectionSupplyChain)
			in.Sections[i] = SectionInput{
				Kind:       SectionSupplyChain,
				Truth:      freshExactTruth("supply-chain.impact"),
				NoEvidence: true,
				NextCalls:  []NextCall{spec.Fallback},
			}
		}
	}
	r := Compose(in)
	sc := sectionByKind(t, r, SectionSupplyChain)
	count := 0
	for _, call := range sc.Answer.RecommendedNextCalls {
		if call["tool"] == "get_supply_chain_impact_inventory" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("fallback next call appeared %d times in section, want 1", count)
	}
}

func TestComposeUnavailableFreshnessWithEvidencePassesThrough(t *testing.T) {
	// Deliberate contract: the composer mirrors AnswerPacket.markPartial and does
	// not reclassify unavailable freshness as partial when evidence is present
	// and NoEvidence is unset. Callers signal emptiness via NoEvidence.
	in := fullInput()
	for i := range in.Sections {
		if in.Sections[i].Kind == SectionDeploymentConfig {
			in.Sections[i].Truth = &query.TruthEnvelope{
				Level:     query.TruthLevelExact,
				Basis:     query.TruthBasisAuthoritativeGraph,
				Freshness: query.TruthFreshness{State: query.FreshnessUnavailable},
			}
		}
	}
	r := Compose(in)
	dep := sectionByKind(t, r, SectionDeploymentConfig)
	if dep.Status != StatusSupported {
		t.Fatalf("unavailable-with-evidence section status = %q, want supported (not reclassified)", dep.Status)
	}
	if dep.Answer.Truth == nil || dep.Answer.Truth.Freshness.State != query.FreshnessUnavailable {
		t.Fatalf("section must preserve the source unavailable freshness")
	}
}

// hasFallback reports whether the section's answer recommends a next call for
// the given tool.
func hasFallback(sec ReportSection, tool string) bool {
	for _, call := range sec.Answer.RecommendedNextCalls {
		if val, ok := call["tool"].(string); ok && val == tool {
			return true
		}
	}
	return false
}
