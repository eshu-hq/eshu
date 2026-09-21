// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package guardset derives the AWS scanner guard set from the repository
// layout rather than from a hardcoded list. It is test-support code shared by
// the runtime and bindings guard tests.
//
// The expected scanner set has two independent sources that must agree:
//
//  1. The set of service bind directories on disk
//     (services/<service>/bind/), which is the set of scanners that
//     SHOULD be wired into the runtime. BindServiceDirs reads it.
//  2. The set of service tokens parsed from the blank imports in
//     runtime/bindings/all.go, which is the set of scanners that ARE
//     wired into the runtime. BindingsImportServices reads it.
//
// Diff compares the two. A non-empty missing result means a
// services/<service>/bind/ package exists without a matching
// all.go import, which is the unwired-scanner failure the guard exists to
// catch. Neither source derives from runtime.SupportedServiceKinds(), so the
// guard stays honest and is not tautological. The package intentionally has no
// dependency on the runtime registry.
package guardset
