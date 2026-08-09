// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The documentation_edges family Odù (#5994, under the #5543 umbrella).
//
// ExtractDocumentationEdgeRowsWithQuarantine projects a MENTIONS edge from a
// DocumentationSection node to the entity a mention resolves to. Every clause
// that decides whether an edge exists is exercised below, because an Odù that
// only carries happy-path mentions proves the extractor emits edges without
// proving it emits the RIGHT ones — and this fixture is asserted as an exact
// set, so a silently-included edge fails as loudly as a missing one.
const (
	documentationFamilyOduName      = "odu:ifa-documentation-family"
	documentationFamilyScopeID      = "scope-ifa-documentation-family"
	documentationFamilyGenerationID = "gen-1"
	documentationFamilyDocID        = "doc-platform-guide"
	documentationFamilySectionID    = "sec-overview"
)

// documentationFamilyOdu carries one document section whose mentions cover the
// extractor's full decision surface: two edges that must exist, and five
// mentions that must NOT produce one.
//
// The negative cases are the point. Each corresponds to a distinct early
// `continue` in ExtractDocumentationEdgeRowsWithQuarantine, so an edit that
// deletes any one of those guards turns this fixture's exact-set assertion red
// instead of passing with a quietly larger graph:
//
//   - resolution_status != "exact"        (ambiguous mentions are not evidence)
//   - len(candidate_refs) != 1            (two candidates is not a resolution)
//   - target_kind == "service"            (services are deliberately excluded)
//   - blank section_id after trimming     (whitespace-only is not an identity)
//   - duplicate section->target pair      (the seen[] dedup)
func documentationFamilyOdu() CatalogOdu {
	odu := Odu{
		Name: documentationFamilyOduName,
		Facts: []facts.Envelope{
			// EDGE 1: the ordinary resolved mention.
			documentationFamilyMention(map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "function",
				"candidate_refs": []map[string]any{
					{"id": "func:payments.Charge", "kind": "function"},
				},
			}),
			// EDGE 2: a second target from the same section, proving the
			// section->target pair is the identity rather than the section.
			documentationFamilyMention(map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "table",
				"candidate_refs": []map[string]any{
					{"id": "sqltable:public.payments", "kind": "table"},
				},
			}),
			// NOT AN EDGE: same section, same target as EDGE 1, different
			// mention text. The dedup keys on section_uid->target_entity_id, so
			// a second mention of the same thing must not double the graph.
			documentationFamilyMention(map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "function",
				"mention_text":      "Charge()",
				"candidate_refs": []map[string]any{
					{"id": "func:payments.Charge", "kind": "function"},
				},
			}),
			// NOT AN EDGE: unresolved. An ambiguous mention names a candidate
			// but has not decided, and a guess is worse than no edge.
			documentationFamilyMention(map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": "ambiguous",
				"candidate_refs": []map[string]any{
					{"id": "func:payments.Refund", "kind": "function"},
				},
			}),
			// NOT AN EDGE: two candidates. "exact" with more than one candidate
			// is a contradiction the extractor refuses to resolve arbitrarily.
			documentationFamilyMention(map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"candidate_refs": []map[string]any{
					{"id": "func:payments.Void", "kind": "function"},
					{"id": "func:billing.Void", "kind": "function"},
				},
			}),
			// NOT AN EDGE: target_kind "service" is excluded by an explicit
			// branch in the extractor.
			documentationFamilyMention(map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"candidate_refs": []map[string]any{
					{"id": "service:payments", "kind": "service"},
				},
			}),
			// NOT AN EDGE: whitespace-only section_id. It decodes fine (the
			// field is present), trims to "", and produces no edge — the
			// behavior the pre-typing raw path had, preserved deliberately so
			// the projected node identity stays byte-identical.
			documentationFamilyMention(map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        "   ",
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"candidate_refs": []map[string]any{
					{"id": "func:payments.Settle", "kind": "function"},
				},
			}),
		},
	}
	return CatalogOdu{
		Odu: odu,
		Detail: "one documentation section with two resolvable entity mentions " +
			"(function + table) deriving exactly two MENTIONS edges, plus five " +
			"mentions that must derive none: a duplicate section->target pair, " +
			"an ambiguous resolution, a two-candidate 'exact', a service-kind " +
			"target, and a whitespace-only section_id",
	}
}

// documentationFamilyMention builds one documentation_entity_mention envelope
// in this fixture's scope and generation.
func documentationFamilyMention(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID:      documentationFamilyScopeID,
		GenerationID: documentationFamilyGenerationID,
		FactKind:     facts.DocumentationEntityMentionFactKind,
		Payload:      payload,
	}
}
