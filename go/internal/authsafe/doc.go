// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package authsafe holds the request-value checks that sign-in flows share.
//
// A sign-in flow reads its post-login destination from the request, so the
// value is attacker-controlled by construction. The checks here decide whether
// such a value may be used, and they live in one package because they were
// previously duplicated across every connector that needed them — three
// byte-identical copies of the return-path check alone (#5388). A security
// check with copies is a check that gets tightened in one place.
//
// The package is deliberately dependency-free and holds no state: everything
// here is a pure function of its input, so a connector can call it without
// wiring, and a test can cover the whole surface without a fixture.
package authsafe
