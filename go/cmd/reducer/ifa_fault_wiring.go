// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ifaFaultScriptEnv names the fault-script path environment variable. Set
// only under the ifafaultinjection build tag; reading it (and the
// sourcecypher.FaultingExecutor decorator it wires in) is unreachable from
// any untagged build (see ifa_fault_wiring_off.go). This is issue #4580
// Layer 4 / P6 slice S4's in-binary fault-injection entry point for the
// (separate, deferred) Docker gate verify-ifa-fault-injection.sh.
const ifaFaultScriptEnv = "ESHU_IFA_FAULT_SCRIPT"

// ifaFaultSentinelSuffix names the restart-backend-between-phase-groups
// sentinel file this wiring derives deterministically from the fault-script
// path: <script path>.restart-sentinel. This is a fixed convention rather
// than a second environment variable so a fault run stays fully described by
// one script path; the (deferred) gate script that restarts the graph
// backend and deletes the sentinel is expected to derive the same path the
// same way.
const ifaFaultSentinelSuffix = ".restart-sentinel"

// wrapIfaFaultExecutor wraps inner with the in-binary fault decorator when
// ESHU_IFA_FAULT_SCRIPT names a fault script, reading and validating it via
// faultreplay.Load. It returns inner unchanged when the env var is unset (the
// common case for every normal run of a tagged binary). Errors here are
// fail-closed: an operator who sets the env var to a bad path or an invalid
// script gets a startup error, never a silently-ignored fault script.
func wrapIfaFaultExecutor(inner sourcecypher.Executor, getenv func(string) string, logger *slog.Logger) (sourcecypher.Executor, error) {
	path := strings.TrimSpace(getenv(ifaFaultScriptEnv))
	if path == "" {
		return inner, nil
	}
	script, err := faultreplay.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load %s=%q: %w", ifaFaultScriptEnv, path, err)
	}
	faulting, err := sourcecypher.NewFaultingExecutor(inner, script, path+ifaFaultSentinelSuffix)
	if err != nil {
		return nil, fmt.Errorf("build ifa faulting executor from %s=%q: %w", ifaFaultScriptEnv, path, err)
	}
	if logger != nil {
		logger.Warn(
			"ifa fault injection enabled for reducer graph writes",
			"fault_script", path,
			"fault_count", len(script.Faults),
		)
	}
	return faulting, nil
}
