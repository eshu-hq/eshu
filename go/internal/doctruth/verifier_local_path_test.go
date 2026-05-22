package doctruth_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

func TestVerifierComparesLocalPathClaims(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		LocalPathResolver: func(_ doctruth.DocumentInput, normalizedPath string) doctruth.LocalPathResolution {
			switch normalizedPath {
			case "deploy/kubernetes/checkout.yaml", "terraform/prod/main.tf":
				return doctruth.LocalPathResolution{Supported: true, Exists: true}
			case "deploy/kubernetes/missing.yaml":
				return doctruth.LocalPathResolution{Supported: true, Exists: false}
			default:
				return doctruth.LocalPathResolution{}
			}
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "docs/runbooks/checkout.md",
		RevisionID: "rev-local-paths",
		Content: "" +
			"Deploy with `deploy/kubernetes/checkout.yaml`.\n" +
			"Terraform lives at [main](terraform/prod/main.tf).\n" +
			"Old docs still mention `deploy/kubernetes/missing.yaml`.\n" +
			"External links like [docs](https://example.com/path.yaml) are not local truth claims.\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	assertFindingStatusForClaim(t, result.Findings, "local_path", "deploy/kubernetes/checkout.yaml", "valid")
	assertFindingStatusForClaim(t, result.Findings, "local_path", "terraform/prod/main.tf", "valid")
	assertFindingStatusForClaim(t, result.Findings, "local_path", "deploy/kubernetes/missing.yaml", "contradicted")
	if got, want := result.Summary.Valid, 2; got != want {
		t.Fatalf("Summary.Valid = %d, want %d", got, want)
	}
	if got, want := result.Summary.Contradicted, 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	if got, want := result.Summary.ClaimsChecked, 3; got != want {
		t.Fatalf("Summary.ClaimsChecked = %d, want %d", got, want)
	}
}

func assertFindingStatusForClaim(
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
	t.Fatalf("missing %s finding for %q in %#v", claimType, normalizedClaim, findings)
}
