// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifafaultinjection

package cypher

import "github.com/eshu-hq/eshu/go/internal/replay/faultreplay"

// NewFaultingExecutor is a no-op in every normal build: it ignores script
// and sentinelPath entirely and returns inner unchanged. See
// fault_executor.go (tag: ifafaultinjection) for the counterpart this build
// tag excludes -- the build-tag-gated in-binary fault decorator (issue #4580
// P6 S4) that makes go/cmd/reducer inject fail-graph-write-once-then-succeed
// and restart-backend-between-phase-groups faults for the (deferred) Docker
// gate verify-ifa-fault-injection.sh. No production, CI, or default-tag
// build ever links FaultingExecutor or references
// go/internal/replay/faultreplay beyond the Script type named in this
// signature, so this decorator costs nothing outside the opt-in tag. Mirrors
// cloud_resource_node_writer_teeth_off.go's tag-split pattern for issue
// #4396's determinism-matrix teeth.
func NewFaultingExecutor(inner Executor, _ faultreplay.Script, _ string) (Executor, error) {
	return inner, nil
}
