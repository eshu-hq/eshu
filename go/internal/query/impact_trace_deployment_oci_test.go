// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const testOCIDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func TestFetchOCIImageRegistryTruthUsesDigestMatchesAsCanonical(t *testing.T) {
	t.Parallel()

	imageRef := "ghcr.io/acme/payments-api@" + testOCIDigest
	got, err := fetchOCIImageRegistryTruth(t.Context(), fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (image:ContainerImage)") &&
				strings.Contains(cypher, "image.digest IN $digests"):
				if gotDigests, want := params["digests"], []string{testOCIDigest}; !reflect.DeepEqual(gotDigests, want) {
					t.Fatalf("digests param = %#v, want %#v", gotDigests, want)
				}
				return []map[string]any{
					{
						"image_id":      "oci-descriptor://ghcr.io/acme/payments-api@" + testOCIDigest,
						"digest":        testOCIDigest,
						"repository_id": "oci-registry://ghcr.io/acme/payments-api",
						"media_type":    "application/vnd.oci.image.manifest.v1+json",
					},
				}, nil
			case strings.Contains(cypher, "MATCH (repo:OciRegistryRepository)") &&
				strings.Contains(cypher, "repo.uid IN $repository_ids"):
				if gotIDs, want := params["repository_ids"], []string{"oci-registry://ghcr.io/acme/payments-api"}; !reflect.DeepEqual(gotIDs, want) {
					t.Fatalf("repository_ids param = %#v, want %#v", gotIDs, want)
				}
				return []map[string]any{
					{
						"repository_id": "oci-registry://ghcr.io/acme/payments-api",
						"registry":      "ghcr.io",
						"repository":    "acme/payments-api",
						"provider":      "ghcr",
					},
				}, nil
			default:
				return nil, nil
			}
		},
	}, []string{imageRef})
	if err != nil {
		t.Fatalf("fetchOCIImageRegistryTruth() error = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(fetchOCIImageRegistryTruth()) = %d, want 1: %#v", len(got), got)
	}
	if got[0]["image_ref"] != imageRef {
		t.Fatalf("image_ref = %#v, want %#v", got[0]["image_ref"], imageRef)
	}
	if got[0]["match_strength"] != "canonical_digest" {
		t.Fatalf("match_strength = %#v, want canonical_digest", got[0]["match_strength"])
	}
	if got[0]["truth_basis"] != "digest" {
		t.Fatalf("truth_basis = %#v, want digest", got[0]["truth_basis"])
	}
}

func TestFetchOCIImageRegistryTruthUsesSelectiveDigestAnchors(t *testing.T) {
	t.Parallel()

	imageRef := "ghcr.io/acme/payments-api@" + testOCIDigest
	seenLabels := make(map[string]bool)
	_, err := fetchOCIImageRegistryTruth(t.Context(), fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			// Every image lookup is single-clause: the registry repository is
			// resolved by a separate query and joined in Go, so the image
			// statement must not carry a second MATCH or a cross-clause join.
			if strings.Contains(cypher, "MATCH (image:") {
				assertSingleAnchorClause(t, cypher)
				assertNotContains(
					t, cypher,
					"MATCH (repo:OciRegistryRepository)",
					"WHERE repo.uid = image.repository_id",
					"CALL {",
					"OR image:",
				)
				switch {
				case strings.Contains(cypher, "MATCH (image:ContainerImage)"):
					seenLabels["ContainerImage"] = true
				case strings.Contains(cypher, "MATCH (image:ContainerImageIndex)"):
					seenLabels["ContainerImageIndex"] = true
				case strings.Contains(cypher, "MATCH (image:ContainerImageDescriptor)"):
					seenLabels["ContainerImageDescriptor"] = true
				default:
					t.Fatalf("Cypher = %q, want one OCI image label anchor", cypher)
				}
				return []map[string]any{
					{
						"image_id":      "oci-descriptor://ghcr.io/acme/payments-api@" + testOCIDigest,
						"digest":        testOCIDigest,
						"repository_id": "oci-registry://ghcr.io/acme/payments-api",
						"media_type":    "application/vnd.oci.image.manifest.v1+json",
					},
				}, nil
			}
			// The registry-repository resolver is its own single-clause query.
			if strings.Contains(cypher, "MATCH (repo:OciRegistryRepository)") {
				assertSingleAnchorClause(t, cypher)
				assertContainsAll(t, cypher, "repo.uid IN $repository_ids")
			}
			return nil, nil
		},
	}, []string{imageRef})
	if err != nil {
		t.Fatalf("fetchOCIImageRegistryTruth() error = %v, want nil", err)
	}
	for _, label := range ociImageLookupLabels {
		if !seenLabels[label] {
			t.Fatalf("fetchOCIImageRegistryTruth() did not query label %s", label)
		}
	}
}

