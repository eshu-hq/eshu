// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package testutil shares the metric-reading test helpers the
// maintenance leaf test packages need in common.
//
// Go test files cannot share unexported symbols across a package
// boundary, so [CounterValue], [GaugeValue], and [HasAttrs] live here
// as an exported leaf instead of being duplicated per leaf test
// package (#7648). Test-only: no production code may import this
// package.
package testutil
