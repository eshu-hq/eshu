// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cpubudget computes the CPU count worker-pool defaults across the
// codebase should size against, in place of the HOST cpu count
// runtime.NumCPU() reports.
//
// On Go 1.25+, the runtime itself already sets GOMAXPROCS from the container
// cgroup CPU quota (the containermaxprocs GODEBUG, default-on since Go
// 1.25), so runtime.GOMAXPROCS(0) already reflects the quota-derived core
// count inside a cgroup-limited container. UsableCPUs is a thin wrapper over
// runtime.GOMAXPROCS(0), floored at 1 — not a reimplementation of cgroup
// reading — and is the function every worker-count default should call in
// place of runtime.NumCPU(). A go.mod-version guard test enforces the
// assumption this relies on: this module's `go` directive must stay at 1.25
// or newer. The package imports only the standard library so any package in
// the module tree can depend on it without risking an import cycle.
//
// Two worker-sizing sites under go/internal/parser are a deliberate,
// temporary exception and are not yet routed through UsableCPUs — see
// README.md's "Deferred: internal/parser" section for why.
package cpubudget
