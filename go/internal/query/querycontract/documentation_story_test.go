// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// documentationStoryStubStore is a ContentStore that also answers
// documentation-findings reads from a canned read model.
type documentationStoryStubStore struct {
	ContentStore
	readModel DocumentationFindingListReadModel
	err       error
}

func (s documentationStoryStubStore) DocumentationFindings(
	context.Context,
	DocumentationFindingFilter,
) (DocumentationFindingListReadModel, error) {
	return s.readModel, s.err
}

// TestDocumentationTargetScope proves service wins, then explicit target,
// then the repository fallback.
func TestDocumentationTargetScope(t *testing.T) {
	t.Parallel()

	if !DocumentationTargetScopeHasSelector(DocumentationTargetScope{Repository: "shop"}) {
		t.Fatal("repository scope should have a selector")
	}
	if DocumentationTargetScopeHasSelector(DocumentationTargetScope{}) {
		t.Fatal("empty scope must not have a selector")
	}
	service := DocumentationTargetScopeFromValues("shop", "repository", "shop", "svc")
	if service.TargetKind != "service" || service.TargetID != "svc" {
		t.Fatalf("service scope = %#v, want service/svc", service)
	}
	target := DocumentationTargetScopeFromValues("shop", "repository", "shop", "")
	if target.TargetKind != "repository" || target.TargetID != "shop" {
		t.Fatalf("target scope = %#v, want repository/shop", target)
	}
	repo := DocumentationTargetScopeFromValues("shop", "", "", "")
	if repo.TargetKind != "repository" || repo.TargetID != "shop" {
		t.Fatalf("repo fallback scope = %#v, want repository/shop", repo)
	}
	finding := DocumentationTargetScopeFromFindingFilter(DocumentationFindingFilter{
		Repository: "shop", TargetKind: "service", TargetID: "svc",
	})
	if finding.TargetKind != "service" || finding.TargetID != "svc" {
		t.Fatalf("finding scope = %#v, want service/svc", finding)
	}
	fact := DocumentationTargetScopeFromFactFilter(DocumentationFactFilter{
		Repository: "shop", TargetKind: "repository", TargetID: "shop",
	})
	if fact.TargetKind != "repository" || fact.TargetID != "shop" {
		t.Fatalf("fact scope = %#v, want repository/shop", fact)
	}
}

// TestDocumentationTargetRefs proves ref selection, repo fallback, and
// trimming dedup.
func TestDocumentationTargetRefs(t *testing.T) {
	t.Parallel()

	refs := DocumentationTargetRefsFromFindingFilter(DocumentationFindingFilter{ServiceID: "svc", TargetID: "svc"})
	if len(refs) != 1 || refs[0] != (DocumentationTargetRef{Kind: "service", ID: "svc"}) {
		t.Fatalf("service refs = %#v, want one deduped service ref", refs)
	}
	factRefs := DocumentationTargetRefsFromFactFilter(DocumentationFactFilter{Repository: "shop"})
	if len(factRefs) != 1 || factRefs[0] != (DocumentationTargetRef{Kind: "repository", ID: "shop"}) {
		t.Fatalf("fact refs = %#v, want the repository fallback", factRefs)
	}
	if refs := DocumentationTargetRefs(DocumentationTargetScope{}); len(refs) != 0 {
		t.Fatalf("empty scope refs = %#v, want none", refs)
	}
	deduped := UniqueDocumentationTargetRefs([]DocumentationTargetRef{
		{Kind: " repository ", ID: " shop "},
		{Kind: "repository", ID: "shop"},
		{Kind: "service", ID: ""},
		{Kind: "service", ID: "svc"},
	})
	want := []DocumentationTargetRef{{Kind: "repository", ID: "shop"}, {Kind: "service", ID: "svc"}}
	if !reflect.DeepEqual(deduped, want) {
		t.Fatalf("deduped refs = %#v, want %#v", deduped, want)
	}
	if DocumentationFindingFilterHasExplicitTarget(DocumentationFindingFilter{TargetID: "x"}) != true {
		t.Fatal("target filter should be explicit")
	}
	if DocumentationFindingFilterHasExplicitTarget(DocumentationFindingFilter{ServiceID: "s"}) != true {
		t.Fatal("service filter should be explicit")
	}
	if DocumentationFindingFilterHasExplicitTarget(DocumentationFindingFilter{Repository: "shop"}) {
		t.Fatal("repository-only filter must not be explicit")
	}
}

