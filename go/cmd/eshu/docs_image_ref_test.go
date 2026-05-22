package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

func TestRunDocsVerifyChecksContainerImageClaims(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	manifestDir := filepath.Join(root, "deploy")
	if err := os.Mkdir(manifestDir, 0o700); err != nil {
		t.Fatalf("Mkdir(deploy) error = %v, want nil", err)
	}
	if err := os.WriteFile(
		filepath.Join(manifestDir, "deployment.yaml"),
		[]byte("containers:\n- image: ghcr.io/acme/api:1.2.3\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(deployment.yaml) error = %v, want nil", err)
	}
	docPath := filepath.Join(root, "README.md")
	if err := os.WriteFile(
		docPath,
		[]byte(""+
			"Deploy `ghcr.io/acme/api:1.2.3`.\n"+
			"Do not publish `ghcr.io/acme/missing:9.9.9`.\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v, want nil", err)
	}

	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--json", "--fail-on", "contradicted"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("docs verify error = nil, want non-zero for contradicted image finding")
	}

	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	if got, want := envelope.Data.Summary.Valid, 1; got != want {
		t.Fatalf("Summary.Valid = %d, want %d", got, want)
	}
	if got, want := envelope.Data.Summary.Contradicted, 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	assertDocsVerifyFinding(t, envelope.Data.Findings, "container_image_ref", "ghcr.io/acme/api:1.2.3", "valid")
	assertDocsVerifyFinding(t, envelope.Data.Findings, "container_image_ref", "ghcr.io/acme/missing:9.9.9", "contradicted")
}

func TestDocsVerifyContainerImageTruthMarksOversizedManifestIncomplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "deployment.yaml"),
		bytes.Repeat([]byte("x"), docsVerifyImageTruthMaxFileBytes+1),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(deployment.yaml) error = %v, want nil", err)
	}

	_, complete := docsVerifyContainerImageTruth(root)
	if complete {
		t.Fatal("docsVerifyContainerImageTruth complete = true, want false for oversized manifest")
	}
}

func assertDocsVerifyFinding(
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
