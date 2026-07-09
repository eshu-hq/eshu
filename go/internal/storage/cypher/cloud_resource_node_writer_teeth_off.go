//go:build !ifadeterminismteeth

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// teethCloudResourceUpsertExtraSet is empty in every normal build: no
// production, CI, or default-tag build ever appends the ifadeterminismteeth
// fault's extra SET clause to canonicalCloudResourceUpsertCypher. See
// cloud_resource_node_writer_teeth.go (tag: ifadeterminismteeth) for the
// counterpart this file's build tag excludes, and issue #4396's determinism
// matrix "--teeth" mode (scripts/verify-ifa-determinism.sh) for why this
// build-tag split exists at all.
const teethCloudResourceUpsertExtraSet = ""
