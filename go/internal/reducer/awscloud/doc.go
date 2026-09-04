// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awscloud projects AWS cloud facts into two canonical truth
// surfaces: the CloudResource -> ContainerImage graph edge (issue #5450) and
// the aws_cloud_runtime_drift orphan/unmanaged finding (issue #39, #5848 CAS
// admission).
//
// The family owns two handlers. [AWSCloudImageMaterializationHandler],
// registered additively through [ImageMaterializationDomainDefinition],
// resolves a lambda_function_uses_image aws_relationship fact's source
// endpoint through the shared cloudjoin.CloudResourceJoinIndex and its
// resolved_image_uri attribute directly to a :ContainerImage node uid, then
// filters the result to targets [containerimage.ContainerImageExistenceLookup]
// confirms actually exist before counting an edge as materialized (issue
// #5450 P1 follow-up). [AWSCloudRuntimeDriftHandler] evaluates joined
// AWS/Terraform-state/Terraform-config evidence through the
// internal/correlation/drift/cloudruntime rule pack and publishes admitted
// candidates through a begin-before-mutate, fencing-token-ordered admission
// check ([AWSCloudRuntimeDriftTx], [PostgresAWSCloudRuntimeDriftWriter]) so a
// stalled worker's stale evidence can never overwrite a fresher worker's
// committed finding.
//
// Both handlers are additive rather than default: the image handler needs an
// explicitly wired [CloudResourceContainerImageEdgeWriter] and fact loader,
// and the drift handler needs an [AWSCloudRuntimeDriftEvidenceLoader], an
// [AWSCloudRuntimeDriftFindingWriter], and an
// [AWSCloudRuntimeDriftFencingTokenIssuer]; registering either without its
// required adapters would accept intents and drop every one, or hard-error on
// every Handle call.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches [reducercontract]
// (aliased as reducercontract), [cloudjoin], [containerimage], [factdecode],
// [factload], [factwrite], [gpphase], [payloadcore], [schemadecode],
// internal/correlation/drift/cloudruntime and its engine/model/rules
// siblings, internal/facts, internal/telemetry, internal/truth, and the
// factschema SDK, and never the parent internal/reducer package. The reducer
// root keeps compatibility aliases in aws_cloud_family_compat.go so the
// reducer command, internal/storage/postgres, and
// internal/replay/costcounting compile unchanged; that direction is root
// importing this family, never the reverse. See AGENTS.md in this directory
// before adding an import.
//
// # Observability
//
// The image handler emits eshu_dp_aws_cloud_image_edges_total (by
// resolution_mode), counting only edges [ExtractAWSCloudImageEdgeRows]
// resolved AND [containerimage.ContainerImageExistenceLookup] confirmed
// materialized. The drift handler's admitted-candidate counts flow through
// internal/correlation/drift/cloudruntime.RecordEvaluation onto that
// package's own instruments; a readiness defer or a superseded write
// classifies as a retryable, non-counting failure class
// ([AWSCloudRuntimeDriftStatePendingFailureClass],
// [AWSCloudRuntimeDriftWriteSupersededFailureClass]) surfaced through the
// existing eshu_dp_reducer_retry_surge_total{failure_class}. Facts rejected
// for a malformed payload increment the shared
// eshu_dp_reducer_input_invalid_facts_total counter, and both handlers' runs
// stay covered by eshu_dp_reducer_executions_total and
// eshu_dp_reducer_run_duration_seconds.
package awscloud
