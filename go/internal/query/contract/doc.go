// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contract declares the capability support rows the query surface
// answers under: for each capability, the maximum truth level it can reach on
// each runtime profile, and the profile it requires at all.
//
// Every row registers itself through querycontract.RegisterCapabilities in an
// init(), and package query blank-imports this package to link them in. Root
// reads the assembled registry; it no longer writes it.
package contract
