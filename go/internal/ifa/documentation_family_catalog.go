// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The documentation_edges family Odù (#5994, under the #5543 umbrella).
//
// documentationFamilyOdu is the binary-portable compiled catalog
// representation. This file projects the committed cassette
// (testdata/cassettes/documentation/ifa-documentation-family.json) through
// the same strict envelope boundary loadDocumentationFamilyOdu
// (documentation_family_odu.go) does, so TestDocumentationFamilyIsCatalogedAndResolvable
// can deeply compare the two representations and fail closed the moment a
// one-sided edit lets them drift.
//
// Both live gate scripts (scripts/verify-ifa-determinism.sh,
// scripts/verify-ifa-fault-injection.sh) drive this SAME cassette. The offline
// extractor guard (resolveDocumentationEdgeMaterializedEdges) and the live
// determinism/fault-injection matrices therefore assert the same committed
// bytes rather than maintaining parallel fixtures that can drift -- the
// #5994 review found exactly that drift: an earlier version of this Odù
// hand-invented mention targets ("func:payments.Charge",
// "sqltable:public.payments") that never matched a real graph uid, so the
// offline guard proved nothing about what the live gate actually persists.
const (
	documentationFamilyOduName      = "odu:ifa-documentation-family"
	documentationFamilyCassettePath = "testdata/cassettes/documentation/ifa-documentation-family.json"
	documentationFamilyScopeID      = "scope-ifa-documentation-family"
	documentationFamilyGenerationID = "gen-ifa-documentation-family-1"
	documentationFamilyRepoID       = "repo-ifa-documentation-family"
	documentationFamilyDocID        = "doc-platform-guide"
	documentationFamilySectionID    = "sec-overview"
)

// documentationFamilyOdu carries one document section whose mentions cover the
// extractor's full decision surface: three edges that must exist (a Function,
// a Class, and a SqlTable target -- the SqlTable case closes #5994's
// production-truth gap in canonical_documentation_edges.go's MATCH label
// alternation), and five mentions that must NOT produce one.
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
			documentationCatalogFact("repository", "repository:"+documentationFamilyRepoID, map[string]any{
				"repo_id":       documentationFamilyRepoID,
				"source_run_id": "run-ifa-documentation-family-1",
				"local_path":    "/" + documentationFamilyRepoID,
			}),
			documentationCatalogFact("file", "file:"+documentationFamilyRepoID+":app/payments.py", map[string]any{
				"repo_id":       documentationFamilyRepoID,
				"relative_path": "app/payments.py",
				"parsed_file_data": map[string]any{
					"path": "/" + documentationFamilyRepoID + "/app/payments.py",
				},
			}),
			documentationCatalogContentEntityFact("content-entity:e_17b9095db873", "Function", "Charge", "app/payments.py", 1, 10),
			documentationCatalogContentEntityFact("content-entity:e_6f013273c987", "Class", "PaymentGateway", "app/payments.py", 20, 40),
			documentationCatalogFact("file", "file:"+documentationFamilyRepoID+":db/schema.sql", map[string]any{
				"repo_id":       documentationFamilyRepoID,
				"relative_path": "db/schema.sql",
				"parsed_file_data": map[string]any{
					"path": "/" + documentationFamilyRepoID + "/db/schema.sql",
				},
			}),
			documentationCatalogFact("content_entity", "content_entity:content-entity:sql-tbl-payments", map[string]any{
				"repo_id":       documentationFamilyRepoID,
				"entity_id":     "content-entity:sql-tbl-payments",
				"entity_type":   "SqlTable",
				"entity_name":   "public.payments",
				"relative_path": "db/schema.sql",
			}),
			documentationCatalogFact("documentation_document", "documentation_document:"+documentationFamilyDocID, map[string]any{
				"document_id": documentationFamilyDocID,
				"title":       "Platform Guide",
			}),
			// EDGE 1: the ordinary resolved mention.
			documentationCatalogMention("1-charge", map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "function",
				"mention_text":      "Charge",
				"candidate_refs": []any{
					map[string]any{"id": "content-entity:e_17b9095db873", "kind": "function"},
				},
			}),
			// EDGE 2: a second target from the same section, proving the
			// section->target pair is the identity rather than the section.
			documentationCatalogMention("2-paymentgateway", map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "class",
				"mention_text":      "PaymentGateway",
				"candidate_refs": []any{
					map[string]any{"id": "content-entity:e_6f013273c987", "kind": "class"},
				},
			}),
			// NOT AN EDGE: same section, same target as EDGE 1, different
			// mention text. The dedup keys on section_uid->target_entity_id, so
			// a second mention of the same thing must not double the graph.
			documentationCatalogMention("3-duplicate-charge", map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "function",
				"mention_text":      "Charge()",
				"candidate_refs": []any{
					map[string]any{"id": "content-entity:e_17b9095db873", "kind": "function"},
				},
			}),
			// NOT AN EDGE: unresolved. An ambiguous mention names a candidate
			// but has not decided, and a guess is worse than no edge.
			documentationCatalogMention("4-ambiguous", map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": "ambiguous",
				"mention_kind":      "function",
				"mention_text":      "Refund",
				"candidate_refs": []any{
					map[string]any{"id": "content-entity:e_258daea46e5b", "kind": "function"},
				},
			}),
			// NOT AN EDGE: two candidates. "exact" with more than one candidate
			// is a contradiction the extractor refuses to resolve arbitrarily.
			documentationCatalogMention("5-multi-candidate", map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "function",
				"mention_text":      "Void",
				"candidate_refs": []any{
					map[string]any{"id": "content-entity:e_deadbeef0001", "kind": "function"},
					map[string]any{"id": "content-entity:e_deadbeef0002", "kind": "function"},
				},
			}),
			// NOT AN EDGE: target_kind "service" is excluded by an explicit
			// branch in the extractor.
			documentationCatalogMention("6-service-kind", map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "service",
				"mention_text":      "payments service",
				"candidate_refs": []any{
					map[string]any{"id": "service:payments", "kind": "service"},
				},
			}),
			// NOT AN EDGE: whitespace-only section_id. It decodes fine (the
			// field is present), trims to "", and produces no edge — the
			// behavior the pre-typing raw path had, preserved deliberately so
			// the projected node identity stays byte-identical.
			documentationCatalogMention("7-blank-section", map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        "   ",
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "function",
				"mention_text":      "Settle",
				"candidate_refs": []any{
					map[string]any{"id": "content-entity:e_deadbeef0003", "kind": "function"},
				},
			}),
			// EDGE 3: a SqlTable target, closing #5994's production-truth gap.
			documentationCatalogMention("8-payments-table", map[string]any{
				"document_id":       documentationFamilyDocID,
				"section_id":        documentationFamilySectionID,
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"mention_kind":      "table",
				"mention_text":      "payments table",
				"candidate_refs": []any{
					map[string]any{"id": "content-entity:sql-tbl-payments", "kind": "table"},
				},
			}),
			documentationCatalogFact("shared_followup", "shared_followup:"+documentationFamilyRepoID+":documentation_materialization", map[string]any{
				"reducer_domain": "documentation_materialization",
				"entity_key":     "repo:" + documentationFamilyRepoID,
				"reason":         "documentation source emitted documentation materialization follow-up",
				"repo_id":        documentationFamilyRepoID,
			}),
		},
	}
	return CatalogOdu{
		Odu: odu,
		Detail: "one documentation section with three resolvable entity mentions " +
			"(function, class, and table) deriving exactly three DOCUMENTS edges, " +
			"plus five mentions that must derive none: a duplicate section->target " +
			"pair, an ambiguous resolution, a two-candidate 'exact', a service-kind " +
			"target, and a whitespace-only section_id",
	}
}

