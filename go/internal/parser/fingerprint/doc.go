// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fingerprint computes language-generic tree-sitter leaf-walk
// fingerprints over function body nodes for the code-divergence report
// (epic #6833, theory proof in
// docs/internal/evidence/6834-code-divergence-theory.md).
//
// A fingerprint captures four fields: the exact token stream hash
// (comments excluded), the alpha-renamed hash (identifiers and literals
// replaced by positional placeholders), a 128-register MinHash sketch over
// 5-token shingles with 32×4 LSH bands, and the leaf token count.
//
// Full tiers (renamed + sketch) exist for Go, Python, TypeScript, TSX,
// JavaScript, and Java, mirroring production function-entity emission.
// Every other grammar is exact-only: exact hash plus token count, no renamed
// hash, sketch, or bands.
package fingerprint
