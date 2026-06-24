// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsVerifyEnvironmentTruthReadsPublicDocsReferenceFromSiblingPage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	referenceDir := filepath.Join(dir, "docs", "public", "reference")
	if err := os.MkdirAll(referenceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(
		filepath.Join(referenceDir, "environment-variables.md"),
		[]byte("| `ESHU_PUBLIC_DOCS_TEST_ENV` | test-only | verifier fixture |\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(environment-variables.md) error = %v, want nil", err)
	}
	pageDir := filepath.Join(dir, "docs", "public", "run-locally")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(run-locally) error = %v, want nil", err)
	}
	pagePath := filepath.Join(pageDir, "docker-compose.md")
	if err := os.WriteFile(pagePath, []byte("Use `ESHU_PUBLIC_DOCS_TEST_ENV`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(docker-compose.md) error = %v, want nil", err)
	}

	if !containsEnvTruth(pagePath, "ESHU_PUBLIC_DOCS_TEST_ENV") {
		t.Fatalf("docsVerifyEnvironmentTruth() did not read docs/public/reference/environment-variables.md")
	}
}

func TestDocsVerifyEnvironmentTruthReadsSplitEnvironmentReferencePages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	referenceDir := filepath.Join(dir, "docs", "public", "reference")
	if err := os.MkdirAll(referenceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(
		filepath.Join(referenceDir, "environment-variables.md"),
		[]byte("Route map only.\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(environment-variables.md) error = %v, want nil", err)
	}
	if err := os.WriteFile(
		filepath.Join(referenceDir, "environment-runtime-storage.md"),
		[]byte("| `ESHU_SPLIT_REFERENCE_TEST_ENV` | test-only | verifier fixture |\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(environment-runtime-storage.md) error = %v, want nil", err)
	}

	pagePath := filepath.Join(dir, "docs", "public", "reference", "environment-runtime-storage.md")
	if !containsEnvTruth(pagePath, "ESHU_SPLIT_REFERENCE_TEST_ENV") {
		t.Fatalf("docsVerifyEnvironmentTruth() did not read split environment reference pages")
	}
}

func TestDocsVerifyEnvironmentTruthIgnoresWildcardEnvironmentFamilies(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	referenceDir := filepath.Join(dir, "docs", "public", "reference")
	if err := os.MkdirAll(referenceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(
		filepath.Join(referenceDir, "environment-runtime-storage.md"),
		[]byte("Family `ESHU_WORKFLOW_COORDINATOR_*`; concrete `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE`.\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(environment-runtime-storage.md) error = %v, want nil", err)
	}

	pagePath := filepath.Join(dir, "docs", "public", "reference", "environment-runtime-storage.md")
	if containsEnvTruth(pagePath, "ESHU_WORKFLOW_COORDINATOR_") {
		t.Fatal("docsVerifyEnvironmentTruth() treated wildcard family prefix as a concrete environment variable")
	}
	if !containsEnvTruth(pagePath, "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE") {
		t.Fatal("docsVerifyEnvironmentTruth() did not keep the concrete environment variable")
	}
}

func TestReadDocumentInputBoundsContentButHashesFullFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "space name.md")
	fullContent := []byte("`eshu docs verify .`\nthis suffix must only affect revision\n")
	if err := os.WriteFile(docPath, fullContent, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	doc, err := readDocumentInput(docPath, 8)
	if err != nil {
		t.Fatalf("readDocumentInput() error = %v, want nil", err)
	}

	if got, want := len(doc.Content), 8; got != want {
		t.Fatalf("len(Content) = %d, want %d", got, want)
	}
	if !doc.ContentTruncated {
		t.Fatal("ContentTruncated = false, want true")
	}
	if got, want := doc.RevisionID, sha256Revision(fullContent); got != want {
		t.Fatalf("RevisionID = %q, want full-file hash %q", got, want)
	}
	if strings.Contains(doc.SourceURI, " ") {
		t.Fatalf("SourceURI = %q, want escaped file URI", doc.SourceURI)
	}
	if !strings.HasPrefix(doc.SourceURI, "file:///") {
		t.Fatalf("SourceURI = %q, want canonical file URI", doc.SourceURI)
	}
}

func TestInventoryDocsStopsAtLimitAndMarksTruncated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("`eshu docs verify .`\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v, want nil", name, err)
		}
	}

	inventory, err := inventoryDocs(docsVerifyOptions{Path: dir, Limit: 1, MaxDocumentBytes: 1024})
	if err != nil {
		t.Fatalf("inventoryDocs() error = %v, want nil", err)
	}
	if got, want := len(inventory.Documents), 1; got != want {
		t.Fatalf("len(Documents) = %d, want %d", got, want)
	}
	if !inventory.Truncated {
		t.Fatal("Truncated = false, want true when the file limit stops traversal")
	}
}

func TestDocsVerifyEnvironmentTruthUsesReferenceDocs(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"ESHU_HOME", "ESHU_QUERY_PROFILE", "ESHU_FACT_STORE_DSN"} {
		if !containsEnvTruth(".", name) {
			t.Fatalf("docsVerifyEnvironmentTruth() missing %s from reference docs", name)
		}
	}
}

func containsEnvTruth(path string, want string) bool {
	for _, name := range docsVerifyEnvironmentTruth(path) {
		if name == want {
			return true
		}
	}
	return false
}

func sha256Revision(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
