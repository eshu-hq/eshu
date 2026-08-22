// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// blockerKind names the mechanism an Ifá fault-injection "kill worker
// mid-handler" cell uses to hold a materialized-edge family's work item
// claimed-but-incomplete when the reducer worker is SIGKILLed mid-handler, so
// the reclaim-and-converge proof exercises real domain-scoped recovery
// instead of ordinary baseline recovery on an already-completed item. The
// vocabulary matches the schema documented at the top of
// scripts/lib/ifa_family_registry.sh.
type blockerKind string

const (
	// blockerSharedIntentLock holds shared_projection_intents so a handler
	// mid-write through IntentWriter.UpsertIntents stays claimed-but-
	// incomplete on kill. Only a handler that actually writes that table can
	// be genuinely interrupted by locking it.
	blockerSharedIntentLock blockerKind = "shared_intent_lock"
	// blockerRunnerLeaseHold holds a shared projection partition lease so the
	// runner-stage wait observes a claimed-but-incomplete intent in the target
	// projection domain. It is distinct from blockerSharedIntentLock: the
	// handler may share its IntentWriter with another family, but the runner
	// lease key is the family-specific recovery seam.
	blockerRunnerLeaseHold blockerKind = "runner_lease_hold"
	// blockerAckBarrier holds public.fact_work_items so a handler that writes
	// graph edges directly (EdgeWriter, no shared_projection_intents write)
	// stays claimed-but-incomplete on kill.
	blockerAckBarrier blockerKind = "ack_barrier"
	// blockerTableLock holds a table_lock:<name> naming a table the handler
	// actually reads or writes on its way to the graph. The registry row's
	// raw value carries the ":<name>" suffix; classifyBlockerKind matches the
	// prefix and drops it, since this test's only falsifiable claim does not
	// depend on which table.
	blockerTableLock blockerKind = "table_lock"
	// blockerNone records that no kill-worker blocker cell exists yet for the
	// family. This is a real, separately tracked gap for some families (e.g.
	// sql_relationships as of this writing) -- this test asserts it
	// truthfully instead of inventing a blocker to make the table look full.
	// A family absent from the registry entirely (no row at all) is treated
	// the same as an explicit "none" row.
	blockerNone blockerKind = "none"
)

// classifyBlockerKind maps a registry row's raw declared string to the
// blockerKind vocabulary above. Returns false for anything that is not one
// of the five documented shapes, so a registry typo or a new blocker
// vocabulary this test does not know about fails loudly instead of silently
// comparing unequal to blockerSharedIntentLock and passing vacuously.
func classifyBlockerKind(raw string) (blockerKind, bool) {
	switch {
	case raw == string(blockerSharedIntentLock):
		return blockerSharedIntentLock, true
	case raw == string(blockerRunnerLeaseHold):
		return blockerRunnerLeaseHold, true
	case raw == string(blockerAckBarrier):
		return blockerAckBarrier, true
	case raw == string(blockerNone):
		return blockerNone, true
	case strings.HasPrefix(raw, string(blockerTableLock)+":"):
		return blockerTableLock, true
	default:
		return "", false
	}
}

// familyBlockerExpectation names the routed reducer materialization Domain
// (defaults_domain_catalog.go's switch key) whose handler actually performs
// one materialized-edge family's writes. It is a *name* linkage between a
// MaterializedEdgeFamilies() family string and a Domain constant, proven per
// entry below by the ProjectionDomain literal that handler's intent/edge
// builder tags its rows with. The declared blocker kind is deliberately NOT
// stored here: it is parsed live from scripts/lib/ifa_family_registry.sh by
// TestMaterializedEdgeFamilyBlockerLockstep, so a Go-side copy of the
// registry's own claim can never quietly drift from -- or worse, only ever
// agree with -- the real declaration. Likewise the STRUCTURAL fact this test
// asserts (does the handler have an IntentWriter field) is never hand-copied
// here: it is derived by calling implementedDefaultDomainDefinitions and
// reflecting on what it actually returns, so a handler swapped or refactored
// in that switch is caught automatically instead of silently going stale in
// a hand-typed copy.
type familyBlockerExpectation struct {
	routedDomain Domain
}

