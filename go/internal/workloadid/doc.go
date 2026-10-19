// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workloadid builds the canonical graph identifiers for Workload and
// WorkloadInstance nodes.
//
// The identifiers are distinct types with exactly one constructor each, so the
// compiler rather than a hand-maintained list enumerates every site that builds
// one. That matters because the format used to be built from the workload name
// alone, so two repositories with a same-named workload collapsed onto one
// node (#5385). Both constructors take the repository id and scope the key by
// it, which kept the re-key an edit inside this package rather than an edit
// at every caller.
//
// The compiler's enumeration is complete for the typed value only. It covers
// the reducer write path, which is where projected graph truth is decided; it
// does not cover the read-side handle builders that concatenate the public
// `workload:<name>` prefix by hand. Those build handles, not node ids, and
// the handle format did not move with the re-key; the package README names
// each one and records why it stays.
//
// The package is a leaf by design and must stay one. It mirrors
// internal/repositoryidentity, which owns the canonical repository id for the
// same reason. Keeping identity construction in a dependency-free package means
// any layer can build an id without taking on the reducer's dependency closure,
// and it survives the reducer package split tracked under #6053.
package workloadid
