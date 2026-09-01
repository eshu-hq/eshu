// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workloadcloud builds the workload-cloud-relationship reducer intent
// from one immutable scope generation: promoting exact workload anchors on
// aws_resource facts into WorkloadInstance USES CloudResource graph edges.
// Root projector assembly owns lookup construction and lifetime, invocation
// order, queue writes, retries, and telemetry.
package workloadcloud
