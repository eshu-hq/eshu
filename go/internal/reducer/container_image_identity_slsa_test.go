// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// slsaConfigSourceProvenanceFact builds an attestation.slsa_provenance
// envelope whose config_source names the given git repository URL and
// commit, mirroring the wire shape the SBOM runtime collector emits
// (go/internal/collector/sbomruntime/attestation.go) for #5456.
func slsaConfigSourceProvenanceFact(factID, statementID, repoURL, commit string) facts.Envelope {
	return attestationSLSAProvenanceFactWithMaterials(
		factID, statementID, "https://slsa.dev/provenance/v1", "",
		nil,
		map[string]any{
			"uri":    "git+" + repoURL + "@refs/heads/main",
			"digest": map[string]string{"sha1": commit},
		},
	)
}

// slsaImageStatementFact mirrors attestationStatementFact but is scoped to
// the container-image-identity proof matrix's naming (the container image
// digest is the statement's single subject digest).
func slsaImageStatementFact(factID, statementID, imageDigest string) facts.Envelope {
	return attestationStatementFact(factID, statementID, imageDigest, "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "parsed", "verified")
}

const (
	slsaProofRepoURL = "https://github.com/acme/payments-api"
	slsaProofCommit  = "0123456789abcdef0123456789abcdef01234567"
)

// TestApplySLSADigestRevisionPositive is proof-matrix case 1 (#5456 spec
// section 5): an image with SLSA provenance naming a config source commit
// must resolve SourceRevisionProvenance to slsa_provenance_commit and
// SourceRevision to the SLSA-attested commit.
func TestApplySLSADigestRevisionPositive(t *testing.T) {
	t.Parallel()

	imageRef := "registry.example.com/team/api@" + testContainerDigest
	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		gitImageRefFact("content-declares", imageRef),
		ociManifestFact("oci-manifest", testContainerDigest),
		slsaImageStatementFact("statement-slsa", "stmt-slsa", testContainerDigest),
		slsaConfigSourceProvenanceFact("provenance-slsa", "stmt-slsa", slsaProofRepoURL, slsaProofCommit),
	})

	got := decisionsByRef(decisions)
	decision, ok := got[imageRef]
	if !ok {
		t.Fatalf("no decision for %q: %#v", imageRef, got)
	}
	if decision.SourceRevisionProvenance != containerImageSourceRevisionSLSAProvenanceCommit {
		t.Fatalf("SourceRevisionProvenance = %q, want %q", decision.SourceRevisionProvenance, containerImageSourceRevisionSLSAProvenanceCommit)
	}
	if decision.SourceRevision != slsaProofCommit {
		t.Fatalf("SourceRevision = %q, want %q", decision.SourceRevision, slsaProofCommit)
	}
}

// TestApplySLSADigestRevisionOutranksOCIConfigSourceLabel is proof-matrix
// case 2: an image with BOTH a SLSA-attested commit and an OCI config source
// revision label must resolve to the SLSA tier, not oci_config_source_label.
func TestApplySLSADigestRevisionOutranksOCIConfigSourceLabel(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact("repo://acme/payments-api", slsaProofRepoURL+".git"),
		ociManifestWithConfigLabels("oci-manifest", testContainerDigest, map[string]string{
			"org.opencontainers.image.source":   slsaProofRepoURL,
			"org.opencontainers.image.revision": "ffffffffffffffffffffffffffffffffffffff",
		}),
		slsaImageStatementFact("statement-slsa-label", "stmt-slsa-label", testContainerDigest),
		slsaConfigSourceProvenanceFact("provenance-slsa-label", "stmt-slsa-label", slsaProofRepoURL, slsaProofCommit),
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	if decision.SourceRevisionProvenance != containerImageSourceRevisionSLSAProvenanceCommit {
		t.Fatalf("SourceRevisionProvenance = %q, want %q (SLSA must outrank the OCI config label)", decision.SourceRevisionProvenance, containerImageSourceRevisionSLSAProvenanceCommit)
	}
	if decision.SourceRevision != slsaProofCommit {
		t.Fatalf("SourceRevision = %q, want the SLSA commit %q, not the label revision", decision.SourceRevision, slsaProofCommit)
	}
}

