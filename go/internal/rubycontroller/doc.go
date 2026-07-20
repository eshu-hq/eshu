// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rubycontroller holds the single, shared Rails-controller
// superclass-chain decision used by both the Ruby parser (same-file, per-file
// registry) and the reducer code-root verdict builder (repo-wide, multimap
// registry). Keeping one copy of the decision — accepted-base set, suffix
// fallbacks, and keep-biased asymmetries — is a hard requirement of issue
// #5376: the reducer downgrade rule must re-run the identical decision the
// parser used to root a controller action, only with fuller ancestry, so the
// two can never drift into disagreeing about what a Rails controller is.
//
// The decision is keep-biased by design. For a dead-code tool a false negative
// ("still call it live") is far cheaper than a false positive that recommends
// deleting reachable code, so every inconclusive outcome (fizzle, cycle, depth
// cap, unresolved Controller-suffixed base) resolves toward keeping the root.
// A downgrade is returned only on positive evidence: every resolved path from
// the class ends at a declared base that is neither an accepted Rails base nor
// a Controller-suffixed name.
package rubycontroller
