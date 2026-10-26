// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package epoch owns the git collector's content epoch ledger (#9097).
//
// The git freshness hint folds one content version string, and the ingestion
// store treats an equal hint and commit as a duplicate generation and stores
// nothing (#7978). That string must move whenever the collector or a parser
// emits different facts for unchanged source, or the new output is dropped
// (ruling a9-117 d6). It used to be a hand-bumped constant; two branches that
// bumped it to the same value for different output could both merge cleanly.
//
// The version now names the epoch of a committed ledger, ledger.tsv, which
// this package embeds and Current reads once per process. The ledger has one
// Bucket per file of the git package's testdata/content_version fixture (input
// digest, fact count, output digest) plus RepositoryBucket for facts that name
// no single file, and one epoch line. The git package's gate test computes the
// buckets and calls Repin, which advances the epoch only when some bucket kept
// its input digest and changed its output digest, or when forced. The advanced
// epoch chains from the previous one, so two branches that both advance from
// one parent with different outputs conflict on the epoch line and re-pin to a
// third value. A fixture-only change keeps the epoch and every deployed hint.
//
// The epoch line also carries a seal over the bucket lines. A ledger whose
// bucket lines were merged by hand under one side's epoch line is not Sealed,
// so Drift reports it and Repin refuses it: re-pin from origin/main's copy.
//
// Current panics on a malformed embedded ledger instead of folding a wrong
// value into every hint. This is a leaf: it imports no Eshu package, and
// package git imports it.
package epoch
