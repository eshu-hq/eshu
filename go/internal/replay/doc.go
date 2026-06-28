// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package replay holds the shared core of Eshu's deterministic replay
// framework: the canonical serialization used by every recorder and validator,
// and the Source interface every replay flavor implements.
//
// Canonicalization is the load-bearing primitive. A recorder that wrote raw
// live output would churn the whole fixture on every refresh — timestamps,
// generation ids, and Go map iteration order all vary run to run. Canonicalize
// removes that churn so a recorded fixture is stable, reviewable, and
// byte-identical when re-derived from equivalent input: it sorts every object
// key, collapses configured volatile fields to fixed sentinels, stably orders
// recorded arrays, redacts configured secret keys, and is idempotent.
//
// Replay flavors live in subpackages (replay/cassette today) so new flavors
// slot in without reshaping the core.
package replay