// materializedEdgeFamilyBlockerExpectations covers the 8 of 14
// MaterializedEdgeFamilies() families that map 1:1 onto a single routed
// handler through implementedDefaultDomainDefinitions' switch
// (defaults_domain_catalog.go:12-129). See
// materializedEdgeFamilyBlockerLockstepExclusions for the other 6 and why
// each is out of scope for a single-handler reflection. A missing row for any
// covered family is a named test failure, not an implicit pass. The three
// symbol-runtime families remain exclusions because
// reflection cannot distinguish their shared handler from code_calls; their
// runner_lease_hold proof is checked separately.
var materializedEdgeFamilyBlockerExpectations = map[string]familyBlockerExpectation{
	// code_call_materialization_intents.go:129,225 tag rows
	// ProjectionDomain: DomainCodeCalls; code_call_materialization.go:224
	// writes them via h.IntentWriter.UpsertIntents.
	DomainCodeCalls: {routedDomain: DomainCodeCallMaterialization},
	// sql_relationship_intents.go:101,154 tag rows
	// ProjectionDomain: DomainSQLRelationships; sql_relationship_materialization.go:113
	// writes them via h.IntentWriter.UpsertIntents.
	DomainSQLRelationships: {routedDomain: DomainSQLRelationshipMaterialization},
	// shell_exec_intents.go:58,106 tag rows ProjectionDomain: DomainShellExec;
	// shell_exec_materialization.go writes them via h.IntentWriter.UpsertIntents.
	DomainShellExec: {routedDomain: DomainShellExecMaterialization},
	// inheritance_intents.go:99,152 tag rows ProjectionDomain: DomainInheritanceEdges;
	// inheritance_materialization.go writes them via h.IntentWriter.UpsertIntents.
	DomainInheritanceEdges: {routedDomain: DomainInheritanceMaterialization},
	// rationale_edge_intents.go:104,164 tag rows ProjectionDomain: DomainRationaleEdges;
	// rationale_edge_materialization.go:98 writes them via h.IntentWriter.UpsertIntents.
	DomainRationaleEdges: {routedDomain: DomainRationaleMaterialization},
	// documentation_edge_materialization.go:268 tags rows
	// ProjectionDomain: DomainDocumentationEdges and writes them via
	// h.EdgeWriter -- no IntentWriter field.
	DomainDocumentationEdges: {routedDomain: DomainDocumentationMaterialization},
	// codeowners_ownership_materialization.go:271 tags rows
	// ProjectionDomain: DomainCodeownersOwnershipEdges and writes them via
	// h.EdgeWriter -- no IntentWriter field. This is the family
	// TestMaterializedEdgeFamilyBlockerLockstepCatchesWrongTableDeclaration
	// exercises. The vacuous shared_intent_lock shape it feeds that test is
	// now SYNTHETIC: #5992 removed it and #6160 replaced it with a fact_records
	// table lock, so the registry row and its pin both record
	// table_lock:fact_records today. The tooth is still the right one to keep --
	// it is the bug class, not a live bug report.
	DomainCodeownersOwnershipEdges: {routedDomain: DomainCodeownersOwnership},
	// submodule_pin_materialization.go:249 tags rows
	// ProjectionDomain: DomainSubmodulePinEdges and writes them via
	// h.EdgeWriter -- no IntentWriter field.
	DomainSubmodulePinEdges: {routedDomain: DomainSubmodulePin},
}

