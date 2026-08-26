// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// canonicalEntityPhaseSkipOwners is the ledger of every label that
// entityTypeLabelMap registers and that canonical phase E (extractEntities)
// deliberately refuses to turn into an EntityRow, mapped to the writer that
// covers it instead. An empty value means NOTHING writes the label from a
// source-local repo generation.
//
// This ledger exists because registration and reachability are different
// things, and the tree already had one label where a reader could not tell them
// apart (#6206). Three hand-maintained registries name "variables"/"Variable"
// -- contentEntityBuckets, the collector's snapshotEntityBuckets twin, and
// entityTypeLabelMap here -- and the #5531 bucket-sync gate enforces that they
// agree. Agreement was read as reachability. It is not: phase E skips Variable,
// no other source-local phase picks it up, and a live golden-corpus run
// measured (Variable) count=0 with no Variable key in graph.node_counts at all
// (REPORTED, carried from #5156). Nothing failed, because nothing was counting.
// That zero is corpus-specific, not proof the label is unreachable: the golden
// corpus stages no Elixir or TSX fixture (scripts/lib/golden-corpus-fixtures.sh),
// and those are the only two shapes the reducer's semantic-entity path accepts
// as a Variable.
//
// The rule for editing this map: an entry with an owner is a label written by a
// different phase, and the owner string must name a writer the tests below
// actually exercise -- adding an owner without a matching
// canonicalEntityPhaseSkipProbes entry fails. An entry with no owner is a label
// the graph never gets.
// Adding one is a decision to drop a registered label on the floor; removing
// Variable's is a decision to re-enable plain-Variable source-local projection,
// which is a projected-truth change needing golden-corpus proof and a B-12
// snapshot update. Either way it moves deliberately, in a diff a reviewer sees.
var canonicalEntityPhaseSkipOwners = map[string]string{
	"Module": "canonical phase F, extractModulesFromEntities: Module MERGEs on " +
		"(name, language), not uid, so phase E would violate its constraint",
	"Parameter": "canonical phase G, extractRelationships over param_name facts: " +
		"Parameter MERGEs on a composite function-scoped key, not uid",
	// No owner on purpose, and "owner" here means SOURCE-LOCAL owner. See
	// go/internal/content/shape/materialize_tables.go and
	// go/internal/storage/cypher/evidence-5156-variable-semantic-owned.md.
	// Plain source Variables stay in the Postgres content/search surface. The
	// reducer's semantic-entity path does write Variable nodes -- for Elixir
	// module attributes and TSX component-type assertions, under
	// evidence_source='parser/semantic-entities' -- and it reaches them from
	// ordinary filesystem parsing of .ex/.tsx files. That path is reducer-owned
	// rather than source-local, which is why it is not an owner in this ledger,
	// NOT because it never runs.
	"Variable": "",
}

// TestCanonicalPhaseESkipsAreDeclared sweeps every label the projector
// registers through the real phase E extractor and reconciles the labels that
// produce no row against the ledger above.
//
// The sweep drives extractEntities rather than reading its source, so a skip
// added anywhere in that function -- a new label branch, a new payload
// precondition, an early return -- is caught the same way the two hardcoded
// ones are. The entity_type is spelled as the PascalCase label because that is
// what production sends: content/shape's materializeEntities stamps
// EntityRecord.EntityType from the bucket LABEL, and EntityTypeLabel accepts a
// label value as well as a snake_case key.
func TestCanonicalPhaseESkipsAreDeclared(t *testing.T) {
	t.Parallel()

	registered := make(map[string]struct{})
	skipped := make(map[string]struct{})
	for _, label := range sortedLabelSet(EntityTypeLabelMap()) {
		registered[label] = struct{}{}
		rows := ExtractEntityRows(
			[]facts.Envelope{contentEntityEnvelopeForLabel(label)},
			"repo-abc",
			"/repos/my-project",
		)
		if len(rows) == 0 {
			skipped[label] = struct{}{}
			continue
		}
		if rows[0].Label != label {
			t.Errorf("phase E turned entity_type %q into label %q", label, rows[0].Label)
		}
	}

	for label := range skipped {
		if _, declared := canonicalEntityPhaseSkipOwners[label]; declared {
			continue
		}
		t.Errorf("canonical phase E drops registered label %q and the skip is undeclared: the label is in "+
			"entityTypeLabelMap, so every registry check passes, and the node is still never written. "+
			"Add it to canonicalEntityPhaseSkipOwners with the writer that covers it, or with an empty "+
			"owner if nothing does.", label)
	}

	for _, label := range sortedKeys(canonicalEntityPhaseSkipOwners) {
		if _, ok := skipped[label]; ok {
			continue
		}
		// Two different edits leave a ledger entry with no matching skip, and
		// they want opposite fixes. Saying "phase E writes it now" about a
		// label that was deleted from entityTypeLabelMap states a cause the
		// sweep never observed -- the sweep never tried the label at all.
		if _, stillRegistered := registered[label]; !stillRegistered {
			t.Errorf("canonicalEntityPhaseSkipOwners[%q] is stale: entityTypeLabelMap no longer registers "+
				"that label, so phase E never sees it and this entry describes nothing. Delete the entry, "+
				"or restore the registration if dropping it was the accident.", label)
			continue
		}
		t.Errorf("canonicalEntityPhaseSkipOwners[%q] is stale: phase E writes that label now. Delete the "+
			"entry, or the next real skip with that name is silently licensed.", label)
	}
}

