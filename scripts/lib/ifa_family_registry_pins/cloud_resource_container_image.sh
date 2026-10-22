#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # consumed by test-ifa-family-registry-derived-pins-cases.sh after sourcing this file
# cloud_resource_container_image hand-derived pin (#6228 slice D live leaf).
# Sourced by scripts/lib/test-ifa-family-registry-derived-pins-cases.sh --
# read that file's header before touching this one. Every value is
# HAND-TYPED, derived from the citations inline, never read back out of the
# registry row.

# go/internal/reducer/awscloud/aws_cloud_image_materialization.go declares
# `FactLoader factload.FactLoader` as a struct field at :100, Handle rejects
# a nil one at :134-136, and passes it to factload.LoadFactsForKinds at
# :163. An ACCESS EXCLUSIVE lock on fact_records therefore blocks a read
# this handler really performs -- the blocker_kind this family's fault cell
# takes.
#
# A DIRECT-materialization family, so there is no shared_projection_intents row
# for shared_intent_lock to take.
IFA_FAMILY_PIN_BLOCKER_KIND="table_lock:fact_records"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
# The fact_work_items.domain the projector fans this family out under, taken
# from BuildMaterializationReducerIntent
# (go/internal/projector/cloud/aws/image/materialization_intents.go:69), and
# corroborated by the domain definition
# (DomainAWSCloudImageMaterialization in
# go/internal/reducer/contract/domain.go:17).
IFA_FAMILY_PIN_WAIT_KEY="aws_cloud_image_materialization"

# go/internal/storage/cypher/cloud/aws/image/container_edge_writer.go:72-75
# reads `MERGE (source)-[rel:%s]->(target)`. The anchor is matched against
# EXECUTED statement text as a SUBSTRING, so it carries the INTERPOLATED
# type: the %s is filled per row from the writer's closed single-member
# vocabulary, and AWS_lambda_function_uses_image is the only type that
# template can emit. A `%s` form would match no executed statement, the
# scripted graph-write fault would never fire, and the cell would report
# green having tested nothing. It is NOT AWS_LAMBDA_FUNCTION_USES_IMAGE,
# the writer's statement-metadata label.
IFA_FAMILY_PIN_ANCHOR="MERGE (source)-[rel:AWS_lambda_function_uses_image]->(target)"
# shared_cell: a plain reducer family needing no maintenance pass, so it is
# driven in the determinism gate's shared N={1,2,4} cell.
IFA_FAMILY_PIN_SHARED_CELL=1
# cell_kind: derived from the gate's call sites -- the custom trio in
# scripts/lib/ifa_fault_injection_cloud_resource_container_image_cells.sh,
# dispatched by name (#6228 slice D live leaf). Custom rather than generic
# for the reason recorded in the sibling iam_instance_profile_role pin.
IFA_FAMILY_PIN_CELL_KIND="custom"