// TestDocumentationPayloadMatches proves ref matching over every list
// shape plus the nested payload fallback.
func TestDocumentationPayloadMatches(t *testing.T) {
	t.Parallel()

	ref := DocumentationTargetRef{Kind: "repository", ID: "shop"}
	matching := map[string]any{
		"candidate_refs": []any{map[string]any{"kind": "repository", "id": "shop"}},
	}
	if !DocumentationPayloadMatchesTargetRef(matching, ref) {
		t.Fatal("candidate_refs match missed")
	}
	evidence := map[string]any{
		"evidence_refs": []map[string]any{{"kind": "repository", "id": "shop"}},
	}
	if !DocumentationPayloadMatchesTargetRef(evidence, ref) {
		t.Fatal("evidence_refs match missed")
	}
	linked := map[string]any{
		"linked_entities": []any{map[string]any{"entity_type": "repository", "entity_id": "shop"}},
	}
	if !DocumentationPayloadMatchesTargetRef(linked, ref) {
		t.Fatal("linked_entities match missed")
	}
	typed := map[string]any{
		"candidate_refs": []map[string]string{{"kind": "repository", "id": "shop"}},
	}
	if !DocumentationPayloadMatchesTargetRef(typed, ref) {
		t.Fatal("typed-map match missed")
	}
	wildcard := map[string]any{
		"candidate_refs": []any{map[string]any{"kind": "service", "id": "shop"}},
	}
	if !DocumentationPayloadMatchesTargetRef(wildcard, DocumentationTargetRef{ID: "shop"}) {
		t.Fatal("blank-kind ref should match any kind")
	}
	if DocumentationPayloadMatchesTargetRef(wildcard, ref) {
		t.Fatal("kind mismatch must not match")
	}
	if DocumentationPayloadMatchesTargetRefs(map[string]any{}, []DocumentationTargetRef{ref}) {
		t.Fatal("empty payload must not match")
	}
	nested := map[string]any{
		"payload": map[string]any{
			"candidate_refs": []any{map[string]any{"kind": "repository", "id": "shop"}},
		},
	}
	if !DocumentationPayloadMatchesTargetRefs(nested, []DocumentationTargetRef{ref}) {
		t.Fatal("nested payload match missed")
	}
	if DocumentationPayloadMatchesTargetRefs(nested, []DocumentationTargetRef{{Kind: "repository", ID: "other"}}) {
		t.Fatal("nested payload mismatch must not match")
	}
	if documentationPayloadMatchesTargetRef(matching, ref) != DocumentationPayloadMatchesTargetRef(matching, ref) {
		t.Fatal("historical alias diverges from the exported matcher")
	}
	if got := DocumentationStringAny("v"); got != "v" {
		t.Fatalf("DocumentationStringAny = %q, want v", got)
	}
	if got := DocumentationStringAny(3); got != "" {
		t.Fatalf("non-string DocumentationStringAny = %q, want empty", got)
	}
}

// TestDocumentationMissingEvidenceForTarget proves each thin-readback
// reason maps to its case.
func TestDocumentationMissingEvidenceForTarget(t *testing.T) {
	t.Parallel()

	if got := DocumentationMissingEvidenceForTarget(DocumentationTargetCoverage{}); got != nil {
		t.Fatalf("selector-less coverage = %#v, want nil", got)
	}
	withFindings := DocumentationTargetCoverage{
		Target:           DocumentationTargetScope{Repository: "shop"},
		FindingsReturned: 2,
	}
	if got := DocumentationMissingEvidenceForTarget(withFindings); got != nil {
		t.Fatalf("covered target = %#v, want nil", got)
	}
	target := DocumentationTargetScope{Repository: "shop"}
	cases := []struct {
		name     string
		coverage DocumentationTargetCoverage
		reason   string
	}{
		{"facts_without_findings", DocumentationTargetCoverage{Target: target, TargetFactCount: 3}, "documentation_findings_absent"},
		{"source_only", DocumentationTargetCoverage{Target: target, SourceOnlyCount: 2}, "target_link_not_modeled"},
		{"nothing", DocumentationTargetCoverage{Target: target}, "documentation_target_facts_absent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DocumentationMissingEvidenceForTarget(tc.coverage)
			if len(got) != 1 || got[0].Reason != tc.reason || got[0].Detail == "" {
				t.Fatalf("missing evidence = %#v, want reason %q with detail", got, tc.reason)
			}
		})
	}
}

