// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The rationale_edges family Odù (#5998, under the #5543 umbrella).
//
// ExtractRationaleEdgeRows projects one Rationale node and one EXPLAINS edge
// per distinct (entity, comment kind, comment text) carried in a content
// entity's parser-emitted rationale_comments. The fixture below exercises every
// clause that decides whether an edge exists, because an Odù carrying only
// well-formed comments proves the extractor emits edges without proving it
// emits the right ones — and the set is asserted exactly, so an edge nobody
// derived fails as loudly as a missing one.
const (
	rationaleFamilyOduName      = "odu:ifa-rationale-family"
	rationaleFamilyScopeID      = "scope-ifa-rationale-family"
	rationaleFamilyGenerationID = "gen-1"
	rationaleFamilyRepoID       = "repo-ifa-rationale"
)

// rationaleFamilyOdu carries two content entities whose comments derive exactly
// three EXPLAINS edges, plus seven inputs that must derive none.
//
// The negatives map one-to-one onto the extractor's early returns, so deleting
// any single guard turns the exact-set assertion red rather than passing with a
// quietly larger graph:
//
//   - a non-content_entity fact kind      (only content entities carry comments)
//   - a tombstoned content entity         (a retraction is not evidence)
//   - a content entity with no repo_id    (an edge with no repo has no scope)
//   - a content entity with a blank entity_id
//     (the edge would have no target identity; guarded in the same condition
//     as repo_id, so a regression to checking only the repo would project it)
//   - a comment with a blank kind         (kind is half the node identity)
//   - a comment whose text is whitespace  (trims to empty, so no excerpt)
//   - a repeated (entity, kind, text)     (the seen[rationaleUID] dedup)
//
// Two positives are deliberately near-identical: the same comment text under
// two different kinds. They share an excerpt hash but differ in kind, so they
// must produce TWO edges. That pins the kind into the node identity — a UID
// built from entity+hash alone would collapse them and the count would drop.
func rationaleFamilyOdu() CatalogOdu {
	const (
		chargeText  = "Retries are capped at three to bound tail latency."
		invoiceText = "Invoices are immutable once issued."
	)
	odu := Odu{
		Name: rationaleFamilyOduName,
		Facts: []facts.Envelope{
			// EDGES 1 and 2: same text, two kinds, from the top-level key.
			// EDGE 3's entity uses the entity_metadata fallback instead.
			rationaleFamilyContentEntity("func:payments.Charge", "services/payments/charge.go", map[string]any{
				"rationale_comments": []map[string]any{
					{"kind": "why", "text": chargeText},
					{"kind": "caveat", "text": chargeText},
					// NOT AN EDGE: identical (entity, kind, text) as the first
					// comment. A parser re-emitting the same comment must not
					// double the graph.
					{"kind": "why", "text": chargeText},
					// NOT AN EDGE: kind is half the Rationale node's identity,
					// so a blank one cannot address a node.
					{"kind": "", "text": "A comment with no kind."},
					// NOT AN EDGE: whitespace-only text trims to empty, so
					// there is no excerpt to hash.
					{"kind": "why", "text": "   "},
				},
			}),
			// EDGE 3: comments carried under entity_metadata rather than at the
			// top level. rationalePayloadComments falls back to that nesting,
			// and a fixture that only used the top-level key would leave the
			// fallback branch unproven.
			rationaleFamilyContentEntity("func:billing.Invoice", "services/billing/invoice.go", map[string]any{
				"entity_metadata": map[string]any{
					"rationale_comments": []map[string]any{
						{"kind": "why", "text": invoiceText},
					},
				},
			}),
			// NOT AN EDGE: tombstoned. A retraction is not evidence for a new
			// edge, however well-formed its comments are.
			rationaleFamilyTombstone("func:payments.Refund", "services/payments/refund.go", map[string]any{
				"rationale_comments": []map[string]any{
					{"kind": "why", "text": "Refunds are idempotent by request id."},
				},
			}),
			// NOT AN EDGE: no repo_id. The edge would have no repository scope
			// to belong to.
			{
				ScopeID:      rationaleFamilyScopeID,
				GenerationID: rationaleFamilyGenerationID,
				FactKind:     "content_entity",
				Payload: map[string]any{
					"entity_id":     "func:orphan.NoRepo",
					"relative_path": "services/orphan/no_repo.go",
					"rationale_comments": []map[string]any{
						{"kind": "why", "text": "This entity names no repository."},
					},
				},
			},
			// NOT AN EDGE: blank entity_id. entity_id is the edge's target and
			// half the Rationale UID, so an edge without one has no target
			// identity at all. Kept distinct from the missing-repo_id case
			// because the extractor guards them in one condition
			// (entityID == "" || repoID == ""): if that regressed to checking
			// only the repository, an edge with an empty target would project
			// and a fixture testing only the repo half would stay green.
			{
				ScopeID:      rationaleFamilyScopeID,
				GenerationID: rationaleFamilyGenerationID,
				FactKind:     "content_entity",
				Payload: map[string]any{
					"repo_id":       rationaleFamilyRepoID,
					"entity_id":     "",
					"relative_path": "services/orphan/no_entity.go",
					"rationale_comments": []map[string]any{
						{"kind": "why", "text": "This content entity names no target."},
					},
				},
			},
			// NOT AN EDGE: wrong fact kind. Only content entities carry
			// parser-emitted rationale comments.
			{
				ScopeID:      rationaleFamilyScopeID,
				GenerationID: rationaleFamilyGenerationID,
				FactKind:     "repository",
				Payload: map[string]any{
					"repo_id":   rationaleFamilyRepoID,
					"entity_id": "repo:ifa-rationale",
					"rationale_comments": []map[string]any{
						{"kind": "why", "text": "A repository fact is not a content entity."},
					},
				},
			},
		},
	}
	return CatalogOdu{
		Odu: odu,
		Detail: "two content entities whose rationale comments derive exactly " +
			"three EXPLAINS edges — including the same text under two kinds, and " +
			"one set reached through the entity_metadata fallback — plus seven " +
			"inputs deriving none: a repeated comment, a blank kind, " +
			"whitespace-only text, a tombstone, a missing repo_id, a blank " +
			"entity_id, and a non-content_entity fact kind",
	}
}

// rationaleFamilyContentEntity builds a live content_entity envelope carrying
// the given payload fields alongside this fixture's repo and entity identity.
func rationaleFamilyContentEntity(entityID, relativePath string, extra map[string]any) facts.Envelope {
	return rationaleFamilyEnvelope(entityID, relativePath, extra, false)
}

// rationaleFamilyTombstone builds the tombstoned variant of the same shape.
func rationaleFamilyTombstone(entityID, relativePath string, extra map[string]any) facts.Envelope {
	return rationaleFamilyEnvelope(entityID, relativePath, extra, true)
}

func rationaleFamilyEnvelope(entityID, relativePath string, extra map[string]any, tombstone bool) facts.Envelope {
	payload := map[string]any{
		"repo_id":       rationaleFamilyRepoID,
		"entity_id":     entityID,
		"relative_path": relativePath,
	}
	for k, v := range extra {
		payload[k] = v
	}
	return facts.Envelope{
		ScopeID:      rationaleFamilyScopeID,
		GenerationID: rationaleFamilyGenerationID,
		FactKind:     "content_entity",
		IsTombstone:  tombstone,
		Payload:      payload,
	}
}
