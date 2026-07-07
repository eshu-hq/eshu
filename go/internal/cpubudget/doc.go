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
// go/internal/parser's go_package_interface_prescan.go worker-sizing site,
// a deliberate temporary exception, is routed through UsableCPUs as of #4759.
// interproc/solve.go is intentionally left on runtime.GOMAXPROCS(0) (stdlib-only
// package, already cgroup-aware) — see README.md's "Formerly deferred:
// internal/parser" section.
package cpubudget
