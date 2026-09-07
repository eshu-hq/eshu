// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workloadid builds the canonical graph identifiers for Workload and
// WorkloadInstance nodes.
//
// The identifiers are distinct types with exactly one constructor each, so the
// compiler rather than a hand-maintained list enumerates every site that builds
// one. That matters because the format is due to change: it is currently built
// from the workload name alone, so two repositories with a same-named workload
// collapse onto one node (#5385). Both constructors already take the repository
// id and ignore it, which makes the re-key an edit inside this package rather
// than an edit at every caller.
//
// The compiler's enumeration is complete for the typed value only. It covers
// the reducer write path, which is where projected graph truth is decided; it
// does not cover callers elsewhere that still concatenate the prefix by hand.
// The package README names those and explains why converting them belongs to
// the re-key rather than to this step.
//
// The package is a leaf by design and must stay one. It mirrors
// internal/repositoryidentity, which owns the canonical repository id for the
// same reason. Keeping identity construction in a dependency-free package means
// any layer can build an id without taking on the reducer's dependency closure,
// and it survives the reducer package split tracked under #6053.
package workloadid
