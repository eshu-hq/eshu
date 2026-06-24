// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package proofofvalue measures whether an agent answers IaC reachability
// questions more accurately with Eshu's reachability analysis than with a
// baseline that only has plain text search ("grep").
//
// The package is a pure scoring and aggregation library. It takes per-question
// predictions from two strategies (baseline and Eshu) plus ground truth, and
// produces honest accuracy, precision, recall, and miss counts for each
// strategy along with the with-minus-without delta. It computes no answers
// itself: callers supply real predictions produced by real tools over a real
// fixture corpus, so the reported delta is reproducible and cannot be
// fabricated by this package.
//
// The companion runner in go/cmd/proof-of-value wires this scorer to the
// existing dead-IaC product-truth fixture corpus
// (tests/fixtures/product_truth/dead_iac) and its ground truth
// (tests/fixtures/product_truth/expected/dead_iac.json), running the baseline
// text-search strategy and the real internal/iacreachability analyzer over the
// same files on disk.
package proofofvalue
