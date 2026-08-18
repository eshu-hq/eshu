// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
	codeownersv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codeowners/v1"
)

const (
	codeownersFamilyGenerationID = "gen-ifa-codeowners-family-1"
	codeownersFamilyRepoID       = "repo-ifa-codeowners-family"
)

// codeownersFamilyOdu returns the binary-portable catalog representation of
// the codeowners_ownership_edges cassette (#5992, under the #5543 umbrella).
// The checked-in cassette remains the live replay source;
// TestCodeownersFamilyIsCatalogedAndResolvable deeply pins this compiled
// literal to the same strict projection.
//
// Every DECLARES_CODEOWNER edge this Odù derives is exercised for a distinct
// reason, because the expected-edge-set fixture is asserted as an EXACT set:
// a silently-included edge fails as loudly as a missing one.
//
//   - RULE A (pattern "*.md", order_index 0) and RULE B (pattern "docs/**",
//     order_index 2) both name @org/docs: two DISTINCT DECLARES_CODEOWNER
//     relationships between the SAME (repo, team) pair, exercising the
//     relationship-MERGE-key-on-{pattern,source_path} shape
//     canonical_codeowners_edges.go documents. A triple-only edge identity
//     collapses these two onto one key; the declared pattern/source_path
//     identity (#6137) keeps them apart, which is what lets a dropped rule
//     stay visible when an unrelated duplicate masks the count. See
//     TestExpectedEdgeKeyDistinguishesADroppedRuleFromAnUnrelatedDuplicate,
//     which proves that on these exact rules.
//   - RULE C (pattern "*.go", order_index 1) names TWO owners (@org/backend
//     and @org/docs): one codeowners.ownership fact must project N edges for
//     its N owner tokens, and its @org/docs owner is a THIRD edge to the same
//     team RULE A/B already own — proving the guard's multiset comparison
//     (not a plain set) is load-bearing.
//   - RULE D repeats RULE A's exact (repo, source_path, pattern, owner) key
//     at a LATER order_index (5):
//     ExtractCodeownersOwnershipEdgeRowsWithQuarantine must collapse it into
//     RULE A's row (keeping the higher order_index), never emit a second
//     edge — proving the last-match-wins dedup
//     (codeowners_ownership_materialization.go's rowIndexByKey path).
//   - RULE E (pattern "*.py", a DIFFERENT CODEOWNERS location:
//     "docs/CODEOWNERS") names @org/infra: proves source_path, not just
//     pattern, is part of a rule's identity, and that a second CODEOWNERS
//     file in the same repository is honored independently of the first.
//
// Total: 5 DECLARES_CODEOWNER edges -- 3 to @org/docs (RULE A, B, C),
// 1 to @org/backend (RULE C), 1 to @org/infra (RULE E).
func codeownersFamilyOdu() CatalogOdu {
	factsForOdu := []facts.Envelope{
		codeownersFamilyRepositoryFact(),
		// RULE A.
		codeownersFamilyOwnershipFact(codeownersv1.Ownership{
			RepoID: codeownersFamilyRepoID, SourcePath: ".github/CODEOWNERS", Pattern: "*.md",
			Owners: []string{"@org/docs"}, OrderIndex: 0,
		}, "rule-a"),
		// RULE C.
		codeownersFamilyOwnershipFact(codeownersv1.Ownership{
			RepoID: codeownersFamilyRepoID, SourcePath: ".github/CODEOWNERS", Pattern: "*.go",
			Owners: []string{"@org/backend", "@org/docs"}, OrderIndex: 1,
		}, "rule-c"),
		// RULE B.
		codeownersFamilyOwnershipFact(codeownersv1.Ownership{
			RepoID: codeownersFamilyRepoID, SourcePath: ".github/CODEOWNERS", Pattern: "docs/**",
			Owners: []string{"@org/docs"}, OrderIndex: 2,
		}, "rule-b"),
		// RULE D: last-match-wins duplicate of RULE A.
		codeownersFamilyOwnershipFact(codeownersv1.Ownership{
			RepoID: codeownersFamilyRepoID, SourcePath: ".github/CODEOWNERS", Pattern: "*.md",
			Owners: []string{"@org/docs"}, OrderIndex: 5,
		}, "rule-d"),
		// RULE E: a second CODEOWNERS file.
		codeownersFamilyOwnershipFact(codeownersv1.Ownership{
			RepoID: codeownersFamilyRepoID, SourcePath: "docs/CODEOWNERS", Pattern: "*.py",
			Owners: []string{"@org/infra"}, OrderIndex: 0,
		}, "rule-e"),
		codeownersFamilySharedFollowupFact(),
	}
	return CatalogOdu{
		Odu: Odu{Name: codeownersFamilyOduName, Facts: factsForOdu},
		Detail: "one repository with two CODEOWNERS files: a same-team multi-pattern " +
			"collision (RULE A/B), a multi-owner rule adding a third edge to that same " +
			"team (RULE C), a last-match-wins duplicate rule (RULE D), and a " +
			"second-file distinct-team rule (RULE E) -- five DECLARES_CODEOWNER edges total",
	}
}

