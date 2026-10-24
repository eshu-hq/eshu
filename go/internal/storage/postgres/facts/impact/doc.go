// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factimpact is the supply-chain impact active-evidence branch
// catalog (#8880). One Go table generates every id-resolution statement
// FactStore.ListActiveSupplyChainImpactFacts issues, the fact_records index
// each statement plans on, the foreign-key guard predicate, and the text of
// migrations 217 to 229, so the statements, the index predicates, and the
// migrations cannot disagree.
//
// The catalog replaces one statement that walked every active generation's
// facts on every 500-row page. Each branch reads fact_records only through a
// key-leading or kind-partial anchor in a MATERIALIZED candidate CTE, applies
// the pre-#8880 active-generation join once per distinct (scope, generation)
// pair, and returns (fact_id, fact_kind). The loader dedupes and orders the
// IDs in Go and fetches payloads by primary key.
//
// Kinds are classed by the #8880 census (Kinds): growth kinds get one key
// probe per contract-carried key, with every other key cell routed to the
// foreign-key guard; bounded kinds get one full-filter statement; file gets
// the JS/TS repo_id probe and a resolved-scope probe. The union over every
// statement the loader issues equals the pre-#8880 row set; the live
// differential in the parent package pins that against the frozen statement.
//
// The package is a leaf: it imports no storage package and runs no SQL.
package factimpact
