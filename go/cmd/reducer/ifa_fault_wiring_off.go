// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifafaultinjection

package main

import (
	"log/slog"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// wrapIfaFaultExecutor is a no-op in every normal build: it returns inner
// unchanged and never inspects getenv or logger. See ifa_fault_wiring.go
// (tag: ifafaultinjection) for the counterpart this build tag excludes --
// the real ESHU_IFA_FAULT_SCRIPT wiring (issue #4580 P6 S4) that no
// production, CI, or default-tag build ever reads or links. Mirrors
// gcp_resource_materialization_teeth_off.go's tag-split call-site pattern:
// buildReducerService calls this function unconditionally in every build
// (see main.go), and only the function BODY differs by tag.
func wrapIfaFaultExecutor(inner sourcecypher.Executor, _ func(string) string, _ *slog.Logger) (sourcecypher.Executor, error) {
	return inner, nil
}
