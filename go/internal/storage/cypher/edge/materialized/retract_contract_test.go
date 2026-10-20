// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	"regexp"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/iam"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/security/group"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
)

// relTypeAlternation pulls every [rel:A|B] alternative out of a Cypher template.
// The class admits lowercase because the observability family names
// AWS_COVERS_<signal> types whose signal suffix is lowercase by construction
// (canonicalObservabilityCoverageCypherType concatenates the label with the raw
// vocabulary member). Every pre-contract retract names uppercase-only types,
// so widening changes none of their extractions.
var relTypeAlternation = regexp.MustCompile(`\[(?:\w*):([A-Za-z_][A-Za-z_|]*)\]`)

// bareRelDelete matches a relationship delete with no named type (`-[rel]->`),
// the shape the #6228 split verdict rejects as a coverage basis: scoped only
// by evidence_source and scope, it reaps any edge type sharing the endpoints,
// including types another family owns.
var bareRelDelete = regexp.MustCompile(`-\[\s*\w*\s*\]->`)

// allHeldRetracts returns every production retract variant the entry holds:
// the primary RetractCypher plus RetractVariants. The drift guard checks all
// of them, so a ByUIDs or stale-generation variant edited without its
// alternation (or detach shape) turns the build red.
func allHeldRetracts(entry materializedEdgeFamily) []string {
	out := make([]string, 0, 1+len(entry.RetractVariants))
	out = append(out, entry.RetractCypher)
	out = append(out, entry.RetractVariants...)
	return out
}

