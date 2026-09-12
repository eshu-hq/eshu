// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package code holds the OpenAPI path fragments for the code-search and
// static-analysis routes: symbol search (Symbols), code quality signals
// (Quality), security-relevant call paths (Security), route-to-caller
// tracing (RouteToCaller), the call/reference graph (Graph), data-flow
// tracing (Flow), code ownership (Owners), and the top-level code routes
// (Routes). Dead-code detection (Investigation, Scan, CrossRepo) lives in
// the dead subpackage.
//
// Each file here holds exactly one exported string constant.
// openapi/spec.go imports both this package and dead and concatenates all
// ten identifiers between them. This package MUST NOT import the openapi
// parent.
package code
