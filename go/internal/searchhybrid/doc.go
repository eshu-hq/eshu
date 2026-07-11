// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchhybrid is a pure-Go hybrid retrieval backend over the curated
// design-430 search-document lane.
//
// It indexes searchdocs.Document records and serves bounded keyword (BM25),
// semantic (vector), and hybrid (Reciprocal Rank Fusion of BM25 and vector)
// retrieval through the searchretrieval.Backend port, with no hosted API
// dependency in this package. Embeddings are optional and supplied through the
// Embedder port by callers; when no embedder is configured the index serves BM25
// only and hybrid fusion degenerates to the BM25 ranking. Searchable text is
// normalized to valid UTF-8 before hashing or embedding, and embeddings are
// cached by content hash so an unchanged document is not re-embedded.
//
// Semantic retrieval is served through an index-owned vector retriever. Exact
// cosine remains the deterministic zero-value correctness baseline. The
// approximate mode is an explicitly selected pure-Go angular-LSH ANN candidate
// index; it computes exact cosine for retrieved candidates and falls back to
// exact retrieval when its scoped candidate set is empty.
//
// The index enforces a hard cap on indexed document count and signals overflow
// explicitly, and retrieval returns deterministic top-K results for fixed
// inputs. Search rank and score stay derived retrieval evidence; nothing here
// writes the canonical graph or promotes a score to canonical truth.
package searchhybrid
