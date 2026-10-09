// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package accepted gates repo-dependency graph-projection authority on the
// relationship generation being active (published) in Postgres.
//
// [GateOnActive] and [GatePrefetchOnActive] decorate an accepted-generation
// lookup so an acceptance row only grants graph-projection authority once the
// generation it names is also active in Postgres, closing the dual-write
// graph-ahead-of-Postgres window for the repo-dependency lane. The fence
// applies ONLY to source runs that carry relationship generation IDs
// ("repo_dependency" or "repo_dependency:<scope>"); code-import and
// package-consumption source runs carry scope generation IDs that never
// appear in relationship_generations and always bypass the fence (B-13).
//
// [Lookup] and [Prefetch] are type aliases over the same unnamed signatures
// the reducer root's own AcceptedGenerationLookup/AcceptedGenerationPrefetch
// define as named types, so this leaf never imports internal/reducer (issue
// #6061). The lookup alone interoperates freely with a root-typed value;
// Prefetch nests Lookup as its return type, where the two packages' named
// return types are no longer identical, so the one call site that crosses
// the boundary (cmd/reducer) adapts with two thin wrapper closures.
// [RelationshipGenerationActiveLookup],
// [RelationshipGenerationsCompleteLookup], and
// [RelationshipGenerationsIncompleteScopesLookup] are plain function types
// the Postgres read models implement; [IncompleteScopeIDs] best-effort
// resolves the scopes holding the corpus fence for a deferral error.
package accepted
