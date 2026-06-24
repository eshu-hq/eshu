package doctruth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

func TestVerifierComparesContainerImageClaims(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		ContainerImageResolver: func(_ doctruth.DocumentInput, imageRef string) doctruth.ContainerImageResolution {
			return doctruth.ContainerImageResolution{
				Supported: true,
				Exists:    imageRef == "ghcr.io/acme/api:1.2.3",
			}
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "docs/deploy.md",
		RevisionID: "rev-image",
		Content: "" +
			"Deploy `ghcr.io/acme/api:1.2.3` from the release manifest.\n" +
			"```yaml\n" +
			"image: ghcr.io/acme/worker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
			"image: \"12345.jpg\"\n" +
			"```\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	assertFindingWithClaim(t, result.Findings, "container_image_ref", "ghcr.io/acme/api:1.2.3", "valid")
	assertFindingWithClaim(
		t,
		result.Findings,
		"container_image_ref",
		"ghcr.io/acme/worker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"contradicted",
	)
	if got, want := result.Summary.Valid, 1; got != want {
		t.Fatalf("Summary.Valid = %d, want %d", got, want)
	}
	if got, want := result.Summary.Contradicted, 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	if got, want := result.Summary.ClaimsChecked, 2; got != want {
		t.Fatalf("Summary.ClaimsChecked = %d, want %d", got, want)
	}
}

func TestVerifierMarksContainerImageUnsupportedWithoutResolver(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "docs/deploy.md",
		RevisionID: "rev-image",
		Content:    "Deploy `ghcr.io/acme/api:1.2.3`.\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	assertFindingWithClaim(t, result.Findings, "container_image_ref", "ghcr.io/acme/api:1.2.3", "unsupported_claim_type")
}

func TestContainerImageRefsFromTextIsConservative(t *testing.T) {
	t.Parallel()

	refs := doctruth.ContainerImageRefsFromText(
		"" +
			"image: ghcr.io/acme/api:1.2.3\n" +
			"image: registry.example.test:5000/team/api:2.0.0\n" +
			"image: postgres:16\n" +
			"image: \"12345.jpg\"\n" +
			"image: ${IMAGE:-ghcr.io/acme/default:2.0.0}\n" +
			"FROM ghcr.io/acme/base:3.0.0\n" +
			"http://example.com/not-an-image:8080\n",
	)
	want := map[string]struct{}{
		"ghcr.io/acme/api:1.2.3":                    {},
		"registry.example.test:5000/team/api:2.0.0": {},
		"postgres:16":                               {},
		"ghcr.io/acme/default:2.0.0":                {},
		"ghcr.io/acme/base:3.0.0":                   {},
	}
	if len(refs) != len(want) {
		t.Fatalf("ContainerImageRefsFromText() = %#v, want %d refs", refs, len(want))
	}
	for _, ref := range refs {
		if _, ok := want[ref]; !ok {
			t.Fatalf("unexpected image ref %q in %#v", ref, refs)
		}
	}
}

func TestContainerImageLineClaimsDoNotExtractBareHostPort(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		ContainerImageResolver: func(_ doctruth.DocumentInput, imageRef string) doctruth.ContainerImageResolution {
			t.Fatalf("unexpected image resolver call for %q", imageRef)
			return doctruth.ContainerImageResolution{}
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "docs/service.md",
		RevisionID: "rev-host-port",
		Content: "" +
			"Local API listens on localhost:8080.\n" +
			"External API listens on example.com:443.\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
	if got := len(result.Findings); got != 0 {
		t.Fatalf("len(Findings) = %d, want 0; findings=%#v", got, result.Findings)
	}
}

func TestVerifierDoesNotTreatBacktickedColonIdentifiersAsImages(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		ContainerImageResolver: func(_ doctruth.DocumentInput, imageRef string) doctruth.ContainerImageResolution {
			t.Fatalf("unexpected image resolver call for %q", imageRef)
			return doctruth.ContainerImageResolution{}
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "docs/reference.md",
		RevisionID: "rev-colon-identifiers",
		Content: strings.Join([]string{
			"Graph labels include `node:Function` and `relationship:CALLS`.",
			"Use `go/internal/storage/postgres/rows.go:22` for the code pointer.",
			"Runtime evidence may include `2026-05-21T23:52:03Z`.",
			"AWS actions such as `dynamodb:GetItem` are not images.",
			"Scoped ids such as `repository:r_<8-hex>` are not images.",
		}, "\n"),
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
	if got := len(result.Findings); got != 0 {
		t.Fatalf("len(Findings) = %d, want 0; findings=%#v", got, result.Findings)
	}
}

func TestNormalizeContainerImageRefRejectsHostPortsWithoutPath(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"localhost:8080", "example.com:443"} {
		if got := doctruth.NormalizeContainerImageRefClaim(raw); got != "" {
			t.Fatalf("NormalizeContainerImageRefClaim(%q) = %q, want empty", raw, got)
		}
	}
	if got, want := doctruth.NormalizeContainerImageRefClaim("registry.example.test:5000/team/api:1.2.3"), "registry.example.test:5000/team/api:1.2.3"; got != want {
		t.Fatalf("NormalizeContainerImageRefClaim(registry with port) = %q, want %q", got, want)
	}
}

func TestVerifierNormalizesBacktickedContainerImageClaims(t *testing.T) {
	t.Parallel()

	digest := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	normalized := "ghcr.io/acme/api@sha256:" + strings.ToLower(digest)
	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		ContainerImageResolver: func(_ doctruth.DocumentInput, imageRef string) doctruth.ContainerImageResolution {
			return doctruth.ContainerImageResolution{Supported: true, Exists: imageRef == normalized}
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "docs/deploy.md",
		RevisionID: "rev-digest",
		Content:    "Deploy `ghcr.io/acme/api@sha256:" + digest + "`.\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
	assertFindingWithClaim(t, result.Findings, "container_image_ref", normalized, "valid")
}

func assertFindingWithClaim(
	t *testing.T,
	findings []doctruth.VerificationFinding,
	claimType string,
	normalizedClaim string,
	status string,
) {
	t.Helper()

	for _, finding := range findings {
		if finding.ClaimType == claimType && finding.NormalizedClaim == normalizedClaim {
			if finding.Status != status {
				t.Fatalf("%s %s status = %q, want %q", claimType, normalizedClaim, finding.Status, status)
			}
			return
		}
	}
	t.Fatalf("missing finding %s %s in %#v", claimType, normalizedClaim, findings)
}