// documentationCatalogFact builds one fact envelope in this fixture's scope
// and generation, mirroring codeCallCatalogFact's shape.
func documentationCatalogFact(kind, stableKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID: documentationFamilyScopeID, GenerationID: documentationFamilyGenerationID, FactKind: kind,
		StableFactKey: stableKey, SchemaVersion: "1.0.0", CollectorKind: "git",
		SourceConfidence: "observed", Payload: payload,
	}
}

// documentationCatalogMention builds one documentation_entity_mention envelope,
// keyed by the same "<n>-<label>" suffix the committed cassette's
// stable_fact_key uses. The key always uses documentationFamilySectionID, even
// for the one mention whose PAYLOAD section_id is whitespace-only: the
// committed cassette's stable_fact_key for that mention is
// "...:sec-overview:7-blank-section", not a key built from the blank payload
// value, since stable_fact_key is a fixture-authoring label, not a derived
// projection of the payload.
func documentationCatalogMention(stableKeySuffix string, payload map[string]any) facts.Envelope {
	return documentationCatalogFact(
		facts.DocumentationEntityMentionFactKind,
		"documentation_entity_mention:"+documentationFamilyDocID+":"+documentationFamilySectionID+":"+stableKeySuffix,
		payload,
	)
}

// documentationCatalogContentEntityFact builds one content_entity envelope for
// a code entity target, mirroring codeCallCatalogContentEntityFact. start_line
// and end_line are cast to float64: json.Unmarshal decodes every JSON number
// into a map[string]any as float64, and this literal must match that exactly
// for TestDocumentationFamilyIsCatalogedAndResolvable's reflect.DeepEqual
// comparison against the cassette-loaded Odù to hold.
func documentationCatalogContentEntityFact(id, entityType, name, relativePath string, startLine, endLine int) facts.Envelope {
	return documentationCatalogFact("content_entity", "content_entity:"+id, map[string]any{
		"repo_id": documentationFamilyRepoID, "entity_id": id, "entity_type": entityType,
		"entity_name": name, "relative_path": relativePath, "start_line": float64(startLine), "end_line": float64(endLine),
	})
}