// materializedEdgeFamilyBlockerLockstepExclusions lists the
// MaterializedEdgeFamilies() families this test deliberately does not cover,
// each with why: none of them fit implementedDefaultDomainDefinitions' clean
// one-family-one-routed-domain switch shape, so "does the handler have an
// IntentWriter field" is not well-defined for a single handler the way it is
// for the 8 families in materializedEdgeFamilyBlockerExpectations.
// deployable_unit_edges DOES have a row in scripts/lib/ifa_family_registry.sh
// (table_lock:admission_decisions) but stays excluded here regardless: the
// registry landing a row for a family does not change whether that family's
// handler is reachable through the one switch this test reflects over.
var materializedEdgeFamilyBlockerLockstepExclusions = map[string]string{
	DomainDeployableUnitEdges: "wired via registry.go's DomainDeployableUnitCorrelation, not defaults_domain_catalog.go's switch; DeployableUnitCorrelationHandler already has its own correctly-scoped admission_decisions table_lock (scripts/lib/ifa_fault_injection_deployable_unit_cells.sh:296), confirmed by its own row in scripts/lib/ifa_family_registry.sh",
	DomainHandlesRoute:        "shares CodeCallMaterializationHandler with code_calls: buildSymbolRuntimeIntentRows uses the same handler IntentWriter, so this test's reflection cannot distinguish the family from code_calls. The exclusion is structural, not a coverage waiver: its runner_lease_hold lockstep is checked by TestMaterializedEdgeFamilyRunnerLeaseHoldLockstep, while the anchor-scoped graph-write cell remains family-specific.",
	DomainRunsIn:              "same shared CodeCallMaterializationHandler reflection limitation as handles_route; its runner_lease_hold lockstep is checked separately, and its anchor-scoped graph-write cell remains family-specific.",
	DomainInvokesCloudAction:  "same shared CodeCallMaterializationHandler reflection limitation as handles_route; its runner_lease_hold lockstep is checked separately, and its anchor-scoped graph-write cell remains family-specific.",
	DomainRepoDependency:      "three separate producer handlers (CrossRepoRelationshipHandler in cross_repo_resolution.go, the handler in package_source_correlation_handler.go, and code_import_repo_edge_handler.go) with inconsistently named intent-writer fields (IntentWriter vs RepoDependencyIntentWriter) -- not a single handler this test's one-field-name reflection can address",
	DomainWorkloadDependency:  "written by WorkloadMaterializationHandler.WorkloadDependencyEdgeWriter, a handler shared across many unrelated domains (DomainWorkloadMaterialization etc.) -- not 1:1 with this family",
}

// materializedEdgeFamilyNotYetInRegistry is the deliberate, reviewed
// allowlist of covered families with no row in
// scripts/lib/ifa_family_registry.sh yet. It exists so a missing row is
// either explicitly acknowledged here with a reason, or fails the test by
// name -- never silently treated as "no blocker declared" the way an earlier
// version of this file did. Confirmed by reading scripts/lib for a dedicated
// ifa_fault_injection_<family>_cells.sh: none exists for any family still
// listed below. This list is expected to stay empty while all covered families
// have rows; TestMaterializedEdgeFamilyBlockerLockstep
// fails loudly if a listed family's row appears without this list being
// updated to drop it, so the list cannot go stale in the other direction
// either. submodule_pin_edges (#6002) is the family this happened to most
// recently: scripts/lib/ifa_family_registry/rows/07_submodule_pin_edges.sh
// and scripts/lib/ifa_fault_injection_submodule_pin_cells.sh landed
// together, so it is removed rather than added here.
var materializedEdgeFamilyNotYetInRegistry = map[string]string{}

// noopFactLoader, noopIntentWriter, and noopSharedProjectionEdgeWriter are
// minimal non-nil DefaultHandlers dependency stubs. They exist only so
// implementedDefaultDomainDefinitions wires every case this test covers the
// same way runtime does, matching the real handler shape as closely as a
// zero-effort stub can; this test never calls Handle() on the returned
// handlers, so their method bodies are unreachable and only need to satisfy
// the interface, not behave usefully.
type noopFactLoader struct{}

func (noopFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return nil, nil
}

// noopIntentWriter satisfies CodeCallIntentWriter, SQLRelationshipIntentWriter,
// ShellExecIntentWriter, InheritanceIntentWriter, and RationaleEdgeIntentWriter
// -- all five are structurally the same single-method interface
// (UpsertIntents(context.Context, []SharedProjectionIntentRow) error), so one
// stub covers all five DefaultHandlers fields.
type noopIntentWriter struct{}

func (noopIntentWriter) UpsertIntents(context.Context, []SharedProjectionIntentRow) error {
	return nil
}

// noopSharedProjectionEdgeWriter satisfies SharedProjectionEdgeWriter, the
// type of DocumentationEdgeWriter, CodeownersOwnershipEdgeWriter, and
// SubmodulePinEdgeWriter on DefaultHandlers.
type noopSharedProjectionEdgeWriter struct{}

func (noopSharedProjectionEdgeWriter) RetractEdges(context.Context, string, []SharedProjectionIntentRow, string) error {
	return nil
}

func (noopSharedProjectionEdgeWriter) WriteEdges(context.Context, string, []SharedProjectionIntentRow, string) (SharedProjectionWriteReport, error) {
	return SharedProjectionWriteReport{}, nil
}

