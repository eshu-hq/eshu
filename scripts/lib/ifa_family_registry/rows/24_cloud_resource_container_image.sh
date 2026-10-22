#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2154
# cloud_resource_container_image row (#6228 slice D live leaf). See
# ../../ifa_family_registry.sh for the schema and every array declaration
# this file assigns into. Read
# rows/13_iam_instance_profile_role.sh's header first -- it explains what
# a DIRECT-materialization family is and which blocker kinds are
# unavailable to one.
#
# FAULT CELLS LANDED (#6228 slice D live leaf).
# scripts/verify-ifa-fault-injection.sh drives and asserts this row through
# scripts/lib/ifa_fault_injection_cloud_resource_container_image_cells.sh
# (baseline/killworker/failgraphwrite trio, dispatched in shard order with
# a co-location atomic group). cell_kind=custom below records the
# hand-written dispatch; specs/ci-gates.v1.yaml gives this family
# ifa-fault-injection triggers so an edit to its cassette or its writer
# re-runs the matrix that observes it.
#
# The writing handler here is AWSCloudImageMaterializationHandler, not a
# *_materialization handler behind a shared port: the AWS cloud-image
# materialization intent fans out under its own additive domain (see
# WAIT_KEY), so the anchor below names the image writer's own
# two-MATCH-MERGE statement.

# Hand-derived, and non-vacuous now a fault cell exists:
# AWSCloudImageMaterializationHandler embeds `FactLoader factload.FactLoader`
# (go/internal/reducer/awscloud/aws_cloud_image_materialization.go:100)
# and Handle refuses to run without it (:134-136) before loading its facts
# through it at :163. The handler reads fact_records AFTER claiming its
# work item, so an ACCESS EXCLUSIVE lock on that table holds it genuinely
# in flight -- the blocker_kind this row's fault cell takes. Same table
# and same reasoning as rows/13; see that row for why fact_work_items is
# the wrong target.
IFA_FAMILY_BLOCKER_KIND[cloud_resource_container_image]="table_lock:fact_records"
IFA_FAMILY_WAIT_STAGE[cloud_resource_container_image]="handler"
# The fact_work_items.domain the projector fans this family out under, read
# off BuildMaterializationReducerIntent
# (go/internal/projector/cloud/aws/image/materialization_intents.go:69),
# and corroborated by the domain definition
# (DomainAWSCloudImageMaterialization in
# go/internal/reducer/contract/domain.go:17). The live cell asserts the
# domain through the registry loop's own non-vacuity checks; the exact
# `aws_cloud_image_materialization | succeeded` row the determinism drive
# leaves is CI-observed, not assumed here.
IFA_FAMILY_WAIT_KEY[cloud_resource_container_image]="aws_cloud_image_materialization"
# A plain reducer family needing no maintenance pass, so it is driven uniformly
# in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_SHARED_CELL[cloud_resource_container_image]=1

IFA_FAMILY_DRIVE_FN[cloud_resource_container_image]="ifa_cloud_resource_container_image_drive"
IFA_FAMILY_ASSERT_FN[cloud_resource_container_image]="ifa_cloud_resource_container_image_assert"
IFA_FAMILY_CASSETTE_VAR[cloud_resource_container_image]="cloud_resource_container_image_cassette"
IFA_FAMILY_EXPECTED_VAR[cloud_resource_container_image]="cloud_resource_container_image_expected_edges"

# go/internal/storage/cypher/cloud/aws/image/container_edge_writer.go:72-75
# reads `MERGE (source)-[rel:%s]->(target)`. The anchor is matched against
# EXECUTED statement text, so it carries the INTERPOLATED type: the %s is
# filled per row from the writer's closed single-member vocabulary, whose
# only member is lambda_function_uses_image, so the anchor pins the one
# type this family's writer can emit (AWS_lambda_function_uses_image).
# Pinning the literal `%s` would match no executed statement and the
# scripted graph-write fault would never fire. It is NOT
# AWS_LAMBDA_FUNCTION_USES_IMAGE, the writer's statement-metadata label.
IFA_FAMILY_ANCHOR[cloud_resource_container_image]="MERGE (source)-[rel:AWS_lambda_function_uses_image]->(target)"
# custom, same dispatch shape as rows/12-13: hand-written cells, dispatched by
# name. The generic table_lock path has never run live and its mandatory
# precondition assumes a `domain` column fact_records does not have
# (scripts/lib/ifa_fault_generic_table_lock.sh's header).
IFA_FAMILY_CELL_KIND[cloud_resource_container_image]="custom"

# Not a shared_intent_lock family, so no retry baseline is required. Declared
# empty rather than omitted -- see rows/12 for why an absent key is worse.
IFA_FAMILY_RETRY_BASELINE_VAR[cloud_resource_container_image]=""

# 0 on the same footing as rows/12-13: drive_all_cassettes does not produce
# this family; each fault cell drives it through DRIVE_FN/CASSETTE_VAR
# instead.
IFA_FAMILY_FAULT_SHARED_DRIVE[cloud_resource_container_image]="0"

IFA_FAMILY_HANDLER_GO_FILE[cloud_resource_container_image]="go/internal/reducer/awscloud/aws_cloud_image_materialization.go"

IFA_FAMILY_NAMES+=(cloud_resource_container_image)
