// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awsrelationship builds the AWS relationship materialization reducer
// intent from one immutable scope generation: when aws_relationship evidence
// is present, it asks the reducer to join the generation's relationship facts
// against committed CloudResource nodes and write canonical AWS relationship
// graph edges. The intent shares the aws_resource_materialization entity key
// with the AWS node builders so the edge handler gates on the same
// canonical-nodes-committed phase and never projects an edge before its
// endpoints exist. Root projector assembly owns lookup construction and
// lifetime, invocation order, queue writes, retries, and telemetry.
package awsrelationship
