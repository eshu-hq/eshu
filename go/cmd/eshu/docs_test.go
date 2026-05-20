package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsVerifyCommandIsRegistered(t *testing.T) {
	t.Parallel()

	cmd, _, err := rootCmd.Find([]string{"docs", "verify"})
	if err != nil {
		t.Fatalf("rootCmd.Find(docs verify) error = %v, want nil", err)
	}
	if cmd == nil {
		t.Fatal("docs verify command missing")
	}
}

func TestRunDocsVerifyJSONReportsContradictedClaims(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(docPath, []byte("Run `eshu vaporize all`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	cmd := newDocsVerifyCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--json", "--fail-on", "contradicted"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("docs verify error = nil, want non-zero for contradicted finding")
	}

	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	data := envelope.Data
	if got, want := int(data.Summary.Contradicted), 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	if got := envelope.Error; got == nil || !strings.Contains(got.Message, "contradicted") {
		t.Fatalf("Envelope.Error = %#v, want contradicted failure", got)
	}
}

func TestRunDocsVerifyTextReportsValidCommandAndEndpoint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "runbook.md")
	if err := os.WriteFile(
		docPath,
		[]byte("Run `eshu docs verify .` and call `GET /api/v0/documentation/findings`.\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	cmd := newDocsVerifyCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--limit", "5"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("docs verify error = %v, want nil; output=%s", err, out.String())
	}
	if !strings.Contains(out.String(), "valid=2") {
		t.Fatalf("output = %q, want valid=2", out.String())
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

	vars := map[string]struct{}{}
	for _, name := range docsVerifyEnvironmentTruth(".") {
		vars[name] = struct{}{}
	}
	for _, name := range []string{"ESHU_HOME", "ESHU_QUERY_PROFILE", "ESHU_FACT_STORE_DSN"} {
		if _, ok := vars[name]; !ok {
			t.Fatalf("docsVerifyEnvironmentTruth() missing %s from reference docs", name)
		}
	}
}

func sha256Revision(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
