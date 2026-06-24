// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestDocsVerifyContainerImageResolverScansLazily(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "deployment.yaml"),
		[]byte("image: ghcr.io/acme/api:1.2.3\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(deployment.yaml) error = %v, want nil", err)
	}

	resolver := docsVerifyLocalContainerImageResolver(root)
	if resolver == nil {
		t.Fatal("docsVerifyContainerImageResolver() = nil, want resolver")
	}
	if err := os.WriteFile(
		filepath.Join(root, "deployment.yaml"),
		[]byte("image: ghcr.io/acme/api:2.0.0\n"),
		0o600,
	); err != nil {
		t.Fatalf("rewrite deployment.yaml error = %v, want nil", err)
	}

	resolution := resolver(doctruth.DocumentInput{}, "ghcr.io/acme/api:2.0.0")
	if !resolution.Supported || !resolution.Exists {
		t.Fatalf("resolution = %#v, want lazy scan to see rewritten manifest", resolution)
	}
}

func TestRunDocsVerifyChecksContainerImageClaimsAgainstAPITruth(t *testing.T) {
	t.Parallel()

	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		if got, want := r.Header.Get("Accept"), eshuEnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v0/supply-chain/container-images/identities"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("limit"), "1"; got != want {
			t.Fatalf("limit = %q, want %q", got, want)
		}
		switch r.URL.Query().Get("image_ref") {
		case "ghcr.io/acme/api:1.2.3":
			_, _ = w.Write([]byte(`{"data":{"identities":[{"identity_id":"image-1","image_ref":"ghcr.io/acme/api:1.2.3","outcome":"tag_resolved"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact"},"error":null}`))
		case "ghcr.io/acme/missing:9.9.9":
			_, _ = w.Write([]byte(`{"data":{"identities":[],"count":0,"limit":1,"truncated":false},"truth":{"level":"exact"},"error":null}`))
		default:
			t.Fatalf("unexpected image_ref query %q", r.URL.Query().Get("image_ref"))
		}
	}))
	defer server.Close()

	root := t.TempDir()
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
	cmd.SetArgs([]string{
		docPath,
		"--json",
		"--image-truth",
		"api",
		"--service-url",
		server.URL,
		"--fail-on",
		"contradicted",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("docs verify error = nil, want non-zero for API contradicted image finding")
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
	if got, want := len(requests), 2; got != want {
		t.Fatalf("API requests = %d, want %d; requests=%#v", got, want, requests)
	}
	assertDocsVerifyFinding(t, envelope.Data.Findings, "container_image_ref", "ghcr.io/acme/api:1.2.3", "valid")
	assertDocsVerifyFinding(t, envelope.Data.Findings, "container_image_ref", "ghcr.io/acme/missing:9.9.9", "contradicted")
}

func TestRunDocsVerifyMarksAPIImageTruthErrorsMissingEvidence(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	root := t.TempDir()
	docPath := filepath.Join(root, "README.md")
	if err := os.WriteFile(docPath, []byte("Deploy `ghcr.io/acme/api:1.2.3`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v, want nil", err)
	}

	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--json", "--image-truth", "api", "--service-url", server.URL})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("docs verify error = %v, want nil for missing evidence; output=%s", err, out.String())
	}

	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	assertDocsVerifyFinding(t, envelope.Data.Findings, "container_image_ref", "ghcr.io/acme/api:1.2.3", "missing_evidence")
}

func TestDocsVerifyFreshnessIncludesEffectiveImageTruthMode(t *testing.T) {
	t.Parallel()

	documents := []doctruth.DocumentInput{{
		Path:       "README.md",
		SourceURI:  "file:///repo/README.md",
		RevisionID: "sha256:doc",
	}}
	local := docsInventoryFreshnessHint(documents, 256*1024, 50, "local")
	api := docsInventoryFreshnessHint(documents, 256*1024, 50, "api")
	if local == api {
		t.Fatalf("freshness local = freshness api = %q, want image truth source in fingerprint", local)
	}
	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	if err := cmd.Flags().Set("image-truth", "auto"); err != nil {
		t.Fatalf("Set(image-truth) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("service-url", "https://api.example.test"); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	opts, err := docsVerifyOptionsFromCommand(cmd, []string{"README.md"})
	if err != nil {
		t.Fatalf("docsVerifyOptionsFromCommand() error = %v, want nil", err)
	}
	if got, want := effectiveDocsVerifyImageTruth(cmd, opts.ImageTruth), "api"; got != want {
		t.Fatalf("effectiveDocsVerifyImageTruth(auto with service-url) = %q, want %q", got, want)
	}
	if got, want := docsInventoryFreshnessHint(documents, 256*1024, 50, effectiveDocsVerifyImageTruth(cmd, opts.ImageTruth)), api; got != want {
		t.Fatalf("auto+service-url freshness = %q, want api freshness %q", got, want)
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