// materializedEdgeFamilyHandlersByDomain calls the real production wiring
// function -- implementedDefaultDomainDefinitions, the same switch
// defaults.go uses to build the runtime's actual handler catalog -- with
// every dependency field the 8 covered families' handlers declare populated
// by a non-nil stub, and returns the routed Domain -> Handler binding it
// produced.
//
// Populating every field matters even though none of the 8 covered switch
// cases in defaults_domain_catalog.go gate their def.Handler assignment on a
// handlers.X != nil check today (verified by reading defaults_domain_catalog.go:64-124;
// contrast DomainDeploymentMapping at :26-45, whose CrossRepoResolver sub-field
// IS conditionally wired, and the DomainConfigStateDrift-shaped additive
// domains that appendAdditiveDomainDefinitions omits entirely without
// adapters -- TestImplementedDefaultDomainDefinitionsOmitsConfigStateDriftWithoutAdapters
// in defaults_test.go proves that omission). A zero-value DefaultHandlers{}
// would happen to produce the same non-nil result for these 8 families today,
// but that is an accident of the current switch, not a guarantee: if a future
// change added a dependency gate to one of these 8 cases the way
// DomainDeploymentMapping already has, a zero-value call would silently start
// returning nil for that family. Populating stubs here removes that
// dependency; TestMaterializedEdgeFamilyBlockerLockstepHandlersAreRegistered
// below is what turns a future nil into a loud, named test failure instead of
// a subset this test quietly stops covering.
func materializedEdgeFamilyHandlersByDomain() map[Domain]Handler {
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                    noopFactLoader{},
		CodeCallIntentWriter:          noopIntentWriter{},
		SQLRelationshipIntentWriter:   noopIntentWriter{},
		ShellExecIntentWriter:         noopIntentWriter{},
		InheritanceIntentWriter:       noopIntentWriter{},
		RationaleEdgeIntentWriter:     noopIntentWriter{},
		DocumentationEdgeWriter:       noopSharedProjectionEdgeWriter{},
		CodeownersOwnershipEdgeWriter: noopSharedProjectionEdgeWriter{},
		SubmodulePinEdgeWriter:        noopSharedProjectionEdgeWriter{},
		PriorGenerationCheck:          func(context.Context, string, string) (bool, error) { return true, nil },
		Instruments:                   &telemetry.Instruments{},
	})
	byDomain := make(map[Domain]Handler, len(definitions))
	for _, def := range definitions {
		if def.Handler != nil {
			byDomain[def.Domain] = def.Handler
		}
	}
	return byDomain
}

// handlerHoldsIntentWriter reports whether handler's concrete type has a
// field literally named IntentWriter -- the shared-projection intent path
// (BuildSharedProjectionIntent plus IntentWriter.UpsertIntents) that
// blockerSharedIntentLock can genuinely interrupt. A handler without that
// field writes graph edges directly through an EdgeWriter and never touches
// shared_projection_intents, so locking that table cannot block it.
func handlerHoldsIntentWriter(handler Handler) bool {
	t := reflect.TypeOf(handler)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return false
	}
	_, ok := t.FieldByName("IntentWriter")
	return ok
}

// checkFamilyBlockerLockstep returns a non-nil, family-naming error exactly
// when declared lies about handler's real write shape: blockerSharedIntentLock
// for a handler that has no IntentWriter field, and therefore never writes
// shared_projection_intents, so locking that table never intercepts it. Any
// other declared kind (ack_barrier, table_lock, none) is accepted regardless
// of handler shape -- this test's only proven claim is the one direction
// that is always wrong.
func checkFamilyBlockerLockstep(family string, routedDomain Domain, declared blockerKind, handler Handler) error {
	if handler == nil {
		return fmt.Errorf("family %q: routed domain %q has no registered handler in implementedDefaultDomainDefinitions(<fully-populated DefaultHandlers>) -- the routed-domain linkage in materializedEdgeFamilyBlockerExpectations is stale", family, routedDomain)
	}
	if declared != blockerSharedIntentLock {
		return nil
	}
	if handlerHoldsIntentWriter(handler) {
		return nil
	}
	return fmt.Errorf(
		"family %q (handler %T): scripts/lib/ifa_family_registry.sh declares blocker kind %q, which locks shared_projection_intents, but this handler has no IntentWriter field -- it writes graph edges directly (EdgeWriter) and never touches that table. Locking it lets the handler run to completion and ack before kill -9 lands, so the cell proves ordinary baseline recovery, not the domain-scoped reclaim its name claims. Use ack_barrier or a table_lock:<name> naming a table this handler actually reads or writes instead.",
		family, handler, declared,
	)
}

