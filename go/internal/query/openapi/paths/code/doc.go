// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package code holds the OpenAPI path fragments for the code-search and
// static-analysis routes: symbol search (Symbols), code quality signals
// (Quality), security-relevant call paths (Security), route-to-caller
// tracing (RouteToCaller), the call/reference graph (Graph), data-flow
// tracing (Flow), code ownership (Owners), the top-level code routes
// (Routes), and dead-code detection, which spans two constants in one file
// (DeadCodeScan for the per-repository scan and, in dead_code.go,
// DeadCodeInvestigation for a single investigation plus CrossRepoDeadCode
// for the cross-repository rollup — one file, two fragments, not the
// one-constant-per-file shape every other file in this package follows).
//
// Each remaining file holds exactly one exported string constant.
// openapi/spec.go concatenates all ten identifiers. This package MUST NOT
// import the openapi parent.
package code