func TestFetchOCIImageRegistryTruthDoesNotPromoteUnresolvedTag(t *testing.T) {
	t.Parallel()

	got, err := fetchOCIImageRegistryTruth(t.Context(), fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "ContainerImageTagObservation") {
				return nil, nil
			}
			return nil, nil
		},
	}, []string{"ghcr.io/acme/payments-api:latest"})
	if err != nil {
		t.Fatalf("fetchOCIImageRegistryTruth() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("fetchOCIImageRegistryTruth() = %#v, want no registry truth for unresolved mutable tag", got)
	}
}

func TestFetchOCIImageRegistryTruthMarksConflictingTagObservationsAmbiguous(t *testing.T) {
	t.Parallel()

	tagRef := "ghcr.io/acme/payments-api:latest"
	otherDigest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	got, err := fetchOCIImageRegistryTruth(t.Context(), fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (tag:ContainerImageTagObservation)"):
				if gotRefs, want := params["image_refs"], []string{tagRef}; !reflect.DeepEqual(gotRefs, want) {
					t.Fatalf("image_refs param = %#v, want %#v", gotRefs, want)
				}
				// Two tag observations for the same mutable tag resolving to
				// different digests -> ambiguous.
				return []map[string]any{
					{"image_ref": tagRef, "digest": testOCIDigest, "tag": "latest", "repository_id": "oci-registry://ghcr.io/acme/payments-api"},
					{"image_ref": tagRef, "digest": otherDigest, "tag": "latest", "repository_id": "oci-registry://ghcr.io/acme/payments-api"},
				}, nil
			case strings.Contains(cypher, "MATCH (repo:OciRegistryRepository)"):
				return []map[string]any{
					{"repository_id": "oci-registry://ghcr.io/acme/payments-api", "registry": "ghcr.io", "repository": "acme/payments-api", "provider": "ghcr"},
				}, nil
			case strings.Contains(cypher, "MATCH (image:ContainerImage)"):
				// Both resolved digests have a canonical image node.
				return []map[string]any{
					{"image_id": "oci-img://a", "digest": testOCIDigest, "repository_id": "oci-registry://ghcr.io/acme/payments-api", "media_type": "application/vnd.oci.image.manifest.v1+json"},
					{"image_id": "oci-img://b", "digest": otherDigest, "repository_id": "oci-registry://ghcr.io/acme/payments-api", "media_type": "application/vnd.oci.image.manifest.v1+json"},
				}, nil
			default:
				return nil, nil
			}
		},
	}, []string{tagRef})
	if err != nil {
		t.Fatalf("fetchOCIImageRegistryTruth() error = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(fetchOCIImageRegistryTruth()) = %d, want 1: %#v", len(got), got)
	}
	if got[0]["match_strength"] != "ambiguous_tag" {
		t.Fatalf("match_strength = %#v, want ambiguous_tag", got[0]["match_strength"])
	}
	if got[0]["ambiguous"] != true {
		t.Fatalf("ambiguous = %#v, want true", got[0]["ambiguous"])
	}
	if got[0]["truth_basis"] != "observed_tag" {
		t.Fatalf("truth_basis = %#v, want observed_tag", got[0]["truth_basis"])
	}
	if got[0]["digest_candidates"] == nil {
		t.Fatalf("digest_candidates missing from ambiguous tag row: %#v", got[0])
	}
}

func TestFetchOCIImageRegistryTruthUsesSelectiveTagAnchor(t *testing.T) {
	t.Parallel()

	tagRef := "ghcr.io/acme/payments-api:latest"
	seenLabels := make(map[string]bool)
	sawTagQuery := false
	_, err := fetchOCIImageRegistryTruth(t.Context(), fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (tag:ContainerImageTagObservation)"):
				// The tag observation lookup is single-clause: the registry
				// repository and resolved image are separate joins in Go, not a
				// three-MATCH statement.
				sawTagQuery = true
				assertSingleAnchorClause(t, cypher)
				assertContainsAll(t, cypher, "tag.image_ref IN $image_refs", "tag.resolved_digest AS digest")
				assertNotContains(
					t, cypher,
					"MATCH (repo:OciRegistryRepository)",
					"WHERE repo.uid = tag.repository_id",
					"image.digest = tag.resolved_digest",
					"CALL {",
					"MATCH (image:",
				)
				// One tag observation so the resolver issues the image/repo joins.
				return []map[string]any{
					{"image_ref": tagRef, "digest": testOCIDigest, "tag": "latest", "repository_id": "oci-registry://ghcr.io/acme/payments-api"},
				}, nil
			case strings.Contains(cypher, "MATCH (image:"):
				assertSingleAnchorClause(t, cypher)
				switch {
				case strings.Contains(cypher, "MATCH (image:ContainerImage)"):
					seenLabels["ContainerImage"] = true
				case strings.Contains(cypher, "MATCH (image:ContainerImageIndex)"):
					seenLabels["ContainerImageIndex"] = true
				case strings.Contains(cypher, "MATCH (image:ContainerImageDescriptor)"):
					seenLabels["ContainerImageDescriptor"] = true
				default:
					t.Fatalf("Cypher = %q, want one OCI image label anchor", cypher)
				}
				return nil, nil
			case strings.Contains(cypher, "MATCH (repo:OciRegistryRepository)"):
				assertSingleAnchorClause(t, cypher)
				assertContainsAll(t, cypher, "repo.uid IN $repository_ids")
				return nil, nil
			default:
				return nil, nil
			}
		},
	}, []string{tagRef})
	if err != nil {
		t.Fatalf("fetchOCIImageRegistryTruth() error = %v, want nil", err)
	}
	if !sawTagQuery {
		t.Fatal("fetchOCIImageRegistryTruth() did not issue the tag observation query")
	}
	for _, label := range ociImageLookupLabels {
		if !seenLabels[label] {
			t.Fatalf("fetchOCIImageRegistryTruth() did not query label %s for tag resolution", label)
		}
	}
}

