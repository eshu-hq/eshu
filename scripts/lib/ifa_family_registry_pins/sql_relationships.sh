#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# sql_relationships hand-derived pin (#6147 PR-0 family registry). Sourced by
# scripts/lib/test-ifa-family-registry-derived-pins-cases.sh, which owns the
# hand-authored-literal rule, the totality diff, and the comparison against
# scripts/lib/ifa_family_registry.sh -- read that file's header before
# touching this one. Every value below is HAND-TYPED literal text, derived by
# reading the citations inline; it is never sourced, generated, or read back
# out of the registry.

# go/internal/reducer/sqlrelationship/sql_relationship_materialization.go:52
# declares SQLRelationshipIntentWriter; :64 embeds it as the handler's
# IntentWriter field; :117 calls h.IntentWriter.UpsertIntents(...) inside
# Handle() (relocated from the reducer root to this subpackage, issue #6061;
# line numbers shifted with the move). The
# handler is architecturally CAPABLE of a shared_intent_lock the same way
# code_calls/rationale_edges are -- but the family's actual fault-injection
# kill cell does not use that mechanism. Read directly (not taken from the
# registry): scripts/lib/ifa_fault_injection_sql_cells.sh:86 (cell_killworker_sql)
# (cell_killworker_sql) calls ifa_fault_wait_for_claimed against fact_work_items
# domain "sql_relationship_materialization"
# (go/internal/reducer/intent.go:55 DomainSQLRelationshipMaterialization
# Domain = "sql_relationship_materialization" -- handler stage), then kills
# the reducer directly with NO lock acquisition anywhere in the function --
# no call to ifa_fault_start_shared_intent_lock or any other blocker helper
# appears in that file. The function's own header comment (:66-85) states this
# plainly, at :78: "What it does NOT prove: that the kill landed mid-handler.
# [...] the restart exercises an already-finished unit and the digest match
# afterwards says nothing about SQL recovery specifically." A
# SEPARATE cell, cell_failgraphwrite_sql (anchored to the QUERIES_TABLE
# MERGE, a once-fault marker, not a queue lock), is what actually backs this
# family's fault-coverage claim. => blocker_kind=none. (This corrected an
# earlier draft of this file's predecessor, which assumed shared_intent_lock
# by analogy to code_calls/rationale_edges without checking this family's
# cell file directly.)
IFA_FAMILY_PIN_BLOCKER_KIND="none"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="sql_relationship_materialization"

# go/internal/storage/cypher/canonical.go:183-189
# (batchCanonicalSQLQueriesTableUpsertCypher) is the SQL-relationship
# family's QUERIES_TABLE write template (one of nine edge types the family
# materializes, per ifa_family_fixtures.sh's header comment, but the one
# this family's fail-graph-write cell targets -- scripts/verify-ifa-fault-injection.sh's sql_edge_operation_match
# sql_edge_operation_match agrees byte-for-byte). shared_cell: driven every N
# cell, THROUGH the registry loop -- scripts/verify-ifa-determinism.sh:338
# calls ifa_family_registry_drive for every shared_cell family, this one
# included. An earlier version of this paragraph said the family "predates the
# registry loop and is still driven by its own inline call": that inline call
# was deleted in this same change, which is precisely why
# ifa_family_registry_drive carries a special-case branch for this family's
# grandfathered ifa_det_drive_sql_baseline signature. The value was right and
# the reason was wrong -- the failure mode the documentation_edges pin warns is
# the one a pin file is least able to survive.
# cell_kind: derived from the gate's call sites, not from blocker_kind.
# scripts/verify-ifa-fault-injection.sh names this family's own functions
# (ifa_fault_shard_run cell_killworker_sql / cell_failgraphwrite_sql); nothing
# routes it through cell_killworker_family. So custom, even though
# blocker_kind=none IS a shape the generic dispatcher supports -- that is the
# distinction the registry schema now spells out, and reading it the other way
# would sanction migrating this family onto the no-blocker path, which is
# weaker than the bespoke cell it has.
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:QUERIES_TABLE]->(target)"
IFA_FAMILY_PIN_SHARED_CELL=1
IFA_FAMILY_PIN_CELL_KIND="custom"
