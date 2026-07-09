//go:build !ifadeterminismteeth

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// ifaTeethStampCloudResourceRow is a no-op in every normal build: it neither
// mutates row nor allocates. See gcp_resource_materialization_teeth.go (tag:
// ifadeterminismteeth) for the counterpart this build tag excludes — the
// build-tag-gated fault (stamping both ifa_teeth_seq, a process-global
// sequence counter, and ifa_teeth_write_order, a wall-clock floor) that makes
// scripts/verify-ifa-determinism.sh --teeth's matrix go red on purpose
// (issue #4396's acceptance clause).
func ifaTeethStampCloudResourceRow(map[string]any) {}
