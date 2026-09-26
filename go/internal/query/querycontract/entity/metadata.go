// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import "github.com/eshu-hq/eshu/go/internal/parser/fingerprint"

// FingerprintMetadataKeys returns the entity-metadata keys the parser
// fingerprint attach step writes: the exact and renamed hashes, the MinHash
// sketch, the shingle identities, and the leaf token count. They are
// store-internal columns consumed by the content writer's side-table fan-out
// and the code-divergence reducer, never by an API or MCP caller, so the query
// layer strips exactly this list from response metadata. The names are the
// exported constants in parser/fingerprint, so a rename there cannot drift from
// this list. fingerprint.StatsKey is a parser payload key, not an entity key,
// and is not included. Each call returns a fresh slice.
func FingerprintMetadataKeys() []string {
	return []string{
		fingerprint.KeyExact,
		fingerprint.KeyRenamed,
		fingerprint.KeySketch,
		fingerprint.KeyTokenCount,
		fingerprint.KeyShingles,
	}
}

// StripFingerprintMetadata deletes every FingerprintMetadataKeys entry from an
// entity-metadata map in place. The query layer calls it at the single
// metadata decode seam so API and MCP responses never carry the store-internal
// fingerprint columns, which were 23-59% of the row bytes on tools over the
// MCP response budget (#7167). Absent keys are a no-op, and a nil map is safe.
func StripFingerprintMetadata(metadata map[string]any) {
	for _, key := range FingerprintMetadataKeys() {
		delete(metadata, key)
	}
}
