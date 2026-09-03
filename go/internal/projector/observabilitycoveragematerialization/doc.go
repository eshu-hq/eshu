// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package observabilitycoveragematerialization builds the
// observability-coverage-materialization reducer intent from one immutable
// scope generation: when the generation carries any AWS-native observability
// aws_resource fact (a CloudWatch alarm, composite alarm, dashboard, logs log
// group, or X-Ray sampling rule/group), it asks the reducer's
// observability_coverage_materialization domain to project that generation's
// exact coverage decisions into canonical COVERS graph edges (issue #391 PR3).
// The intent anchors to the earliest such fact in original input order so the
// reducer claim is stable across reprojections, and it reuses the
// "aws_resource_materialization:<scope>" entity key so the edge handler's
// readiness gate resolves the same canonical-nodes-committed row the AWS node
// builders publish — coverage edges never project before the CloudResource
// nodes commit. Root projector assembly owns lookup construction and lifetime,
// invocation order, queue writes, retries, and telemetry.
//
// The sibling package observabilitycoverage owns the separate
// observability_coverage_correlation family, whose trigger is wider (any
// observability source fact as well as the AWS objects) and whose entity key
// is family-distinct.
package observabilitycoveragematerialization
