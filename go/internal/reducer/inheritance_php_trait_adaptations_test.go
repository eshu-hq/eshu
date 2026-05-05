package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestInheritanceTraitOverrideTargetsParsesNamespacedMultiTargetInsteadof(t *testing.T) {
	t.Parallel()

	got := inheritanceTraitOverrideTargets(`Vendor\Features\Auditable::record insteadof Vendor\Features\Loggable, Vendor\Legacy\Traceable`)

	want := []string{"Loggable", "Traceable"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d; got=%v", len(got), len(want), got)
	}
	for i, target := range want {
		if got[i] != target {
			t.Fatalf("got[%d] = %q, want %q (got=%v)", i, got[i], target, got)
		}
	}
}

func TestInheritanceTraitAliasTargetsParsesNamespacedAliasClause(t *testing.T) {
	t.Parallel()

	got := inheritanceTraitAliasTargets(`Vendor\Features\Loggable::record as private logRecord`)

	want := []string{"Loggable"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d; got=%v", len(got), len(want), got)
	}
	for i, target := range want {
		if got[i] != target {
			t.Fatalf("got[%d] = %q, want %q (got=%v)", i, got[i], target, got)
		}
	}
}

func TestInheritanceTraitAliasMappingParsesTraitMethodAndAlias(t *testing.T) {
	t.Parallel()

	got, ok := inheritanceTraitAliasMapping(`Vendor\Features\Loggable::record as private logRecord`)
	if !ok {
		t.Fatalf("inheritanceTraitAliasMapping() ok = false, want true")
	}

	if got.TraitName != "Loggable" {
		t.Fatalf("TraitName = %q, want %q", got.TraitName, "Loggable")
	}
	if got.SourceMethodName != "record" {
		t.Fatalf("SourceMethodName = %q, want %q", got.SourceMethodName, "record")
	}
	if got.AliasMethodName != "logRecord" {
		t.Fatalf("AliasMethodName = %q, want %q", got.AliasMethodName, "logRecord")
	}
}

func TestExtractInheritanceRowsMaterializesPHPTraitAdaptationOverrides(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-php",
				"entity_id":   "content-entity:loggable",
				"entity_type": "Trait",
				"entity_name": "Loggable",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-php",
				"entity_id":   "content-entity:auditable",
				"entity_type": "Trait",
				"entity_name": "Auditable",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-php",
				"entity_id":   "content-entity:traceable",
				"entity_type": "Trait",
				"entity_name": "Traceable",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-php",
				"entity_id":   "content-entity:child",
				"entity_type": "Class",
				"entity_name": "Child",
				"entity_metadata": map[string]any{
					"bases": []any{"Loggable", "Auditable"},
					"trait_adaptations": []any{
						`Vendor\Features\Auditable::record insteadof Vendor\Features\Loggable, Vendor\Legacy\Traceable`,
						"Loggable::record as private logRecord",
					},
				},
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-php",
				"entity_id":   "content-entity:child-log-record",
				"entity_type": "Function",
				"entity_name": "logRecord",
				"entity_metadata": map[string]any{
					"class_context": "Child",
				},
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-php",
				"entity_id":   "content-entity:loggable-record",
				"entity_type": "Function",
				"entity_name": "record",
				"entity_metadata": map[string]any{
					"class_context": "Loggable",
				},
			},
		},
	}

	repoIDs, rows := ExtractInheritanceRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-php" {
		t.Fatalf("repoIDs = %v, want [repo-php]", repoIDs)
	}

	var inheritsCount, overridesCount, aliasesCount int
	var methodAliasFound bool
	for _, row := range rows {
		switch row["relationship_type"] {
		case "INHERITS":
			inheritsCount++
		case "OVERRIDES":
			overridesCount++
			if got, want := row["parent_entity_id"], "content-entity:loggable"; got != want && got != "content-entity:traceable" {
				t.Fatalf("override parent_entity_id = %#v, want loggable or traceable", got)
			}
		case "ALIASES":
			aliasesCount++
			parentID := row["parent_entity_id"]
			childID := row["child_entity_id"]
			if parentID == "content-entity:loggable-record" || childID == "content-entity:child-log-record" {
				if childID != "content-entity:child-log-record" {
					t.Fatalf("method alias child_entity_id = %#v, want %#v", childID, "content-entity:child-log-record")
				}
				if parentID != "content-entity:loggable-record" {
					t.Fatalf("method alias parent_entity_id = %#v, want %#v", parentID, "content-entity:loggable-record")
				}
				methodAliasFound = true
				continue
			}
			if got, want := parentID, "content-entity:loggable"; got != want {
				t.Fatalf("alias parent_entity_id = %#v, want %#v", got, want)
			}
		}
	}

	if got, want := inheritsCount, 2; got != want {
		t.Fatalf("inheritsCount = %d, want %d", got, want)
	}
	if got, want := overridesCount, 2; got != want {
		t.Fatalf("overridesCount = %d, want %d", got, want)
	}
	if got, want := aliasesCount, 2; got != want {
		t.Fatalf("aliasesCount = %d, want %d", got, want)
	}
	if !methodAliasFound {
		t.Fatalf("method-level alias row not found in rows=%#v", rows)
	}
}
