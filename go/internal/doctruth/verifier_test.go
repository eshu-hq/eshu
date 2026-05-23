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

func TestVerifierAcceptsDocumentedArgumentsAndEndpointTemplates(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands: []doctruth.CommandTruth{
			{Path: []string{"docs", "verify"}, AllowsArgs: true},
			{Path: []string{"index"}, AllowsArgs: true},
			{Path: []string{"find", "name"}, AllowsArgs: true},
			{Path: []string{"analyze", "calls"}, AllowsArgs: true},
			{Path: []string{"mcp"}},
		},
		HTTPEndpoints: []doctruth.HTTPEndpointTruth{
			{Method: "GET", Path: "/api/v0/entities/{entity_id}/context"},
			{Method: "GET", Path: "/api/v0/repositories/{repo_id}/story"},
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "docs/reference/http-api.md",
		RevisionID: "rev-template",
		Content: "" +
			"`eshu docs verify [path]` verifies documentation.\n" +
			"`eshu index <path>` indexes a local checkout.\n" +
			"`eshu find name handleRelationships` finds one symbol by name.\n" +
			"`eshu analyze calls handleRelationships --repo eshu` shows direct callees.\n" +
			"`eshu mcp stdio` is not a shipped command.\n" +
			"`GET /api/v0/entities/{id}/context` documents the route family.\n" +
			"`GET /api/v0/entities/workload:payments-api/context` is a valid example.\n" +
			"`GET /api/v0/repositories/repository:r_ab12cd34/story` is also a valid example.\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	if got, want := result.Summary.Valid, 7; got != want {
		t.Fatalf("Summary.Valid = %d, want %d; findings=%#v", got, want, result.Findings)
	}
	if got, want := result.Summary.Contradicted, 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d; findings=%#v", got, want, result.Findings)
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

func TestVerifierIgnoresEnvironmentVariablePrefixExamples(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		EnvironmentVariables: []string{"ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE"},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{{
		Path:       "go/internal/coordinator/README.md",
		RevisionID: "rev-prefix",
		Content: "" +
			"`ESHU_WORKFLOW_COORDINATOR_*` names a variable family, not one variable.\n" +
			"`ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE` is one concrete variable.\n",
	}})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	if got, want := result.Summary.ClaimsChecked, 1; got != want {
		t.Fatalf("Summary.ClaimsChecked = %d, want %d; findings=%#v", got, want, result.Findings)
	}
	assertFindingStatus(t, result.Findings, "environment_variable", "valid")
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

func TestVerifierDefaultGenerationAndVersionsAreRunUnique(t *testing.T) {
	t.Parallel()

	fixedNow := func() time.Time {
		return time.Date(2026, time.May, 20, 15, 0, 0, 0, time.UTC)
	}
	first := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands: []doctruth.CommandTruth{{Path: []string{"scan"}}},
		Now:      fixedNow,
	})
	second := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands: []doctruth.CommandTruth{{Path: []string{"scan"}}},
		Now:      fixedNow,
	})
	doc := doctruth.DocumentInput{Path: "README.md", RevisionID: "rev", Content: "`eshu scan .`\n"}

	firstResult, err := first.Verify(context.Background(), []doctruth.DocumentInput{doc})
	if err != nil {
		t.Fatalf("first Verify() error = %v, want nil", err)
	}
	secondResult, err := second.Verify(context.Background(), []doctruth.DocumentInput{doc})
	if err != nil {
		t.Fatalf("second Verify() error = %v, want nil", err)
	}

	if got, wantNot := firstResult.Envelopes[0].GenerationID, secondResult.Envelopes[0].GenerationID; got == wantNot {
		t.Fatalf("GenerationID = %q for both runs, want run-unique defaults", got)
	}
	if got, wantNot := firstResult.Findings[0].FindingVersion, secondResult.Findings[0].FindingVersion; got == wantNot {
		t.Fatalf("FindingVersion = %q for both runs, want run-unique versions", got)
	}
	if got, wantNot := firstResult.EvidencePackets[0].PacketVersion, secondResult.EvidencePackets[0].PacketVersion; got == wantNot {
		t.Fatalf("PacketVersion = %q for both runs, want run-unique versions", got)
	}
}

func TestVerifierDocumentIDsIncludeSourceURI(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands: []doctruth.CommandTruth{{Path: []string{"scan"}}},
		Now: func() time.Time {
			return time.Date(2026, time.May, 20, 15, 0, 0, 0, time.UTC)
		},
	})

	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{
		{Path: "README.md", SourceURI: "file:///repo-a/README.md", RevisionID: "a", Content: "`eshu scan .`\n"},
		{Path: "README.md", SourceURI: "file:///repo-b/README.md", RevisionID: "b", Content: "`eshu scan .`\n"},
	})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	if got, wantNot := result.Findings[0].DocumentID, result.Findings[1].DocumentID; got == wantNot {
		t.Fatalf("DocumentID = %q for both sources, want source-aware document identity", got)
	}
	if got, wantNot := result.Findings[0].FindingID, result.Findings[1].FindingID; got == wantNot {
		t.Fatalf("FindingID = %q for both sources, want source-aware finding identity", got)
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
