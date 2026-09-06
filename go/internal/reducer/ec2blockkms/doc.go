// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ec2blockkms derives conservative EC2 block-device KMS encryption
// posture and writes it as reducer-owned properties on existing EC2
// CloudResource nodes (issue #1304).
//
// The domain is additive and gates on a DUAL readiness lookup rather than a
// graph round trip: the EC2 instance-node canonical-nodes phase (published by
// internal/reducer's ec2_instance_node_materialization) and the EBS/KMS
// CloudResource canonical-nodes phase (published by the aws_resource
// materialization). A miss on either phase is retryable
// ([EC2BlockDeviceKMSPostureNodesNotReadyFailureClass]), never a property
// write against a node set that has not committed yet. Node existence is
// fenced entirely by that gate: the handler never creates a CloudResource
// node, only matches by uid.
//
// A posture's block-device volume ids are joined against a bounded in-memory
// index built from the scope generation's aws_resource (EBS volume, KMS key)
// and aws_relationship (ec2_volume_uses_kms_key) facts, mirroring the AWS
// relationship edge join (#805). Missing or ambiguous evidence -- an
// unresolved volume, a detached or mismatched attachment, an AWS-managed or
// default KMS key, a conflicting volume or KMS-relationship fact -- produces
// state=unknown with a specific reason rather than a silently wrong
// encrypted/not_encrypted classification. A malformed aws_resource,
// aws_relationship, or ec2_instance_posture fact is quarantined per-fact
// (input_invalid) while every valid fact in the same batch still projects.
//
// Rows are deduplicated by EC2 instance uid and sorted, so a batched write is
// byte-stable and idempotent across retries and reprojections.
//
// The exported surface is [MaterializationDomainDefinition],
// [EC2BlockDeviceKMSPostureNodeWriter],
// [EC2BlockDeviceKMSPostureMaterializationHandler],
// [ExtractEC2BlockDeviceKMSPostureRows], and
// [EC2BlockDeviceKMSPostureNodesNotReadyFailureClass]. This package never
// imports internal/reducer.
package ec2blockkms
