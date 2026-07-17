// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveOCITraceDeploymentRegistryTruth is the backend-required proof for the
// #5287 OCI trace-deployment fix. It seeds a representative OCI registry graph
// on a live NornicDB, captures the OLD multi-clause shapes (which corrupt on the
// pinned build) for evidence, and asserts that the shipped single-clause
// fetchOCIImageRegistryTruth returns the correct canonical-digest and
// tag-resolved registry truth.
//
//	Run: ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 \
//		go test ./internal/query -run TestLiveOCITraceDeploymentRegistryTruth -count=1 -v
func TestLiveOCITraceDeploymentRegistryTruth(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1 to run the live OCI registry-truth proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required (e.g. bolt://localhost:17687)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	write := func(cypher string, params map[string]any) {
		s := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
		defer func() { _ = s.Close(ctx) }()
		if _, err := s.Run(ctx, cypher, params); err != nil {
			t.Fatalf("seed write failed: %v\ncypher=%s", err, cypher)
		}
	}
	reader := NewNeo4jReader(driver, "nornic")

	const (
		digest    = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		repoUID   = "oci-registry://ghcr.io/acme/payments-api"
		imageID   = "oci-img://payments@d1"
		mediaType = "application/vnd.oci.image.manifest.v1+json"
		digestRef = "ghcr.io/acme/payments-api@sha256:1111111111111111111111111111111111111111111111111111111111111111"
		tagRef    = "ghcr.io/acme/payments-api:v1"
	)

	// Clean + seed a representative OCI registry graph. Delete ONLY the exact
	// synthetic nodes by id/digest/ref (never a label-wide DETACH DELETE) so
	// running this against a retained evidence graph cannot wipe production-shaped
	// OCI rows. The same targeted cleanup runs on exit.
	cleanup := func() {
		write(`MATCH (n:OciRegistryRepository {uid:$uid}) DETACH DELETE n`, map[string]any{"uid": repoUID})
		write(`MATCH (n:ContainerImage {id:$id}) DETACH DELETE n`, map[string]any{"id": imageID})
		write(`MATCH (n:ContainerImageTagObservation {image_ref:$ref}) DETACH DELETE n`, map[string]any{"ref": tagRef})
	}
	cleanup()
	defer cleanup()
	write(`CREATE (:OciRegistryRepository {uid:$uid, registry:$reg, repository:$repo, provider:$prov})`,
		map[string]any{"uid": repoUID, "reg": "ghcr.io", "repo": "acme/payments-api", "prov": "ghcr"})
	write(`CREATE (:ContainerImage {id:$id, digest:$d, repository_id:$rid, media_type:$mt})`,
		map[string]any{"id": imageID, "d": digest, "rid": repoUID, "mt": mediaType})
	write(`CREATE (:ContainerImageTagObservation {image_ref:$ref, tag:$tag, resolved_digest:$rd, repository_id:$rid})`,
		map[string]any{"ref": tagRef, "tag": "v1", "rd": digest, "rid": repoUID})

	// Capture the OLD multi-clause shapes for the evidence doc.
	oldDigest, _ := reader.Run(ctx, `
MATCH (image:ContainerImage) WHERE image.digest IN $digests
MATCH (repo:OciRegistryRepository) WHERE repo.uid = image.repository_id
RETURN DISTINCT coalesce(image.id, image.descriptor_id) AS image_id, image.digest AS digest,
       repo.registry AS registry, repo.uid AS repository_id, image.media_type AS media_type
ORDER BY repository_id, digest`, map[string]any{"digests": []string{digest}})
	oldTag, _ := reader.Run(ctx, `
MATCH (tag:ContainerImageTagObservation) WHERE tag.image_ref IN $image_refs
MATCH (repo:OciRegistryRepository) WHERE repo.uid = tag.repository_id
MATCH (image:ContainerImage) WHERE image.digest = tag.resolved_digest
RETURN DISTINCT tag.image_ref AS image_ref, tag.resolved_digest AS digest,
       coalesce(image.id, image.descriptor_id) AS image_id
ORDER BY image_ref, digest`, map[string]any{"image_refs": []string{tagRef}})
	logJSON(t, "OLD digest shape (2x MATCH + DISTINCT)", oldDigest)
	logJSON(t, "OLD tag shape (3x MATCH + DISTINCT)", oldTag)
	if len(oldDigest) == 1 && StringVal(oldDigest[0], "image_id") != "" {
		t.Logf("NOTE: OLD digest image_id was non-empty this run; corruption is coalesce->null under multi-clause")
	}

	// Assert the shipped single-clause path returns correct truth.
	truth, err := fetchOCIImageRegistryTruth(ctx, reader, []string{digestRef, tagRef})
	if err != nil {
		t.Fatalf("fetchOCIImageRegistryTruth() error = %v", err)
	}
	logJSON(t, "NEW fetchOCIImageRegistryTruth (single-clause + Go join)", truth)

	var digestRow, tagRow map[string]any
	for _, row := range truth {
		switch StringVal(row, "image_ref") {
		case digestRef:
			digestRow = row
		case tagRef:
			tagRow = row
		}
	}
	if digestRow == nil {
		t.Fatalf("no canonical-digest truth row for %s in %#v", digestRef, truth)
	}
	if got := StringVal(digestRow, "image_id"); got != imageID {
		t.Errorf("digest truth image_id = %q, want %q (OLD shape corrupted this to null)", got, imageID)
	}
	for key, want := range map[string]string{
		"digest": digest, "registry": "ghcr.io", "repository": "acme/payments-api",
		"repository_id": repoUID, "provider": "ghcr", "match_strength": "canonical_digest",
	} {
		if got := StringVal(digestRow, key); got != want {
			t.Errorf("digest truth %s = %q, want %q", key, got, want)
		}
	}
	if tagRow == nil {
		t.Fatalf("no tag-resolved truth row for %s (OLD 3x-MATCH shape dropped every row) in %#v", tagRef, truth)
	}
	for key, want := range map[string]string{
		"tag": "v1", "digest": digest, "image_id": imageID, "registry": "ghcr.io",
		"repository": "acme/payments-api", "repository_id": repoUID, "match_strength": "tag_resolved_to_digest",
	} {
		if got := StringVal(tagRow, key); got != want {
			t.Errorf("tag truth %s = %q, want %q", key, got, want)
		}
	}
}

func logJSON(t *testing.T, label string, rows []map[string]any) {
	t.Helper()
	b, _ := json.MarshalIndent(rows, "", "  ")
	t.Logf("\n=== %s (%d rows) ===\n%s", label, len(rows), b)
}
