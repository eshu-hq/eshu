package doctruth_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestVerifierComparesCLIEndpointAndEnvClaims(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands: []doctruth.CommandTruth{
			{Path: []string{"scan"}},
			{Path: []string{"docs", "verify"}},
		},
		HTTPEndpoints: []doctruth.HTTPEndpointTruth{
			{Method: "GET", Path: "/api/v0/documentation/findings"},
		},
		EnvironmentVariables: []string{"ESHU_SERVICE_URL"},
		Now: func() time.Time {
			return time.Date(2026, time.May, 20, 15, 0, 0, 0, time.UTC)
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "docs/runbook.md",
		SourceURI:  "file://docs/runbook.md",
		RevisionID: "rev-1",
		Content: "" +
			"Run `eshu scan .` before release.\n" +
			"Then call `GET /api/v0/documentation/findings`.\n" +
			"Set `ESHU_SERVICE_URL` for remote API access.\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	assertFindingStatus(t, result.Findings, "cli_command", "valid")
	assertFindingStatus(t, result.Findings, "http_endpoint", "valid")
	assertFindingStatus(t, result.Findings, "environment_variable", "valid")
	if got, want := result.Summary.Valid, 3; got != want {
		t.Fatalf("Summary.Valid = %d, want %d", got, want)
	}
	if got, want := len(result.EvidencePackets), 3; got != want {
		t.Fatalf("len(EvidencePackets) = %d, want %d", got, want)
	}
	if got, want := countEnvelopes(result.Envelopes, facts.DocumentationFindingFactKind), 3; got != want {
		t.Fatalf("documentation finding envelopes = %d, want %d", got, want)
	}
	if got, want := countEnvelopes(result.Envelopes, facts.DocumentationEvidencePacketFactKind), 3; got != want {
		t.Fatalf("documentation evidence packet envelopes = %d, want %d", got, want)
	}
}

func TestVerifierKeepsContradictedUnsupportedAndMissingEvidenceSeparate(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands: []doctruth.CommandTruth{
			{Path: []string{"scan"}},
		},
		HTTPEndpoints: []doctruth.HTTPEndpointTruth{
			{Method: "GET", Path: "/api/v0/status/index"},
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "README.md",
		RevisionID: "rev-2",
		Content: "" +
			"`eshu vaporize all` is not real.\n" +
			"`POST /api/v0/nope` is also not real.\n" +
			"`ESHU_NOT_DECLARED` lacks a local truth source.\n" +
			"`terraform apply` is outside this verifier slice.\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	assertFindingStatus(t, result.Findings, "cli_command", "contradicted")
	assertFindingStatus(t, result.Findings, "http_endpoint", "contradicted")
	assertFindingStatus(t, result.Findings, "environment_variable", "missing_evidence")
	assertFindingStatus(t, result.Findings, "shell_command", "unsupported_claim_type")
	if got, want := result.Summary.Contradicted, 2; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	if got, want := result.Summary.MissingEvidence, 1; got != want {
		t.Fatalf("Summary.MissingEvidence = %d, want %d", got, want)
	}
	if got, want := result.Summary.UnsupportedClaimType, 1; got != want {
		t.Fatalf("Summary.UnsupportedClaimType = %d, want %d", got, want)
	}
}

func TestVerifierBoundsDocumentsAndContentBytes(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands:         []doctruth.CommandTruth{{Path: []string{"scan"}}},
		MaxDocuments:     1,
		MaxDocumentBytes: 24,
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{
		{Path: "one.md", RevisionID: "1", Content: "`eshu scan .`\n`eshu vaporize all`\n"},
		{Path: "two.md", RevisionID: "1", Content: "`eshu scan .`\n"},
	})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	if !result.Truncated {
		t.Fatal("result.Truncated = false, want true")
	}
	if got, want := result.Summary.DocumentsScanned, 1; got != want {
		t.Fatalf("DocumentsScanned = %d, want %d", got, want)
	}
	if got, want := result.Summary.BytesScanned, 24; got != want {
		t.Fatalf("BytesScanned = %d, want %d", got, want)
	}
}

func assertFindingStatus(t *testing.T, findings []doctruth.VerificationFinding, claimType string, status string) {
	t.Helper()

	for _, finding := range findings {
		if finding.ClaimType == claimType {
			if finding.Status != status {
				t.Fatalf("%s status = %q, want %q", claimType, finding.Status, status)
			}
			if finding.EvidencePacketID == "" {
				t.Fatalf("%s EvidencePacketID = empty, want durable packet id", claimType)
			}
			return
		}
	}
	t.Fatalf("missing finding with claim type %q in %#v", claimType, findings)
}

func countEnvelopes(envelopes []facts.Envelope, factKind string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			count++
		}
	}
	return count
}
