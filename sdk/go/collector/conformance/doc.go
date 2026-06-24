// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package conformance is the public, out-of-tree-runnable collector conformance
// harness for Eshu component packages.
//
// It validates an out-of-tree collector package's manifest proof metadata and
// its collector-sdk/v1alpha1 result fixtures against the manifest-derived host
// contract, then emits a stable machine-readable Report. The package performs
// no file or network I/O and imports no Eshu internal packages, so an external
// collector repository can run conformance in its own CI by importing only
// github.com/eshu-hq/eshu/sdk/go/collector and this package.
//
// The in-tree extension host re-exports this report contract so the same
// verdict is produced inside and outside the Eshu monorepo.
package conformance