// TestCanonicalEntityLabelsWithNoSourceLocalWriterAreExactlyVariable is the pin
// #6206 asks for: the set of registered labels that no source-local writer
// materializes is named, and moving it takes an edit here.
//
// "Source-local" is load-bearing, and the failure text below repeats it rather
// than saying "no graph writer". SemanticEntityWriter
// (go/internal/storage/cypher/semantic_entity.go) does write Variable nodes,
// for Elixir module attributes and TSX component-type assertions, and it does
// so for repos read straight off the filesystem: the .ex parser stamps
// attribute_kind=module_attribute and the .tsx parser stamps
// component_type_assertion on rows in this same variables bucket. That path is
// reducer-owned, not source-local, which is the only reason Variable sits here
// with an empty owner. A reader who trusted an unqualified "no graph writer"
// could delete that writer's Variable support as dead code.
//
// Variable is the whole set today. If this test fails with the set grown, a
// registered label was quietly stranded. If it fails with the set shrunk to
// empty, plain-Variable source-local projection was re-enabled -- which is a
// projected-truth change: it needs golden-corpus evidence and, per #6183, makes
// a B-12 snapshot pin possible for the first time.
func TestCanonicalEntityLabelsWithNoSourceLocalWriterAreExactlyVariable(t *testing.T) {
	t.Parallel()

	var unwritten []string
	for label, owner := range canonicalEntityPhaseSkipOwners {
		if owner == "" {
			unwritten = append(unwritten, label)
		}
	}
	sort.Strings(unwritten)

	want := []string{"Variable"}
	if len(unwritten) != len(want) {
		t.Fatalf("registered labels with no source-local graph writer = %v, want %v", unwritten, want)
	}
	for i := range want {
		if unwritten[i] != want[i] {
			t.Fatalf("registered labels with no source-local graph writer = %v, want %v", unwritten, want)
		}
	}
}

// canonicalEntityPhaseSkipProbes holds one assertion per ledger entry that
// names an owner. Each probe drives the phase that owner points at, so it fails
// the moment that phase stops producing the label.
//
// This is a map rather than two hardcoded subtests because the test below pairs
// it against canonicalEntityPhaseSkipOwners in BOTH directions: an owner with
// no probe fails, and a probe with no owner fails. Without that pairing a
// second stranded label could be handed a plausible-looking owner -- "canonical
// phase Z, extractTraitsFromNowhere" -- and the ledger would read exactly as
// honest as it does today while nothing ran. That is #6206's own defect class,
// a hand-maintained list read as truth, one layer down.
//
// Residual, stated rather than asserted away: the pairing is by LABEL. A probe
// is machine-checked to exist and to pass, but nothing checks that the function
// it calls is the phase the owner STRING names. An owner rewritten to point at
// a different phase while its probe keeps driving the old one is not caught
// here. That is the same residual nonBucketProjectorLabels documents for its
// source strings in
// go/internal/content/shape/bucket_sync_projector_orphans_test.go; the gate on
// it is that a reviewer reads both halves in the same diff.
var canonicalEntityPhaseSkipProbes = map[string]func(*testing.T){
	"Module": func(t *testing.T) {
		rows := extractModulesFromEntities([]facts.Envelope{contentEntityEnvelopeForLabel("Module")})
		if len(rows) != 1 {
			t.Fatalf("extractModulesFromEntities returned %d rows for a Module content entity, want 1", len(rows))
		}
		if rows[0].Name != "entity-for-Module" {
			t.Fatalf("ModuleRow.Name = %q, want %q", rows[0].Name, "entity-for-Module")
		}
	},
	"Parameter": func(t *testing.T) {
		// The distinction is payload SHAPE, not fact kind. This envelope is a
		// content_entity fact and it DOES yield a Parameter row, because
		// extractRelationships keys on the param_name payload field and never
		// filters on FactKind (canonical_builder.go, "Parameters: facts with
		// param_name payload key"). What would prove nothing is sending the
		// entity_type/entity_name shape contentEntityEnvelopeForLabel builds,
		// since phase G reads param_name and that shape carries none.
		//
		// An earlier version of this comment said Parameter rows never come
		// from a content_entity fact at all, which the envelope directly below
		// it contradicts — the same "registration read as truth" imprecision
		// this file exists to close.
		mat, _ := buildCanonicalMaterialization(testScope(), testGeneration(), []facts.Envelope{{
			FactID:   "param-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"param_name":    "timeout",
				"function_name": "handler",
				"relative_path": "src/handler.go",
				"function_line": 10,
			},
		}})
		if len(mat.Parameters) != 1 {
			t.Fatalf("buildCanonicalMaterialization produced %d ParameterRows, want 1", len(mat.Parameters))
		}
		if mat.Parameters[0].ParamName != "timeout" {
			t.Fatalf("ParameterRow.ParamName = %q, want %q", mat.Parameters[0].ParamName, "timeout")
		}
	},
}

