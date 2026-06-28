// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package schema is the discoverable, machine-readable contract for the v1
// cassette envelope format, plus a fast offline validator for committed
// cassettes.
//
// CassetteFormatV1 builds the JSON Schema (draft 2020-12) for the cassette
// format from the cassette structs themselves, so the committed
// cassette-format.v1.schema.json (and its SDK mirror) cannot drift from the
// loader. A matches-golden test is the drift gate; a cross-link test proves the
// schema's declared properties equal the cassette structs' JSON keys.
//
// ValidateCassetteBytes checks a cassette document's envelope shape entirely
// offline: structural validation through the canonical loader plus
// additionalProperties:false enforcement, which rejects misspelled field names
// that JSON decoding silently drops. It runs with no Docker and no graph so a
// contributor can catch a malformed cassette in milliseconds instead of eight
// minutes into a CI gate.
package schema