// TestMaterializedEdgeFamilyBlockerLockstep binds each covered family's
// blocker kind -- parsed live from scripts/lib/ifa_family_registry.sh, the
// real declaration another lane owns, never a Go-side copy of it -- to its
// real handler's write shape, derived by calling the production wiring
// function and reflecting on what it returns (never by hand-copying handler
// field names). See checkFamilyBlockerLockstep for the one direction it can
// prove wrong.
func TestMaterializedEdgeFamilyBlockerLockstep(t *testing.T) {
	t.Parallel()

	declaredRaw := parseIfaFamilyRegistryBlockerKinds(t, ifaFamilyRegistryRowsDir(t))
	handlerByDomain := materializedEdgeFamilyHandlersByDomain()

	for family, want := range materializedEdgeFamilyBlockerExpectations {
		family, want := family, want
		t.Run(family, func(t *testing.T) {
			t.Parallel()

			raw, hasRow := declaredRaw[family]
			reason, listedNotYet := materializedEdgeFamilyNotYetInRegistry[family]
			switch {
			case !hasRow && !listedNotYet:
				t.Fatalf("family %q: has no row in scripts/lib/ifa_family_registry.sh and is not listed in materializedEdgeFamilyNotYetInRegistry -- a covered family must be either declared in the registry or explicitly marked not-yet-declared with a reason; a fault cell landing with no registry row must not silently pass", family)
			case !hasRow && listedNotYet:
				return // explicitly acknowledged as not-yet-declared (reason: reason); nothing to compare yet
			case hasRow && listedNotYet:
				t.Fatalf("family %q: has a row in scripts/lib/ifa_family_registry.sh (%q) but is still listed in materializedEdgeFamilyNotYetInRegistry (%q) -- its fault cell has landed, remove it from that list", family, raw, reason)
			}

			declared, known := classifyBlockerKind(raw)
			if !known {
				t.Fatalf("family %q: scripts/lib/ifa_family_registry.sh declares blocker kind %q, which is not one of shared_intent_lock, runner_lease_hold, ack_barrier, none, or table_lock:<name> -- the registry's vocabulary drifted from what this test's classifier understands", family, raw)
			}

			if err := checkFamilyBlockerLockstep(family, want.routedDomain, declared, handlerByDomain[want.routedDomain]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestMaterializedEdgeFamilyBlockerLockstepNotYetDeclaredListIsScoped guards
// materializedEdgeFamilyNotYetInRegistry itself: every entry must name a
// family materializedEdgeFamilyBlockerExpectations actually covers (a typo
// or stray entry here would silently exempt some other family from
// TestMaterializedEdgeFamilyBlockerLockstep's missing-row check) and carry a
// non-blank reason.
func TestMaterializedEdgeFamilyBlockerLockstepNotYetDeclaredListIsScoped(t *testing.T) {
	t.Parallel()

	for family, reason := range materializedEdgeFamilyNotYetInRegistry {
		if _, covered := materializedEdgeFamilyBlockerExpectations[family]; !covered {
			t.Errorf("materializedEdgeFamilyNotYetInRegistry names family %q, which materializedEdgeFamilyBlockerExpectations does not cover -- stale or mistyped entry", family)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("family %q in materializedEdgeFamilyNotYetInRegistry has a blank reason", family)
		}
	}
}

// TestMaterializedEdgeFamilyBlockerLockstepHandlersAreRegistered asserts,
// independently of the blocker-kind comparison, that every covered family's
// routedDomain actually comes back with a non-nil Handler from
// implementedDefaultDomainDefinitions given a fully-populated DefaultHandlers.
// A nil handler here means the routedDomain linkage in
// materializedEdgeFamilyBlockerExpectations is stale or a dependency gate was
// added to a case this test assumed was unconditional -- either way that MUST
// fail loudly and name the family, not be silently skipped by a map lookup
// that quietly leaves the family out of TestMaterializedEdgeFamilyBlockerLockstep.
func TestMaterializedEdgeFamilyBlockerLockstepHandlersAreRegistered(t *testing.T) {
	t.Parallel()

	handlerByDomain := materializedEdgeFamilyHandlersByDomain()
	for family, want := range materializedEdgeFamilyBlockerExpectations {
		family, want := family, want
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			handler, ok := handlerByDomain[want.routedDomain]
			if !ok || handler == nil {
				t.Fatalf("family %q: routed domain %q has no non-nil Handler in implementedDefaultDomainDefinitions(<fully-populated DefaultHandlers>) -- this family is silently uncovered, not merely unclassified", family, want.routedDomain)
			}
		})
	}
}

// TestMaterializedEdgeFamilyBlockerLockstepCoversAllFamilies is the totality
// assertion: every family MaterializedEdgeFamilies() returns must be either
// covered by materializedEdgeFamilyBlockerExpectations or excluded with a
// named reason in materializedEdgeFamilyBlockerLockstepExclusions, never
// both and never neither. Without this, a 15th family added later would
// silently fall through this test looking green while asserting nothing
// about it; with it, an unclassified family fails loudly until someone
// deliberately covers or excludes it.
func TestMaterializedEdgeFamilyBlockerLockstepCoversAllFamilies(t *testing.T) {
	t.Parallel()

	all := make(map[string]bool, len(MaterializedEdgeFamilies()))
	for _, family := range MaterializedEdgeFamilies() {
		all[family] = true
	}

	classified := make(map[string]bool, len(all))
	for family := range materializedEdgeFamilyBlockerExpectations {
		if !all[family] {
			t.Errorf("materializedEdgeFamilyBlockerExpectations names family %q, which MaterializedEdgeFamilies() does not return -- stale entry", family)
			continue
		}
		classified[family] = true
	}
	for family, reason := range materializedEdgeFamilyBlockerLockstepExclusions {
		if !all[family] {
			t.Errorf("materializedEdgeFamilyBlockerLockstepExclusions names family %q, which MaterializedEdgeFamilies() does not return -- stale entry", family)
			continue
		}
		if classified[family] {
			t.Errorf("family %q is both covered by materializedEdgeFamilyBlockerExpectations and excluded (%q) -- pick one", family, reason)
			continue
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("family %q is excluded with a blank reason -- every exclusion needs a named reason", family)
		}
		classified[family] = true
	}

	for family := range all {
		if !classified[family] {
			t.Errorf("family %q from MaterializedEdgeFamilies() is neither covered by materializedEdgeFamilyBlockerExpectations nor excluded with a reason in materializedEdgeFamilyBlockerLockstepExclusions -- a new or renamed materialized-edge family must be classified deliberately before this test can pass", family)
		}
	}
}

// TestMaterializedEdgeFamilyBlockerLockstepCatchesWrongTableDeclaration is
// the deliberate-break tooth: it proves checkFamilyBlockerLockstep itself
// fails, and names the right thing, on the exact bug class this test exists
// to catch -- independent of file I/O, so it stays fast and deterministic as
// a permanent regression check. It calls checkFamilyBlockerLockstep directly
// with blockerSharedIntentLock for codeowners_ownership_edges' real
// (reflected) handler -- a shared_projection_intents lock declared for an
// EdgeWriter-only handler -- and asserts it is rejected. That declaration was
// once committed for this family; #5992 removed it and #6160 replaced it with
// a fact_records table lock, so the input here is now constructed rather than
// quoted from a live file. Keeping it costs nothing and the bug class it
// guards is what any future family can still hit.
func TestMaterializedEdgeFamilyBlockerLockstepCatchesWrongTableDeclaration(t *testing.T) {
	t.Parallel()

	const family = DomainCodeownersOwnershipEdges
	routedDomain := materializedEdgeFamilyBlockerExpectations[family].routedDomain
	handler := materializedEdgeFamilyHandlersByDomain()[routedDomain]

	err := checkFamilyBlockerLockstep(family, routedDomain, blockerSharedIntentLock, handler)
	if err == nil {
		t.Fatalf("checkFamilyBlockerLockstep(%q, shared_intent_lock, %T) = nil error, want an error: this handler has no IntentWriter field, so declaring shared_intent_lock must be caught", family, handler)
	}
	if !strings.Contains(err.Error(), family) {
		t.Errorf("error %q does not name the family %q", err.Error(), family)
	}
	if !strings.Contains(err.Error(), "IntentWriter") {
		t.Errorf("error %q does not explain the missing IntentWriter field", err.Error())
	}
}

// TestIfaFamilyRegistryWaitKeyIsKnownDomain proves each family's
// IFA_FAMILY_WAIT_KEY row -- parsed live from
// scripts/lib/ifa_family_registry/rows/ by parseIfaFamilyRegistryWaitKeys,
// never a Go-side copy of it -- names a real reducer Domain constant, checked
// against knownDomains through the same Domain.Validate() production code
// path ParseDomain uses, never a hand-copied constant list. Each row is
// otherwise guarded only by a hand-typed pin in the fault-injection wait
// helper comparing one hand-typed string to another: both sides can rename
// together and still agree on a value neither of them is real. That drift
// would surface only as a live Docker shard timing out in
// ifa_fault_wait_for_claimed after tens of minutes -- loud, but enormously
// expensive for something this test catches in milliseconds.
func TestIfaFamilyRegistryWaitKeyIsKnownDomain(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)
	if len(waitKeys) == 0 {
		t.Fatal("parsed zero IFA_FAMILY_WAIT_KEY rows -- registry format changed or the rows were emptied")
	}
	for family, raw := range waitKeys {
		family, raw := family, raw
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			// knownDomains is the fact_work_items keyspace, so this check only
			// applies to handler-stage rows. A runner-stage wait_key is a
			// projection domain from allProjectionDomains, a DISJOINT keyspace
			// -- validating it here would reject a correct row for the wrong
			// reason the moment the first runner-stage family lands, which is
			// the explicit purpose of the enabler this test ships with.
			// TestIfaFamilyRegistryWaitStageAndKeyCohere owns the runner half.
			// A MISSING wait_stage must not take the runner-stage exit. Skipping
			// on absence would leave such a row validated by nothing: it would
			// be waved through here AND absent from
			// TestIfaFamilyRegistryWaitStageAndKeyCohere, which ranges over
			// waitStages. Unreachable while each row file declares exactly one
			// family -- nothing enforces that -- so fail loudly rather than
			// rely on a property no gate asserts.
			stage, ok := waitStages[family]
			if !ok {
				t.Fatalf("family %q has an IFA_FAMILY_WAIT_KEY but no IFA_FAMILY_WAIT_STAGE row; it would then be checked by neither this test nor TestIfaFamilyRegistryWaitStageAndKeyCohere", family)
			}
			if stage != "handler" {
				t.Skipf("family %q declares wait_stage=%q; its wait_key lives in allProjectionDomains, not knownDomains, and is checked by TestIfaFamilyRegistryWaitStageAndKeyCohere", family, stage)
			}
			if err := Domain(raw).Validate(); err != nil {
				t.Fatalf("family %q: IFA_FAMILY_WAIT_KEY=%q is not a real reducer Domain constant (%v) -- scripts/lib/ifa_family_registry.sh's row and the fault-injection wait helper's hand-typed pin could rename together and still agree on a dead string here", family, raw, err)
			}
		})
	}
}