// TestCanonicalEntityPhaseSkipOwnersActuallyWrite keeps the ledger's owner
// strings from becoming a comment that outlived its writer. Every non-empty
// owner is exercised against the phase it names, so "Module is covered by phase
// F" fails the moment phase F stops covering it -- and because the probes are
// driven FROM the ledger rather than hardcoded beside it, a new entry with an
// owner and no probe fails too. Otherwise the ledger could claim coverage for a
// second stranded label and read exactly as honest as it does today.
func TestCanonicalEntityPhaseSkipOwnersActuallyWrite(t *testing.T) {
	t.Parallel()

	for _, label := range sortedKeys(canonicalEntityPhaseSkipOwners) {
		probe, hasProbe := canonicalEntityPhaseSkipProbes[label]

		if canonicalEntityPhaseSkipOwners[label] == "" {
			if hasProbe {
				t.Errorf("canonicalEntityPhaseSkipOwners[%q] has no owner and canonicalEntityPhaseSkipProbes "+
					"has a probe for it. The two disagree about whether anything writes the label: drop the "+
					"probe, or give the entry the owner the probe proves.", label)
			}
			continue
		}

		if !hasProbe {
			t.Errorf("canonicalEntityPhaseSkipOwners[%q] names an owner and no probe in "+
				"canonicalEntityPhaseSkipProbes exercises it. Phase E drops the label, the owner string "+
				"asserts another phase picks it up, and nothing runs that assertion -- which is exactly how "+
				"a stranded label acquires a plausible owner and passes. Add a probe that drives the named "+
				"phase, or set the owner empty and accept that the label is never written.", label)
			continue
		}

		t.Run(label, func(t *testing.T) {
			t.Parallel()

			probe(t)
		})
	}

	for _, label := range sortedKeys(canonicalEntityPhaseSkipProbes) {
		if _, declared := canonicalEntityPhaseSkipOwners[label]; !declared {
			t.Errorf("canonicalEntityPhaseSkipProbes[%q] has no canonicalEntityPhaseSkipOwners entry: phase E "+
				"does not skip that label any more, so the probe guards nothing and would silently absorb a "+
				"future skip of the same name.", label)
		}
	}
}

// TestTerraformVariableReachesTheGraph is the sibling-label check #6206 raises.
//
// terraform_variables/TerraformVariable sits three rows below variables/Variable
// in the same bucket table, and the two names invite the assumption that they
// share a fate. They do not: phase E has no TerraformVariable branch, so the row
// lands in CanonicalMaterialization.Entities and the canonical node writer
// MERGEs it by label (canonicalEntityRowsByLabel, storage/cypher). The sweep
// above already covers this, but the assertion is spelled out so the answer is
// findable by name rather than by inference from an absent ledger entry.
func TestTerraformVariableReachesTheGraph(t *testing.T) {
	t.Parallel()

	rows := ExtractEntityRows(
		[]facts.Envelope{contentEntityEnvelopeForLabel("TerraformVariable")},
		"repo-abc",
		"/repos/my-project",
	)
	if len(rows) != 1 {
		t.Fatalf("phase E produced %d rows for a TerraformVariable content entity, want 1", len(rows))
	}
	if rows[0].Label != "TerraformVariable" {
		t.Fatalf("EntityRow.Label = %q, want %q", rows[0].Label, "TerraformVariable")
	}
}

// contentEntityEnvelopeForLabel builds the content_entity fact the collector
// emits for one entity label, with every payload field phase E reads populated
// so a missing row means the label was skipped, not that the fixture was thin.
func contentEntityEnvelopeForLabel(label string) facts.Envelope {
	return facts.Envelope{
		FactID:   "entity-" + label,
		ScopeID:  "scope-1",
		FactKind: "content_entity",
		Payload: map[string]any{
			"entity_id":     "id-" + label,
			"entity_type":   label,
			"entity_name":   "entity-for-" + label,
			"relative_path": "src/thing.txt",
			"start_line":    1,
			"end_line":      2,
			"language":      "go",
			"repo_id":       "repo-abc",
		},
	}
}

// sortedLabelSet returns the deduplicated label values of an entity-type map in
// a stable order. Several entity types share one label (terraform_moved and
// terraform_moved_block both name TerraformMovedBlock), and the sweep asserts
// per label, not per key.
func sortedLabelSet(m map[string]string) []string {
	set := make(map[string]struct{}, len(m))
	for _, label := range m {
		set[label] = struct{}{}
	}
	return sortedKeys(set)
}

// sortedKeys returns a map's keys in a stable order so failures list the same
// labels in the same sequence on every run.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
