#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# runtime_declaration row (#6228 runtime_declaration live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 runtime_declaration live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_runtime_declaration_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler here is declaration.Handler, not a
# *_materialization handler behind a shared port: the runtime_declaration
# intent runs under its own additive domain (see WAIT_KEY), so the
# anchor below names the runtime declaration writer's own
# MERGE (repo)-[rel:DECLARES_RUNTIME]->(d) statement.

# Hand-derived, and non-vacuous now a fault cell exists:
# declaration.Handler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/runtime/declaration/handler.go:68)
# and Handle refuses to run without it (:83-85) before passing it to its
# fact load (:90). The loader is factload.LoadFactsForKinds
# (go/internal/reducer/factload/loader.go:55-63),
# which lists through the postgres fact store's kind-filtered page queries
# over FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,118,145), so an
# ACCESS EXCLUSIVE lock on that table holds it genuinely in flight -- the
# blocker_kind this row's fault cell takes. Same table and same reasoning
# as rows/13; see that row for why fact_work_items is the wrong target.
#
# No readiness banking (the k8s shape, not the crc-image one): this handler
# carries no in-handler readiness gate before its fact load -- Handle goes
# straight from the nil guards to the load -- and the domain carries no
# claim-time readiness requirement, so the first claim blocks
# deterministically in its load. A prereq-scoped reducer would add a wait
# nothing here needs.
IFA_FAMILY_BLOCKER_KIND[runtime_declaration]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[runtime_declaration]="handler"
# The fact_work_items.domain the projector fans this family out under, read
# off the collector's shared_followup marker payload
# (go/internal/collector/repo/git/followup_facts.go:430 carries
# reducer_domain runtime_declaration_projection), converted by the
# projector's generic per-fact intent path
# (go/internal/projector/runtime/reducer_intent.go), and corroborated by
# the domain definition (DomainRuntimeDeclarationProjection in
# go/internal/reducer/contract/domain.go:54). The live cell asserts the
# domain through the registry loop's own non-vacuity checks; the exact
# `runtime_declaration_projection | succeeded` row the determinism drive leaves
# is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[runtime_declaration]="runtime_declaration_projection"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[runtime_declaration]=1

IFA_FAMILY_DRIVE_FN[runtime_declaration]="ifa_runtime_declaration_drive"
IFA_FAMILY_ASSERT_FN[runtime_declaration]="ifa_runtime_declaration_assert"
IFA_FAMILY_CASSETTE_VAR[runtime_declaration]="runtime_declaration_cassette"
IFA_FAMILY_EXPECTED_VAR[runtime_declaration]="runtime_declaration_expected_edges"

# go/internal/storage/cypher/edge/writer/runtime_declaration.go, which reads
# `MERGE (repo)-[rel:DECLARES_RUNTIME]->(d)`. The anchor is matched against
# EXECUTED statement text, so it carries the FULL form: the template's
# edge MERGE carries no property map (the SETs follow it), so the bare
# `]->(d)` spelling DOES appear in executed text. DECLARES_RUNTIME is
# the only type this writer can emit (static token, no vocabulary
# substitution), and no other production statement MERGEs it -- the only
# other DECLARES_RUNTIME spellings in the tree are MATCH/DETACH DELETE
# retracts and prose. Pinning a PREFIX without the `->(d)` close would also
# match the node MERGE in the same template and name the wrong half of the
# statement the cell asserts.
IFA_FAMILY_ANCHOR[runtime_declaration]="MERGE (repo)-[rel:DECLARES_RUNTIME]->(d)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[runtime_declaration]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[runtime_declaration]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[runtime_declaration]="0"

IFA_FAMILY_HANDLER_GO_FILE[runtime_declaration]="go/internal/reducer/runtime/declaration/handler.go"

IFA_FAMILY_NAMES+=(runtime_declaration)
