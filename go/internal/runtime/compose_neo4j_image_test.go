// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "testing"

// neo4jCompatibilityImage is the Neo4j artifact the #6782 B-7 Neo4j cell and
// its live differential tests were proven on. A tag alone can be retargeted
// upstream, which would flip the Neo4j cell with no Eshu change.
const neo4jCompatibilityImage = "neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f"

// TestNeo4jComposeDefaultPinsProvenImageByDigest keeps the Neo4j compatibility
// overlay on the digest the evidence ran, with an operator override matching
// the NORNICDB_IMAGE pattern.
func TestNeo4jComposeDefaultPinsProvenImageByDigest(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.neo4j.yml")
	service := requireComposeService(t, doc, "neo4j")
	if want := "${NEO4J_IMAGE:-" + neo4jCompatibilityImage + "}"; service.Image != want {
		t.Fatalf("neo4j image = %q, want digest-pinned default %q", service.Image, want)
	}
	if !digestedImageRef.MatchString(neo4jCompatibilityImage) {
		t.Fatalf("neo4j default %q is not pinned by a full sha256 digest", neo4jCompatibilityImage)
	}
}
