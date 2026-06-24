// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCosignProvenanceVerifierVerifiesSignatureAndAttestation(t *testing.T) {
	cosignPath, logPath := writeFakeCosign(t)
	verifier := CosignProvenanceVerifier{Command: cosignPath}
	requirement := ProvenanceRequirement{
		CertificateIdentity: "https://github.com/eshu-hq/eshu/.github/workflows/release.yml@refs/tags/v0.1.0",
		OIDCIssuer:          "https://token.actions.githubusercontent.com",
	}

	if err := verifier.VerifyProvenance(context.Background(), validManifest(), requirement); err != nil {
		t.Fatalf("VerifyProvenance() error = %v, want nil", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v, want nil", err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"verify ",
		"verify-attestation ",
		"--certificate-identity " + requirement.CertificateIdentity,
		"--certificate-oidc-issuer " + requirement.OIDCIssuer,
		"--type " + DefaultProvenancePredicateType,
		"--check-claims",
		"-a eshu.component.id=dev.eshu.collector.aws",
		"-a eshu.component.publisher=eshu-hq",
		"-a eshu.component.version=0.1.0",
		"-a eshu.component.fact-kinds=dev.eshu.aws.cloud_resource",
		"-a eshu.component.fact-schema-versions=dev.eshu.aws.cloud_resource:1.0.0",
		"-a eshu.component.fact-source-confidence=dev.eshu.aws.cloud_resource:reported",
		"-a eshu.component.reducer-phases=cloud_resource_uid:canonical_nodes_committed",
		"-a eshu.component.metrics-prefix=eshu_dp_aws_",
		validManifest().Spec.Artifacts[0].Image,
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("cosign args missing %q:\n%s", want, log)
		}
	}
}

func TestCosignProvenanceVerifierClassifiesSignatureFailure(t *testing.T) {
	cosignPath, _ := writeFakeCosign(t)
	t.Setenv("COSIGN_FAIL_VERIFY", "1")
	verifier := CosignProvenanceVerifier{Command: cosignPath}
	requirement := ProvenanceRequirement{
		CertificateIdentity: "https://github.com/eshu-hq/eshu/.github/workflows/release.yml@refs/tags/v0.1.0",
		OIDCIssuer:          "https://token.actions.githubusercontent.com",
	}

	err := verifier.VerifyProvenance(context.Background(), validManifest(), requirement)
	if err == nil {
		t.Fatal("VerifyProvenance() error = nil, want signature failure")
	}
	if got, want := ErrorCodeOf(err), ErrorCodeProvenanceInvalid; got != want {
		t.Fatalf("ErrorCodeOf(error) = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "registry-token") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("VerifyProvenance() error leaked stderr: %v", err)
	}
}

func TestCosignProvenanceVerifierClassifiesUnsupportedAttestation(t *testing.T) {
	cosignPath, _ := writeFakeCosign(t)
	t.Setenv("COSIGN_FAIL_ATTESTATION", "1")
	verifier := CosignProvenanceVerifier{Command: cosignPath}
	requirement := ProvenanceRequirement{
		CertificateIdentity: "https://github.com/eshu-hq/eshu/.github/workflows/release.yml@refs/tags/v0.1.0",
		OIDCIssuer:          "https://token.actions.githubusercontent.com",
	}

	err := verifier.VerifyProvenance(context.Background(), validManifest(), requirement)
	if err == nil {
		t.Fatal("VerifyProvenance() error = nil, want attestation failure")
	}
	if got, want := ErrorCodeOf(err), ErrorCodeUnsupportedProvenance; got != want {
		t.Fatalf("ErrorCodeOf(error) = %q, want %q", got, want)
	}
}

func writeFakeCosign(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "cosign-args.log")
	cosignPath := filepath.Join(dir, "cosign")
	body := `#!/bin/sh
printf '%s\n' "$*" >> "$COSIGN_ARGS_LOG"
case "$1" in
  verify)
    if [ "$COSIGN_FAIL_VERIFY" = "1" ]; then
      printf '%s\n' 'registry-token=secret-from-stderr' >&2
      exit 17
    fi
    ;;
  verify-attestation)
    if [ "$COSIGN_FAIL_ATTESTATION" = "1" ]; then
      printf '%s\n' 'registry-token=secret-from-stderr' >&2
      exit 18
    fi
    ;;
esac
printf '%s\n' '[]'
`
	if err := os.WriteFile(cosignPath, []byte(body), 0o700); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	t.Setenv("COSIGN_ARGS_LOG", logPath)
	return cosignPath, logPath
}