// TestIfaFamilyRegistryWaitStageAndKeyCohere proves each family's declared
// (wait_stage, wait_key) pair names a queue that exists and a key that queue
// can actually contain, and that shared_intent_lock only ever pairs with the
// stage whose proof it depends on.
//
// The three rules, and the failure each one prevents:
//
//   - wait_stage=handler => wait_key must validate as a fact_work_items claim
//     Domain. That is what ifa_fault_wait_for_claimed polls; a key that is not
//     a real Domain waits for a row that can never appear and the cell dies on
//     a timeout tens of minutes into a live shard.
//   - wait_stage=runner => wait_key must be a member of allProjectionDomains.
//     That is what the runner-stage predicate polls; the two keyspaces are
//     disjoint, and a projection domain in a handler row (or the reverse) is
//     the same never-appears failure wearing the other queue's name.
//   - blocker_kind=shared_intent_lock => wait_stage MUST be handler. The
//     mandatory retry-above-baseline proof dereferences
//     fact_work_items.attempt_count scoped to wait_key, and
//     shared_projection_intents has no attempt_count column at all, so a
//     runner wait_key can never satisfy it. The cell would fail claiming the
//     family never retried, which is a statement about the reducer rather than
//     about the row that is actually wrong.
//
// TestIfaFamilyRegistryWaitKeyIsKnownDomain validated EVERY row against
// knownDomains regardless of stage. That was correct while every registered
// family was handler-stage and becomes wrong the moment a runner-stage family
// lands -- it would reject a correct row for the wrong reason. That test is
// gated on wait_stage == handler in this same change so the deference is real
// rather than asserted here; it keeps the handler half, this test owns the
// runner half and the blocker/stage pairing.
func TestIfaFamilyRegistryWaitStageAndKeyCohere(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)
	blockerKinds := parseIfaFamilyRegistryBlockerKinds(t, rowsDir)
	if len(waitKeys) == 0 || len(waitStages) == 0 {
		t.Fatal("parsed zero wait_key or wait_stage rows -- registry format changed or the rows were emptied")
	}

	projectionDomains := make(map[Domain]struct{}, len(allProjectionDomains))
	for _, d := range allProjectionDomains {
		projectionDomains[d] = struct{}{}
	}

	for family, stage := range waitStages {
		family, stage := family, stage
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			raw, ok := waitKeys[family]
			if !ok {
				t.Fatalf("family %q declares IFA_FAMILY_WAIT_STAGE=%q but no IFA_FAMILY_WAIT_KEY -- a stage with no key polls nothing", family, stage)
			}
			switch stage {
			case "handler":
				if err := Domain(raw).Validate(); err != nil {
					t.Fatalf("family %q: wait_stage=handler but wait_key %q is not a real reducer Domain (%v) -- ifa_fault_wait_for_claimed would poll fact_work_items for a domain that never appears", family, raw, err)
				}
			case "runner":
				if _, ok := projectionDomains[Domain(raw)]; !ok {
					t.Fatalf("family %q: wait_stage=runner but wait_key %q is not in allProjectionDomains -- the runner predicate polls shared_projection_intents.projection_domain, a disjoint keyspace from fact_work_items.domain", family, raw)
				}
			default:
				t.Fatalf("family %q declares IFA_FAMILY_WAIT_STAGE=%q, which is neither handler nor runner", family, stage)
			}
			if blockerKinds[family] == "shared_intent_lock" && stage != "handler" {
				t.Fatalf("family %q declares blocker_kind=shared_intent_lock with wait_stage=%q -- that blocker's mandatory retry-above-baseline proof reads fact_work_items.attempt_count scoped to wait_key, and shared_projection_intents has no attempt_count, so the proof could never pass and the cell would fail blaming the reducer", family, stage)
			}
		})
	}
}