// codeownersFamilySharedFollowupFact builds the shared_followup envelope that
// enqueues this family's reducer work item. WITHOUT IT THE LIVE GATES CANNOT
// DRIVE THIS HANDLER AT ALL, and that was measured, not reasoned (#5992):
// driving the committed cassette against a live stack logged
// `"stage":"build_projection","fact_count":6,"reducer_intent_count":0` and left
// fact_work_items holding only a projector-stage row, so no
// `domain='codeowners_ownership'` row ever existed to claim, kill, or assert on.
//
// The enqueue path is this fact, not the registry metadata. `codeowners.ownership`
// does declare `ReducerDomain: "codeowners_ownership"` in
// go/internal/facts/fact_kind_registry.generated.go, but that field is read only
// by the registry generator/validator, the MCP disclosure ledger, and
// replaycoverage's depth requirements -- never by the projector. The projector's
// own fan-out (appendScopeGenerationReducerIntents,
// go/internal/projector/scope_generation_intents.go) has 36 build*ReducerIntent
// probes and no codeowners one; neither does documentation, which is the point:
// both families are enqueued by a shared_followup fact instead.
//
// In production that fact comes from the git collector
// (go/internal/collector/git_followup_facts.go), which emits one per follow-up
// domain and DOES include codeowners_ownership -- so this is a fixture gap, not
// a production defect. The committed Odù simply omitted the fact its own live
// proof depends on, which is why every cell built on the old two-stage reading
// of this pipeline was unpassable regardless of what it locked.
func codeownersFamilySharedFollowupFact() facts.Envelope {
	return facts.Envelope{
		ScopeID:          "scope-ifa-codeowners-family",
		GenerationID:     codeownersFamilyGenerationID,
		FactKind:         "shared_followup",
		StableFactKey:    "shared_followup:" + codeownersFamilyRepoID + ":codeowners_ownership",
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: "observed",
		Payload: map[string]any{
			"reducer_domain": "codeowners_ownership",
			"entity_key":     "codeowners:" + codeownersFamilyRepoID,
			"reason":         "repository snapshot emitted codeowners ownership follow-up",
			"repo_id":        codeownersFamilyRepoID,
		},
	}
}

// codeownersFamilyRepositoryFact builds the typed "repository" fact the
// codeowners.ownership facts below are scoped against.
func codeownersFamilyRepositoryFact() facts.Envelope {
	sourceRunID := "run-ifa-codeowners-family-1"
	localPath := "/repo-codeowners"
	repository := codegraphv1.Repository{
		RepoID: codeownersFamilyRepoID, SourceRunID: &sourceRunID, LocalPath: &localPath,
	}
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode codeowners catalog repository %q: %v", codeownersFamilyRepoID, err))
	}
	return facts.Envelope{
		ScopeID: "scope-ifa-codeowners-family", GenerationID: codeownersFamilyGenerationID,
		FactKind: factschema.FactKindCodegraphRepository, StableFactKey: "repository:" + codeownersFamilyRepoID,
		SchemaVersion: "1.0.0", CollectorKind: "git", SourceConfidence: "observed", Payload: payload,
	}
}

// codeownersFamilyOwnershipFact builds one typed "codeowners.ownership" fact.
// ruleLabel feeds the stable_fact_key so each of the five rules has its own
// stable identity even though RULE A and RULE D otherwise share every field
// but order_index.
func codeownersFamilyOwnershipFact(ownership codeownersv1.Ownership, ruleLabel string) facts.Envelope {
	payload, err := factschema.EncodeCodeownersOwnership(ownership)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode codeowners catalog ownership %q/%q: %v", ownership.SourcePath, ownership.Pattern, err))
	}
	return facts.Envelope{
		ScopeID: "scope-ifa-codeowners-family", GenerationID: codeownersFamilyGenerationID,
		FactKind:         factschema.FactKindCodeownersOwnership,
		StableFactKey:    "codeowners.ownership:" + codeownersFamilyRepoID + ":" + ruleLabel,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: "observed",
		Payload:          payload,
	}
}
