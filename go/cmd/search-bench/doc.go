// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command search-bench runs the design-430 search-lane benchmark over a live
// Eshu content corpus.
//
// It loads content entities and files for one repository from the Postgres
// content store, projects them into curated search documents with the shared
// searchdocs projection, and measures keyword-retrieval latency for the current
// Postgres content-search baseline against the in-process curated hybrid lane
// (internal/searchhybrid) over that real corpus.
//
// The benchmark reports real measured latency, index build cost, and curated
// corpus shape. When supplied with a validated labeled query suite it also runs
// corpus-cap sweeps and reports measured recall, precision, nDCG, overflow, and
// false-canonical-claim counts. It does not fabricate the NornicDB search-lane
// arm: the canonical NornicDB runs search-disabled per design 430, and no
// search-enabled curated NornicDB deployment exists yet.
package main