// TestLoadStoryTargetDocumentation proves the store, error, blank-repo,
// and non-store paths.
func TestLoadStoryTargetDocumentation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if doc, err := LoadRepositoryStoryTargetDocumentation(ctx, documentationStoryStubStore{}, "  "); doc != nil || err != nil {
		t.Fatalf("blank repo load = (%#v, %v), want (nil, nil)", doc, err)
	}
	plain := struct{ ContentStore }{nil}
	if doc, err := LoadStoryTargetDocumentation(ctx, plain, DocumentationFindingFilter{}); doc != nil || err != nil {
		t.Fatalf("non-store load = (%#v, %v), want (nil, nil)", doc, err)
	}
	boom := errors.New("store down")
	failing := documentationStoryStubStore{err: boom}
	if _, err := LoadStoryTargetDocumentation(ctx, failing, DocumentationFindingFilter{Repository: "shop"}); !errors.Is(err, boom) {
		t.Fatalf("failing load err = %v, want the store error", err)
	}
	readModel := DocumentationFindingListReadModel{
		Findings: []map[string]any{
			{"candidate_refs": []any{map[string]any{"kind": "repository", "id": "shop"}}},
		},
		Coverage: DocumentationTargetCoverage{
			Target:          DocumentationTargetScope{Repository: "shop"},
			TargetFactCount: 1,
		},
	}
	store := documentationStoryStubStore{readModel: readModel}
	doc, err := LoadRepositoryStoryTargetDocumentation(ctx, store, "shop")
	if err != nil {
		t.Fatalf("load err = %v, want nil", err)
	}
	if doc["finding_count"] != 1 || doc["source"] != "documentation_read_model" || doc["limit"] != DocumentationStoryReadLimit {
		t.Fatalf("loaded doc = %#v, want counts, source, and limit", doc)
	}
	empty := DocumentationFindingListReadModel{}
	if doc, err := LoadStoryTargetDocumentation(ctx, documentationStoryStubStore{readModel: empty},
		DocumentationFindingFilter{Repository: "shop"}); doc != nil || err != nil {
		t.Fatalf("empty readback = (%#v, %v), want (nil, nil)", doc, err)
	}
}

// TestAttachStoryTargetDocumentation proves attach, summary wiring, and
// duplicate suppression.
func TestAttachStoryTargetDocumentation(t *testing.T) {
	t.Parallel()

	overview := map[string]any{"name": "api"}
	if got := AttachStoryTargetDocumentation(overview, nil); !reflect.DeepEqual(got, overview) {
		t.Fatalf("empty attach changed the overview: %#v", got)
	}
	doc := map[string]any{"finding_count": 2, "related_fact_count": 0}
	attached := AttachStoryTargetDocumentation(nil, doc)
	if attached["target_documentation"] == nil {
		t.Fatalf("attached = %#v, want target_documentation", attached)
	}
	story, _ := attached["story"].(string)
	if !strings.Contains(story, "2 target-linked finding(s)") {
		t.Fatalf("attached story = %q, want the findings summary", story)
	}
	again := AttachStoryTargetDocumentation(attached, doc)
	if again["story"] != story {
		t.Fatalf("re-attach story = %q, want no duplicate fragment", again["story"])
	}
}

// TestBuildStoryTargetDocumentationMaps proves coverage and missing
// evidence render into the response map.
func TestBuildStoryTargetDocumentationMaps(t *testing.T) {
	t.Parallel()

	coverage := DocumentationTargetCoverage{
		Target:              DocumentationTargetScope{Repository: "shop"},
		FindingsReturned:    1,
		TargetFactCount:     2,
		TargetFactKinds:     map[string]int{"doc": 2},
		SourceOnlyCount:     1,
		SourceOnlyFactKinds: map[string]int{"ext": 1},
		Truncated:           true,
	}
	target, _ := documentationTargetCoverageMap(coverage)["target"].(map[string]any)
	if target["repository"] != "shop" || target["target_kind"] != "" {
		t.Fatalf("coverage target = %#v, want the repository scope", target)
	}
	maps := documentationMissingEvidenceMaps([]DocumentationMissingEvidence{
		{Reason: "r", Detail: "d"},
		{Reason: "bare"},
	})
	if len(maps) != 2 || maps[0]["detail"] != "d" {
		t.Fatalf("missing maps = %#v, want reason/detail rows", maps)
	}
	if _, ok := maps[1]["detail"]; ok {
		t.Fatalf("blank detail leaked into %#v", maps[1])
	}
	if got := documentationMissingEvidenceMaps(nil); len(got) != 0 {
		t.Fatalf("nil missing maps = %#v, want empty", got)
	}
	summary := StoryTargetDocumentationSummary(map[string]any{"finding_count": 3})
	if !strings.Contains(summary, "3 target-linked finding(s)") {
		t.Fatalf("summary = %q, want the findings sentence", summary)
	}
	related := StoryTargetDocumentationSummary(map[string]any{"related_fact_count": 2})
	if !strings.Contains(related, "2 target-related fact(s)") {
		t.Fatalf("related summary = %q, want the facts sentence", related)
	}
	sourceOnly := StoryTargetDocumentationSummary(map[string]any{
		"coverage": map[string]any{"source_only_count": 1},
	})
	if !strings.Contains(sourceOnly, "none carry structured refs") {
		t.Fatalf("source-only summary = %q, want the refs sentence", sourceOnly)
	}
	if got := StoryTargetDocumentationSummary(map[string]any{}); got != "" {
		t.Fatalf("empty summary = %q, want empty", got)
	}
	fields := map[string]any{}
	appendStoryField(fields, "  ")
	if len(fields) != 0 {
		t.Fatalf("blank fragment wrote %#v, want nothing", fields)
	}
	appendStoryField(fields, "first")
	appendStoryField(fields, "second")
	if fields["story"] != "first second" {
		t.Fatalf("story = %#v, want appended fragments", fields)
	}
}