// TestApplySLSADigestRevisionOutranksCICDRunCommit is proof-matrix case 3: an
// image with both a SLSA-attested commit and a ci.run-derived commit must
// resolve to the SLSA tier, not ci_run_commit.
func TestApplySLSADigestRevisionOutranksCICDRunCommit(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123def456"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		ociManifestFact("oci-manifest", testContainerDigest),
		slsaImageStatementFact("statement-slsa-cirun", "stmt-slsa-cirun", testContainerDigest),
		slsaConfigSourceProvenanceFact("provenance-slsa-cirun", "stmt-slsa-cirun", slsaProofRepoURL, slsaProofCommit),
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	if decision.SourceRevisionProvenance != containerImageSourceRevisionSLSAProvenanceCommit {
		t.Fatalf("SourceRevisionProvenance = %q, want %q (SLSA must outrank ci_run_commit)", decision.SourceRevisionProvenance, containerImageSourceRevisionSLSAProvenanceCommit)
	}
	if decision.SourceRevision != slsaProofCommit {
		t.Fatalf("SourceRevision = %q, want the SLSA commit %q, not the ci.run commit", decision.SourceRevision, slsaProofCommit)
	}
}

// TestApplySLSADigestRevisionNoRegressionLabelOnly is proof-matrix case 4a:
// an image with only an OCI config source label (no SLSA provenance) must
// stay on oci_config_source_label exactly as before #5456.
func TestApplySLSADigestRevisionNoRegressionLabelOnly(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact("repo://acme/payments-api", slsaProofRepoURL+".git"),
		ociManifestWithConfigLabels("oci-manifest", testContainerDigest, map[string]string{
			"org.opencontainers.image.source":   slsaProofRepoURL,
			"org.opencontainers.image.revision": "ffffffffffffffffffffffffffffffffffffff",
		}),
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	if decision.SourceRevisionProvenance != containerImageSourceRevisionOCIConfigLabel {
		t.Fatalf("SourceRevisionProvenance = %q, want unchanged %q", decision.SourceRevisionProvenance, containerImageSourceRevisionOCIConfigLabel)
	}
	if decision.SourceRevision != "ffffffffffffffffffffffffffffffffffffff" {
		t.Fatalf("SourceRevision = %q, want the label revision unchanged", decision.SourceRevision)
	}
}

// TestApplySLSADigestRevisionNoRegressionCICDRunOnly is proof-matrix case 4b:
// an image with only a ci.run-derived commit (no SLSA provenance) must stay
// on ci_run_commit exactly as before #5456.
func TestApplySLSADigestRevisionNoRegressionCICDRunOnly(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123def456"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	if decision.SourceRevisionProvenance != containerImageSourceRevisionCIRunCommit {
		t.Fatalf("SourceRevisionProvenance = %q, want unchanged %q", decision.SourceRevisionProvenance, containerImageSourceRevisionCIRunCommit)
	}
	if decision.SourceRevision != "abc123def456" {
		t.Fatalf("SourceRevision = %q, want the ci.run commit unchanged", decision.SourceRevision)
	}
}

// TestApplySLSADigestRevisionRefusesAmbiguousCommits is proof-matrix case 5:
// two distinct SLSA-attested commits for the same digest must NOT be
// invented into slsa_provenance_commit; the decision falls back to whatever
// weaker tier (here none) was otherwise resolved.
func TestApplySLSADigestRevisionRefusesAmbiguousCommits(t *testing.T) {
	t.Parallel()

	imageRef := "registry.example.com/team/api@" + testContainerDigest
	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		gitImageRefFact("content-declares", imageRef),
		ociManifestFact("oci-manifest", testContainerDigest),
		slsaImageStatementFact("statement-slsa-a", "stmt-slsa-a", testContainerDigest),
		slsaConfigSourceProvenanceFact("provenance-slsa-a", "stmt-slsa-a", slsaProofRepoURL, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		slsaImageStatementFact("statement-slsa-b", "stmt-slsa-b", testContainerDigest),
		slsaConfigSourceProvenanceFact("provenance-slsa-b", "stmt-slsa-b", slsaProofRepoURL, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	})

	got := decisionsByRef(decisions)
	decision, ok := got[imageRef]
	if !ok {
		t.Fatalf("no decision for %q: %#v", imageRef, got)
	}
	if decision.SourceRevisionProvenance == containerImageSourceRevisionSLSAProvenanceCommit {
		t.Fatalf("SourceRevisionProvenance = %q, want NOT slsa_provenance_commit under ambiguity (two distinct commits for one digest)", decision.SourceRevisionProvenance)
	}
	if decision.SourceRevision != "" {
		t.Fatalf("SourceRevision = %q, want empty under ambiguity (no invented commit)", decision.SourceRevision)
	}
}