// mergeNodeLabelsFromWriteCypher returns the distinct node labels MERGEd by a
// write template, read out of the held const itself. An evidence family MERGEs
// only its private evidence label (twice for incident routing, which MERGEs
// both endpoints under one label), so a disagreeing pair is a template bug,
// not a second label to track.
func mergeNodeLabelsFromWriteCypher(t *testing.T, family, cypher string) []string {
	t.Helper()
	matches := regexp.MustCompile(`MERGE\s*\(\s*\w+\s*:\s*([A-Za-z_][A-Za-z0-9_]*)\b`).FindAllStringSubmatch(cypher, -1)
	seen := map[string]struct{}{}
	var out []string
	for _, m := range matches {
		if _, dup := seen[m[1]]; !dup {
			seen[m[1]] = struct{}{}
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("family %q: write template MERGEs no labeled node; the detach-shape check cannot derive the evidence label:\n%s", family, cypher)
	}
	return out
}

// mergeRelTypeFromWriteCypher returns the single relationship type a write
// template MERGEs, read out of the held const itself. It is the derivation
// behind a single-type evidence family's declared edge type: the type comes
// from the template, never from a literal copied into the registry or test.
func mergeRelTypeFromWriteCypher(t *testing.T, family, cypher string) string {
	t.Helper()
	matches := regexp.MustCompile(`-\[\s*\w*\s*:\s*([A-Za-z_][A-Za-z0-9_]*)\s*\]->`).FindAllStringSubmatch(cypher, -1)
	if len(matches) != 1 {
		t.Fatalf("family %q: write template MERGEs %d relationship types, want exactly 1 for a single-type derivation:\n%s", family, len(matches), cypher)
	}
	return matches[0][1]
}

// checkRelAlternationRetract pins one held retract variant's named alternation
// against the family's declared edge types, in both directions. It is the
// pre-contract semantic, unchanged: several long-registered families scope
// their retract by a node repo_id rather than an edge scope_id, so predicate
// shape is not pinned here. Contract families pin theirs separately in
// checkClosedVocabularyPredicates. Violations come from the
// relAlternationRetractViolations seam; this wrapper only reports them.
func checkRelAlternationRetract(t *testing.T, family, variant string, entry materializedEdgeFamily) {
	t.Helper()
	for _, violation := range relAlternationRetractViolations(family, variant, entry.EdgeTypes) {
		t.Errorf("%s", violation)
	}
}

// checkClosedVocabularyPredicates pins what the #6228 split verdict's kind-C
// refactor must keep while naming the alternation: the evidence_source plus
// scope predicates on every held variant, no bare [rel] that would reap a
// foreign type sharing the endpoints and source, and no DETACH DELETE (node
// deletion belongs to evidenceNodeDetach families with a sole-creator
// census). It runs only over the multi-type direct table: every entry there
// is a contract family whose retract is held and exactly-scoped — an
// edge-scoped alternation or, for kind-A evidenceNodeDetach entries, a
// node-detach on a sole-creator label. Violations come from the
// closedVocabularyPredicateViolations seam, which reads only the variant;
// this wrapper only reports them.
func checkClosedVocabularyPredicates(t *testing.T, family, variant string) {
	t.Helper()
	for _, violation := range closedVocabularyPredicateViolations(variant) {
		t.Errorf("%s", violation)
	}
}

// checkEvidenceNodeDetachRetract pins one held retract variant's detach shape:
// MATCH on the evidence label derived from the held write template, DETACH
// DELETE of that node, and the evidence_source plus scope predicates that
// keep the delete to this writer's generation. The edge cascade needs no type
// alternation because the sole-creator census proves no other writer's edges
// hang off these nodes. Violations come from the
// evidenceNodeDetachRetractViolations seam; this wrapper only reports them.
func checkEvidenceNodeDetachRetract(t *testing.T, family, variant, evidenceLabel string) {
	t.Helper()
	for _, violation := range evidenceNodeDetachRetractViolations(family, variant, evidenceLabel) {
		t.Errorf("%s", violation)
	}
}

// deriveEvidenceNodeDetachTypes returns the relationship types an
// evidenceNodeDetach family owns, derived from held production consts and
// vocabulary maps — never from a copied literal. A family without a derivation
// arm fails closed here so a new detach family cannot register silently.
func deriveEvidenceNodeDetachTypes(t *testing.T, family string, entry materializedEdgeFamily) map[string]struct{} {
	t.Helper()
	switch family {
	case "code_taint_evidence":
		// Single static MERGE type: read the relationship type straight out
		// of the held write template.
		return map[string]struct{}{mergeRelTypeFromWriteCypher(t, family, entry.IdentityCypher): {}}
	case "authenticates_vault_role":
		// Single static MERGE type, but the writer splits node and edge
		// templates: IdentityCypher holds the role NODE upsert (the one
		// template that MERGEs the evidence label), so the edge type is read
		// off the held edge upsert instead. Both are held production consts
		// of the same writer, never copied literals.
		return map[string]struct{}{mergeRelTypeFromWriteCypher(t, family, iam.SecretsIAMAuthenticatesVaultRoleEdgeUpsertCypher): {}}
	case "uses_vault_policy":
		// Single static MERGE type, same split shape as
		// authenticates_vault_role: IdentityCypher holds the policy NODE
		// upsert, so the edge type is read off the held edge upsert
		// instead. Both are held production consts of the same writer,
		// never copied literals.
		return map[string]struct{}{mergeRelTypeFromWriteCypher(t, family, iam.SecretsIAMUsesVaultPolicyEdgeUpsertCypher): {}}
	case "incident_routing_evidence":
		// Parameterized MERGE type (%s): the write template names no type, so
		// the slot-to-type map the write path screens each row against is the
		// source, cross-checked against the slot vocabulary it is screened
		// by — a slot the map forgot (or one the vocabulary rejects) turns
		// the build red here rather than on a live gate.
		slotTypes := edgewriter.IncidentRoutingEvidenceRelationshipTypes()
		slots := edgewriter.IncidentRoutingEvidenceSlotVocabulary()
		var unmapped, unscreened []string
		for slot := range slots {
			if _, ok := slotTypes[slot]; !ok {
				unmapped = append(unmapped, slot)
			}
		}
		for slot := range slotTypes {
			if _, ok := slots[slot]; !ok {
				unscreened = append(unscreened, slot)
			}
		}
		sort.Strings(unmapped)
		sort.Strings(unscreened)
		if len(unmapped) > 0 {
			t.Fatalf("vocabulary slots %v have no relationship type; rows in those slots would fail validation while the registry claims the family", unmapped)
		}
		if len(unscreened) > 0 {
			t.Fatalf("slot-to-type map entries %v are outside the screened vocabulary; the write path rejects them while the registry claims them", unscreened)
		}
		out := make(map[string]struct{}, len(slotTypes))
		for _, relType := range slotTypes {
			out[relType] = struct{}{}
		}
		return out
	default:
		t.Fatalf("family %q: no EdgeTypes derivation for an evidenceNodeDetach family; add one reading the held write const and vocabulary map, never a copied literal", family)
		return nil
	}
}

// deriveEvidenceLabel returns the evidence node label from the held write
// template: every MERGEd node label must agree, so a template that MERGEs two
// different labels is a bug, not a second evidence kind.
func deriveEvidenceLabel(t *testing.T, family string, entry materializedEdgeFamily) string {
	t.Helper()
	labels := mergeNodeLabelsFromWriteCypher(t, family, entry.IdentityCypher)
	if len(labels) != 1 {
		t.Fatalf("family %q: write template MERGEs node labels %v, want exactly the one private evidence label", family, labels)
	}
	return labels[0]
}

// checkRegistryEntryMatchesItsRetract dispatches one registry entry on its
// RetractKind: alternation pinning for relAlternation families (every held
// variant), detach-shape pinning plus derived-type agreement for
// evidenceNodeDetach families. pinPredicates is true for the multi-type direct
// table, whose entries are all contract families with held, exactly-scoped
// retracts (edge alternation or kind-A node-detach), and false for the
// single-type table, where long-registered families keep their pre-contract
// predicate shapes.
func checkRegistryEntryMatchesItsRetract(t *testing.T, family string, entry materializedEdgeFamily, pinPredicates bool) {
	t.Helper()
	variants := allHeldRetracts(entry)
	if len(entry.RetractCypher) == 0 {
		t.Fatalf("family %q holds no RetractCypher; the drift guard cannot pin a retract it does not hold", family)
	}
	switch entry.RetractKind {
	case retractKindRelAlternation:
		for _, variant := range variants {
			checkRelAlternationRetract(t, family, variant, entry)
			if pinPredicates {
				checkClosedVocabularyPredicates(t, family, variant)
			}
		}
	case retractKindEvidenceNodeDetach:
		evidenceLabel := deriveEvidenceLabel(t, family, entry)
		derived := deriveEvidenceNodeDetachTypes(t, family, entry)
		var missingFromRegistry, missingFromDerived []string
		for relType := range derived {
			if _, ok := entry.EdgeTypes[relType]; !ok {
				missingFromRegistry = append(missingFromRegistry, relType)
			}
		}
		for relType := range entry.EdgeTypes {
			if _, ok := derived[relType]; !ok {
				missingFromDerived = append(missingFromDerived, relType)
			}
		}
		sort.Strings(missingFromRegistry)
		sort.Strings(missingFromDerived)
		if len(missingFromRegistry) > 0 {
			t.Errorf("write path emits %v but the registry omits them; the Ifá baseline gate would ignore those edges while calling %s exhaustive",
				missingFromRegistry, family)
		}
		if len(missingFromDerived) > 0 {
			t.Errorf("registry names %v but the write path never emits them; the live gate would assert an always-empty population for them",
				missingFromDerived)
		}
		for _, variant := range variants {
			checkEvidenceNodeDetachRetract(t, family, variant, evidenceLabel)
		}
	default:
		t.Fatalf("family %q: unknown RetractKind %d; add a dispatch arm before registering the kind", family, uint8(entry.RetractKind))
	}
}

// TestMaterializedEdgeFamilyRegistryMatchesItsRetract pins every registered
// family's declared edge types against what its retract actually deletes
// (#5543, #6228 retract-registry contract).
//
// The two live in different files and different shapes, and nothing else stops
// them drifting. Drift is silent in both directions: a type written but not
// retracted survives every reprojection as stale truth, and a type retracted but
// missing from the registry is one the Ifá baseline gate stops asserting while
// still calling the family exhaustive.
//
// The check reads the production consts held in the table, not literals copied
// into this test. A copy would pin the test file against itself and keep
// passing after the retract changed.
func TestMaterializedEdgeFamilyRegistryMatchesItsRetract(t *testing.T) {
	t.Parallel()

	for family, entry := range singleTypeMaterializedEdgeFamilies {
		t.Run("single/"+family, func(t *testing.T) {
			t.Parallel()
			checkRegistryEntryMatchesItsRetract(t, family, entry, false)
		})
	}
	for family, entry := range multiTypeDirectMaterializedEdgeFamilies {
		t.Run("multi/"+family, func(t *testing.T) {
			t.Parallel()
			checkRegistryEntryMatchesItsRetract(t, family, entry, true)
		})
	}
	if len(singleTypeMaterializedEdgeFamilies) == 0 || len(multiTypeDirectMaterializedEdgeFamilies) == 0 {
		t.Fatal("one registry table is empty; this test would pass vacuously on that half")
	}
}

// TestMultiTypeDirectMaterializedEdgeTypesFailsClosed proves an unregistered
// multi-type direct family reports absence rather than an empty set, mirroring
// the single-type fail-closed contract.
func TestMultiTypeDirectMaterializedEdgeTypesFailsClosed(t *testing.T) {
	t.Parallel()

	if got, ok := MultiTypeDirectMaterializedEdgeTypes("no_such_family"); ok {
		t.Errorf("unregistered family reported ok=true with %v; it must fail closed", got)
	}
	for _, family := range MultiTypeDirectMaterializedEdgeFamilyNames() {
		if _, ok := MultiTypeDirectMaterializedEdgeTypes(family); !ok {
			t.Errorf("registered family %q reported ok=false", family)
		}
	}
}

// TestMultiTypeDirectMaterializedEdgeTypesReturnsACopy stops a caller mutating
// the package-level multi-type table through the returned map.
func TestMultiTypeDirectMaterializedEdgeTypesReturnsACopy(t *testing.T) {
	t.Parallel()

	first, ok := MultiTypeDirectMaterializedEdgeTypes("security_group_sg_rule")
	if !ok {
		t.Fatal("security_group_sg_rule is not registered")
	}
	delete(first, "ALLOWS_INGRESS")

	second, _ := MultiTypeDirectMaterializedEdgeTypes("security_group_sg_rule")
	if _, ok := second["ALLOWS_INGRESS"]; !ok {
		t.Error("deleting from a returned map mutated the package registry; the accessor must return a copy")
	}
}

// TestRegisteredMultiTypeDirectFamiliesAreEnumeratedAsDirect keeps the
// multi-type table honest about what it is for: every entry must be a
// reducer-enumerated direct-materialization family, never a shared-projection
// family (which belongs in the single-type table) and never a name the
// reducer does not know.
func TestRegisteredMultiTypeDirectFamiliesAreEnumeratedAsDirect(t *testing.T) {
	t.Parallel()

	direct := map[string]struct{}{}
	for _, family := range reducer.DirectMaterializedEdgeFamilies() {
		direct[family] = struct{}{}
	}
	if len(direct) == 0 {
		t.Fatal("reducer.DirectMaterializedEdgeFamilies() returned zero families; this check would pass vacuously")
	}
	for _, family := range MultiTypeDirectMaterializedEdgeFamilyNames() {
		if _, ok := direct[family]; !ok {
			t.Errorf("family %q is registered in multiTypeDirectMaterializedEdgeFamilies but the reducer does not enumerate it as direct", family)
		}
	}
}

// TestMultiTypeDirectTableDerivationsMatchWriterVocabularies is the seam guard
// between the multi-type table's declared EdgeTypes and the writer-side maps
// the resolver arms read: the declaration and the derivation must name the
// same set, or the live gate asserts a different population than the drift
// guard pins. Both sides are read off held production consts and maps, never
// literals.
func TestMultiTypeDirectTableDerivationsMatchWriterVocabularies(t *testing.T) {
	t.Parallel()

	derived := map[string]map[string]struct{}{
		"observability_coverage":    {},
		"security_group_sg_rule":    {},
		"incident_routing_evidence": {},
	}
	for relType := range edgewriter.ObservabilityCoverageRelationshipTypes() {
		derived["observability_coverage"][relType] = struct{}{}
	}
	for relType := range group.SecurityGroupSGRuleRelationshipTypes() {
		derived["security_group_sg_rule"][relType] = struct{}{}
	}
	for _, relType := range edgewriter.IncidentRoutingEvidenceRelationshipTypes() {
		derived["incident_routing_evidence"][relType] = struct{}{}
	}
	for family, want := range derived {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			reg, ok := MultiTypeDirectMaterializedEdgeTypes(family)
			if !ok {
				t.Fatalf("family %q is not registered", family)
			}
			var missingFromTable, missingFromDerived []string
			for relType := range want {
				if _, ok := reg[relType]; !ok {
					missingFromTable = append(missingFromTable, relType)
				}
			}
			for relType := range reg {
				if _, ok := want[relType]; !ok {
					missingFromDerived = append(missingFromDerived, relType)
				}
			}
			sort.Strings(missingFromTable)
			sort.Strings(missingFromDerived)
			if len(missingFromTable) > 0 {
				t.Errorf("writer vocabulary emits %v but the table omits them", missingFromTable)
			}
			if len(missingFromDerived) > 0 {
				t.Errorf("table names %v but the writer vocabulary never emits them", missingFromDerived)
			}
		})
	}
}

// TestMaterializedEdgeIdentityPropertiesResolvesMultiTypeDirect proves the
// three multi-type direct families resolve identity without error and declare
// endpoint-only identity: their relationship MERGEs carry no property map, so
// a fixture naming identity properties for their types must fail to load.
func TestMaterializedEdgeIdentityPropertiesResolvesMultiTypeDirect(t *testing.T) {
	t.Parallel()

	for _, family := range MultiTypeDirectMaterializedEdgeFamilyNames() {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			got, err := MaterializedEdgeIdentityProperties(family)
			if err != nil {
				t.Fatalf("MaterializedEdgeIdentityProperties(%s): %v", family, err)
			}
			if len(got) != 0 {
				t.Fatalf("MaterializedEdgeIdentityProperties(%s) = %v, want empty (endpoint-keyed MERGEs)", family, got)
			}
		})
	}
}
