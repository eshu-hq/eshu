// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package payloadstore encodes and decodes the JSONB fact payload column and
// the small set of empty-string-to-SQL-binding helpers every fact, queue,
// relationship, and collector writer in the parent postgres package shares.
//
// MarshalPayload turns a fact payload map into Postgres-JSONB-safe JSON,
// stripping \u0000 escapes and raw control bytes that Postgres JSONB rejects
// while preserving literal backslash-escaped source text that merely
// resembles a null escape. UnmarshalPayload reverses the encoding, returning
// a nil map (not an error) for empty input or an empty decoded object.
// EmptyToNil and EmptyToDefault normalize optional string fields for SQL
// binding and for the version-less-fact sentinel respectively.
//
// This package is a leaf: it must not import the parent postgres package or
// any other storage/postgres family, so every caller — facts, both queues,
// relationships, and the collector — can import it without a cycle.
package payloadstore
