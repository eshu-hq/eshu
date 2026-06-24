// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchdocs defines the curated search-document contract for Eshu's
// search lane.
//
// The package projects already-indexed content and read-model summaries into
// bounded documents that can later feed BM25, vector, or hybrid retrieval. It
// deliberately stays separate from graph writes and public API handlers:
// projected documents are derived evidence, and graph expansion must use the
// stable handles carried on each Document.
package searchdocs