func TestBuildDeploymentTraceResponseIncludesOCIRegistryTruthInOverview(t *testing.T) {
	t.Parallel()

	got := buildDeploymentTraceResponse("payments-api", map[string]any{
		"id":         "workload:payments-api",
		"name":       "payments-api",
		"image_refs": []string{"ghcr.io/acme/payments-api@" + testOCIDigest},
		"image_registry_truth": []map[string]any{
			{
				"image_ref":       "ghcr.io/acme/payments-api@" + testOCIDigest,
				"digest":          testOCIDigest,
				"match_strength":  "canonical_digest",
				"truth_basis":     "digest",
				"repository_id":   "oci-registry://ghcr.io/acme/payments-api",
				"registry":        "ghcr.io",
				"repository":      "acme/payments-api",
				"provider":        "ghcr",
				"identity_source": "oci_registry_projection",
			},
		},
	})

	truth, ok := got["image_registry_truth"].([]map[string]any)
	if !ok || len(truth) != 1 {
		t.Fatalf("image_registry_truth = %#v, want one registry truth row", got["image_registry_truth"])
	}
	overview := mapValue(got, "deployment_overview")
	if got, want := IntVal(overview, "image_registry_match_count"), 1; got != want {
		t.Fatalf("deployment_overview.image_registry_match_count = %d, want %d", got, want)
	}
	if got, want := IntVal(overview, "canonical_image_match_count"), 1; got != want {
		t.Fatalf("deployment_overview.canonical_image_match_count = %d, want %d", got, want)
	}
}

// TestOCIRegistryTruthQueriesAreNornicDBSafe guards the #5287 fix: every OCI
// registry-truth read must be a single-anchor-clause shape. The pinned NornicDB
// build mis-executes any read that places a second MATCH (or a cross-clause
// property join) between the anchor and the projection — the old two-MATCH
// digest query returned a null coalesce(image.id, image.descriptor_id) and the
// old three-MATCH tag query dropped every row.
func TestOCIRegistryTruthQueriesAreNornicDBSafe(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"images-by-digest[ContainerImage]":           fmt.Sprintf(ociImageByDigestCypher, "ContainerImage"),
		"images-by-digest[ContainerImageIndex]":      fmt.Sprintf(ociImageByDigestCypher, "ContainerImageIndex"),
		"images-by-digest[ContainerImageDescriptor]": fmt.Sprintf(ociImageByDigestCypher, "ContainerImageDescriptor"),
		"repositories-by-uid":                        ociRepositoryByUIDCypher,
		"tag-observations-by-ref":                    ociTagObservationByRefCypher,
	}
	for name, q := range queries {
		if got := strings.Count(q, "MATCH"); got != 1 {
			t.Errorf("%s must use exactly one MATCH (multi-clause corrupts on NornicDB), got %d: %s", name, got, q)
		}
		if strings.Contains(q, "RETURN DISTINCT") {
			t.Errorf("%s must not use RETURN DISTINCT (returns literal alias text on NornicDB): %s", name, q)
		}
		if strings.Contains(q, "OPTIONAL") {
			t.Errorf("%s must not use OPTIONAL MATCH (multi-clause row-drop on NornicDB): %s", name, q)
		}
		if strings.Contains(q, "WITH ") {
			t.Errorf("%s must not use an intermediate WITH clause (multi-clause corrupts on NornicDB): %s", name, q)
		}
	}
}

// assertSingleAnchorClause fails when a dynamically issued OCI read is not a
// single-anchor-clause shape.
func assertSingleAnchorClause(t *testing.T, cypher string) {
	t.Helper()
	if got := strings.Count(cypher, "MATCH"); got != 1 {
		t.Fatalf("Cypher must use exactly one MATCH, got %d: %s", got, cypher)
	}
	assertNotContains(t, cypher, "RETURN DISTINCT", "OPTIONAL", "WITH ")
}

func assertContainsAll(t *testing.T, value string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(value, want) {
			t.Fatalf("Cypher = %q, want substring %q", value, want)
		}
	}
}

func assertNotContains(t *testing.T, value string, forbidden ...string) {
	t.Helper()
	for _, want := range forbidden {
		if strings.Contains(value, want) {
			t.Fatalf("Cypher = %q, must not contain %q", value, want)
		}
	}
}
