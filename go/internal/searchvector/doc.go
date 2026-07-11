// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchvector builds persisted vector rows for curated Eshu
// search documents.
//
// The package reads active search-document rows through a caller-supplied
// document store, embeds the shared searchhybrid document text with a
// caller-supplied Embedder, and writes derived vector metadata and values
// through caller-supplied stores. Persisted rows retain the document store's
// content-hash token so pending selection and completed writes use one identity.
// Production build requests also carry the current projection revision and
// vector-scope build fence so Postgres can reject superseded worker writes.
// Callers may supply per-document admission for
// provider-backed builds; denied documents are marked disabled without vector
// values. It performs no graph writes, no provider profile loading, no API/MCP
// routing, and no queue scheduling.
package searchvector
