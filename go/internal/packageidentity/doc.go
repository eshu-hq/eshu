// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packageidentity normalizes package coordinates across package
// managers without claiming repository ownership or vulnerability impact.
//
// The package keeps ecosystem rules explicit for registries such as npm, PyPI,
// Maven, NuGet, Cargo, SwiftPM, and Pub. Unknown aliases fail closed so
// collectors and reducers cannot silently join unrelated package facts.
package packageidentity
