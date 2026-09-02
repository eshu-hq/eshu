// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awscloudimage builds the AWS cloud-image materialization reducer
// intent from one immutable scope generation: when the generation carries any
// aws_resource fact, it asks the reducer to project the generation's
// lambda_function_uses_image aws_relationship facts into canonical
// CloudResource -> ContainerImage graph edges (issue #5450). The trigger is
// aws_resource presence — the persistent every-generation AWS signal — not
// relationship presence, so a generation where a Lambda's image relationship
// disappeared still enqueues and the reducer handler's retract-first logic
// retracts the stale prior edge. The intent shares the
// aws_resource_materialization entity key with the AWS node builders so the
// edge handler gates on the same canonical-nodes-committed phase and never
// projects an edge before its source node exists. Root projector assembly owns
// lookup construction and lifetime, invocation order, queue writes, retries,
// and telemetry.
package awscloudimage
