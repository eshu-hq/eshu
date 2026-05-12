package query

import (
	"context"
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
			if !strings.Contains(cypher, "ContainerImage") || !strings.Contains(cypher, "image.digest IN $digests") {
				return nil, nil
			}
			if gotDigests, want := params["digests"], []string{testOCIDigest}; !reflect.DeepEqual(gotDigests, want) {
				t.Fatalf("digests param = %#v, want %#v", gotDigests, want)
			}
			return []map[string]any{
				{
					"image_id":      "oci-descriptor://ghcr.io/acme/payments-api@" + testOCIDigest,
					"digest":        testOCIDigest,
					"registry":      "ghcr.io",
					"repository":    "acme/payments-api",
					"repository_id": "oci-registry://ghcr.io/acme/payments-api",
					"media_type":    "application/vnd.oci.image.manifest.v1+json",
					"provider":      "ghcr",
				},
			}, nil
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
	_, err := fetchOCIImageRegistryTruth(t.Context(), fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			assertContainsAll(t, cypher,
				"MATCH (image:ContainerImage)",
				"MATCH (image:ContainerImageIndex)",
				"MATCH (image:ContainerImageDescriptor)",
				"image.digest IN $digests",
				"MATCH (image)<-[:PUBLISHES_MANIFEST|PUBLISHES_INDEX|PUBLISHES_DESCRIPTOR]-(repo:OciRegistryRepository)",
			)
			assertNotContains(t, cypher,
				"MATCH (repo:OciRegistryRepository)-[:PUBLISHES_MANIFEST|PUBLISHES_INDEX|PUBLISHES_DESCRIPTOR]->(image)",
				"OR image:",
			)
			return nil, nil
		},
	}, []string{imageRef})
	if err != nil {
		t.Fatalf("fetchOCIImageRegistryTruth() error = %v, want nil", err)
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
			if !strings.Contains(cypher, "ContainerImageTagObservation") {
				return nil, nil
			}
			if gotRefs, want := params["image_refs"], []string{tagRef}; !reflect.DeepEqual(gotRefs, want) {
				t.Fatalf("image_refs param = %#v, want %#v", gotRefs, want)
			}
			return []map[string]any{
				{
					"image_ref":     tagRef,
					"digest":        testOCIDigest,
					"tag":           "latest",
					"repository_id": "oci-registry://ghcr.io/acme/payments-api",
				},
				{
					"image_ref":     tagRef,
					"digest":        otherDigest,
					"tag":           "latest",
					"repository_id": "oci-registry://ghcr.io/acme/payments-api",
				},
			}, nil
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
	_, err := fetchOCIImageRegistryTruth(t.Context(), fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "ContainerImageTagObservation") {
				return nil, nil
			}
			assertContainsAll(t, cypher,
				"MATCH (tag:ContainerImageTagObservation)",
				"tag.image_ref IN $image_refs",
				"MATCH (repo:OciRegistryRepository)-[:OBSERVED_TAG]->(tag)",
				"MATCH (image:ContainerImage)",
				"MATCH (image:ContainerImageIndex)",
				"MATCH (image:ContainerImageDescriptor)",
				"image.digest = tag.resolved_digest",
			)
			assertNotContains(t, cypher,
				"tag.reference IN $image_refs",
				"OR tag.",
				"OR image:",
				"MATCH (image)",
			)
			return nil, nil
		},
	}, []string{tagRef})
	if err != nil {
		t.Fatalf("fetchOCIImageRegistryTruth() error = %v, want nil", err)
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
