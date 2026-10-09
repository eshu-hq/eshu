// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package testfixtures shares the fixture helpers the storage/postgres
// test packages need in common.
//
// The root package tests (package postgres) and the activation package
// tests (package activation_test) cannot share unexported helpers, so
// each side grew its own copy of the same fixtures. This package merges
// the proven twins — byte-identical, or differing only in package
// qualification — behind one exported spelling (#7648): catalog scope
// and generation builders, the fact channel, the disposable proof DSN,
// scope seeders, and the activation obligation live-proof helpers.
// Test-only: no production code may import this package.
package testfixtures