// TestIfaFamilyRegistryHandlerWaitKeysAreExclusive proves no two handler-stage
// families claim the same wait_key.
//
// The failure this prevents is not false evidence, which is why it is worth
// stating precisely. Two families sharing one handler share one intent-write
// call, so killing that handler mid-write genuinely interrupts both -- the
// evidence is real. What is not real is the ACCOUNTING: the registry would
// claim two per-family kill proofs from one mechanism run, while the second
// family's own surface, the one that distinguishes it from the first, stays
// unexercised. That is a check agreeing with itself, and uniqueness makes it
// unrepresentable rather than merely discouraged.
//
// The tie-break when two families genuinely contest one handler domain is that
// the handler's namesake owns the handler-stage wait and every other family
// must prove its own seam. This test never applies that rule automatically --
// it fails naming both rows and a human decides, because auto-resolving would
// silently pick a winner and re-create the accounting error it exists to stop.
func TestIfaFamilyRegistryHandlerWaitKeysAreExclusive(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)

	owner := make(map[string]string, len(waitKeys))
	families := make([]string, 0, len(waitStages))
	for family := range waitStages {
		families = append(families, family)
	}
	sort.Strings(families)

	checked := 0
	for _, family := range families {
		if waitStages[family] != "handler" {
			continue
		}
		key := waitKeys[family]
		if key == "" {
			continue
		}
		if prior, clash := owner[key]; clash {
			t.Fatalf("families %q and %q both declare wait_stage=handler with wait_key=%q -- one kill of that handler cannot be counted as a per-family proof for both. The handler's namesake owns the handler-stage wait; the other family must prove its own seam (a runner-stage wait on its own projection domain, or a blocker that engages a write only it performs).", prior, family, key)
		}
		owner[key] = family
		checked++
	}
	if checked == 0 {
		t.Fatal("no handler-stage family was checked for wait_key exclusivity -- the parse produced no handler rows, so this test proved nothing")
	}
}
