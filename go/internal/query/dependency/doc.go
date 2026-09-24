// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package dependency serves GET /api/v0/dependencies, the bounded
// package-dependency inventory.
//
// Handler reads the package-native dependency chain from the authoritative
// graph (Package -> PackageVersion -> PackageDependency -> Package) in one of
// two directions: forward ("what does package X depend on") or reverse ("who
// depends on package X"). Reverse requires a package anchor. Both directions
// page with a keyset cursor (after_name, after_edge) and cap a page at 200
// rows, probing one extra row to report truncation. The route answers only on
// a profile with an authoritative graph; elsewhere it returns an
// unsupported-capability error rather than an empty page.
//
// The handler never returns repository ownership truth. That stays a reducer
// correlation concern served elsewhere.
//
// Capability and Support declare the route's capability row once; the
// capability matrix in internal/query/contract registers Support() for
// production and main_test.go registers it for this package's own tests.
//
// ForwardCypher is exported so the root query package's plan-binding test can
// pin the forward traversal text; production callers go through Handler.
//
// The package moved out of the root query package for #6642. Root keeps a
// DependenciesHandler alias (package_registry_alias.go) for cmd/api until the
// #6642 alias sweep.
package dependency
