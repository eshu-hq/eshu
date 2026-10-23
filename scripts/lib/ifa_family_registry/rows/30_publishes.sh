#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# publishes row (#6228 publishes live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 publishes live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_publishes_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler here is PackageSourceHandler, not a
# *_materialization handler behind a shared port: the publishes intent
# fans out under its own package_source_correlation domain (see WAIT_KEY),
# so the anchor below names the provenance writer's own PUBLISHES
# templates.

# Hand-derived, and non-vacuous now a fault cell exists:
# PackageSourceHandler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/packages/correlation/source_handler.go:48)
# and Handle refuses to run without it (:80) before passing it to its
# fact load (:87). The loader is factload.LoadFactsForKinds
# (go/internal/reducer/factload/loader.go:55-67), which pages over
# FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:49,75,103,130), so an
# ACCESS EXCLUSIVE lock on that table holds it genuinely in flight -- the
# blocker_kind this row's fault cell takes. Same table and same reasoning
# as rows/13; see that row for why fact_work_items is the wrong target.
# The cross-scope active-repository fallback
# (source_handler.go:96-100) reads the same table, so it blocks with the
# own-scope load rather than slipping past the lock.
#
# No readiness banking (the k8s shape, not the crc-image one): this handler
# carries no in-handler readiness gate before its fact load -- Handle goes
# straight from the nil guards to the load -- and the domain carries no
# claim-time readiness requirement, so the first claim blocks
# deterministically in its load. A prereq-scoped reducer would add a wait
# nothing here needs.
IFA_FAMILY_BLOCKER_KIND[publishes]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[publishes]="handler"
# The fact_work_items.domain the projector fans this family out under, read
# off BuildReducerIntent
# (go/internal/projector/package/source/reducer_intent.go:32),
# and corroborated by the domain definition
# (DomainPackageSourceCorrelation in
# go/internal/reducer/contract/domain.go:114-115). The live cell asserts the
# domain through the registry loop's own non-vacuity checks; the exact
# `package_source_correlation | succeeded` row the determinism drive leaves
# is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[publishes]="package_source_correlation"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[publishes]=1

IFA_FAMILY_DRIVE_FN[publishes]="ifa_publishes_drive"
IFA_FAMILY_ASSERT_FN[publishes]="ifa_publishes_assert"
IFA_FAMILY_CASSETTE_VAR[publishes]="publishes_cassette"
IFA_FAMILY_EXPECTED_VAR[publishes]="publishes_expected_edges"

# go/internal/storage/cypher/edge/writer/provenance.go:140,154, which read
# `MERGE (repo)-[rel:PUBLISHES {` with the scope_id/evidence_source
# identity map on the MERGE itself (Package and PackageVersion twins share
# the prefix). The anchor is matched against EXECUTED statement text, so it
# carries the PREFIX form (the same rationale as the built_from and
# derived_from pins): a full `]->(target)` spelling would match no executed
# statement and the fault would never fire. PUBLISHES is the only type
# either template can emit (static token, no vocabulary substitution), and
# no other production statement MERGEs it -- the only other PUBLISHES
# spellings in the tree are retract DELETEs and prose.
IFA_FAMILY_ANCHOR[publishes]="MERGE (repo)-[rel:PUBLISHES"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[publishes]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[publishes]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[publishes]="0"

IFA_FAMILY_HANDLER_GO_FILE[publishes]="go/internal/reducer/packages/correlation/source_handler.go"

IFA_FAMILY_NAMES+=(publishes)
