// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperatorDigestArtifactWriterWritesStableJSON(t *testing.T) {
	firstPath := filepath.Join(t.TempDir(), "operator-digest.json")
	secondPath := filepath.Join(t.TempDir(), "operator-digest.json")

	runOperatorDigestArtifact(t, firstPath, "--scope", "repo:demo/service-api", "--profile", "local_authoritative")
	runOperatorDigestArtifact(t, secondPath, "--scope", "repo:demo/service-api", "--profile", "local_authoritative")

	first := readArtifactFile(t, firstPath)
	second := readArtifactFile(t, secondPath)
	if !bytes.Equal(first, second) {
		t.Fatalf("artifact output is not stable:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	var artifact operatorDigestArtifact
	if err := json.Unmarshal(first, &artifact); err != nil {
		t.Fatalf("unmarshal artifact JSON: %v\n%s", err, first)
	}
	if artifact.Schema != operatorDigestArtifactSchema {
		t.Fatalf("schema = %q, want %q", artifact.Schema, operatorDigestArtifactSchema)
	}
	if artifact.Digest.Schema != operatorDigestSchema {
		t.Fatalf("digest schema = %q, want %q", artifact.Digest.Schema, operatorDigestSchema)
	}
	if artifact.Artifact.ID == "" {
		t.Fatal("artifact id is empty")
	}
	if artifact.Artifact.WriterKind != "cli" || artifact.Artifact.Format != "json" {
		t.Fatalf("artifact metadata = %+v, want cli json", artifact.Artifact)
	}
	if artifact.Validation.Status != "passed" {
		t.Fatalf("validation status = %q, want passed", artifact.Validation.Status)
	}
	if len(artifact.SourceRefs) < len(artifact.Digest.SourceRefs) {
		t.Fatalf("source refs = %d, want at least %d", len(artifact.SourceRefs), len(artifact.Digest.SourceRefs))
	}
	if !operatorDigestArtifactHasSourceRef(artifact.SourceRefs, "mcp:get_service_story") {
		t.Fatalf("artifact source refs missing question target: %+v", artifact.SourceRefs)
	}
	if len(artifact.Redaction.AppliedRules) == 0 {
		t.Fatalf("redaction missing applied rules: %+v", artifact.Redaction)
	}
	info, err := os.Stat(firstPath)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("artifact mode = %v, want %v", got, want)
	}
}

func TestOperatorDigestArtifactWriterTightensExistingFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator-digest.json")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed existing artifact: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod existing artifact: %v", err)
	}

	runOperatorDigestArtifact(t, path, "--scope", "repo:demo/service-api")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("artifact mode = %v, want %v", got, want)
	}
}

func TestOperatorDigestArtifactWriterRejectsUnsafeScopeBeforeWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator-digest.json")
	cmd := newOperatorDigestCommand()
	cmd.SetArgs([]string{"--scope", "repo:/Users/example/private", "--artifact-out", path})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("command succeeded with unsafe scope, want error")
	}
	if !strings.Contains(err.Error(), "scope must be share-safe") {
		t.Fatalf("error = %v, want share-safe scope error", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("artifact file exists after failed validation, stat err=%v", statErr)
	}
}

func TestOperatorDigestArtifactValidationRejectsInvalidDigest(t *testing.T) {
	_, err := buildOperatorDigestArtifact(operatorDigest{Schema: "wrong"})
	if err == nil {
		t.Fatal("buildOperatorDigestArtifact succeeded with invalid digest, want error")
	}
	if !strings.Contains(err.Error(), "operator digest schema") {
		t.Fatalf("error = %v, want schema validation error", err)
	}
}

func TestOperatorDigestCommandHasArtifactOutFlag(t *testing.T) {
	cmd := newOperatorDigestCommand()
	if flag := cmd.Flags().Lookup("artifact-out"); flag == nil {
		t.Fatal("operator digest command missing --artifact-out flag")
	}
}

func TestOperatorDigestJSONWithArtifactOutKeepsStdoutDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator-digest.json")
	cmd := newOperatorDigestCommand()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetArgs([]string{"--scope", "repo:demo/service-api", "--json", "--artifact-out", path})
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	var digest operatorDigest
	if err := json.Unmarshal(out.Bytes(), &digest); err != nil {
		t.Fatalf("stdout is not digest JSON: %v\n%s", err, out.String())
	}
	if digest.Schema != operatorDigestSchema {
		t.Fatalf("stdout schema = %q, want %q", digest.Schema, operatorDigestSchema)
	}
	if !strings.Contains(errOut.String(), "wrote operator digest artifact") {
		t.Fatalf("stderr missing artifact write status: %q", errOut.String())
	}
	var artifact operatorDigestArtifact
	if err := json.Unmarshal(readArtifactFile(t, path), &artifact); err != nil {
		t.Fatalf("artifact is not JSON: %v", err)
	}
}

func TestOperatorDigestArtifactAllowsZeroQuestionLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator-digest.json")
	cmd := newOperatorDigestCommand()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetArgs([]string{"--scope", "repo:demo/service-api", "--question-limit", "0", "--json", "--artifact-out", path})
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	var artifact operatorDigestArtifact
	if err := json.Unmarshal(readArtifactFile(t, path), &artifact); err != nil {
		t.Fatalf("artifact is not JSON: %v", err)
	}
	if len(artifact.Digest.SuggestedQuestions) != 0 {
		t.Fatalf("suggested questions = %d, want 0", len(artifact.Digest.SuggestedQuestions))
	}
}

func runOperatorDigestArtifact(t *testing.T, path string, args ...string) {
	t.Helper()
	cmd := newOperatorDigestCommand()
	allArgs := append([]string{}, args...)
	allArgs = append(allArgs, "--artifact-out", path)
	cmd.SetArgs(allArgs)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v", err)
	}
}

func readArtifactFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact %s: %v", path, err)
	}
	return data
}

func operatorDigestArtifactHasSourceRef(refs []operatorDigestSourceRef, id string) bool {
	for _, ref := range refs {
		if ref.ID == id {
			return true
		}
	}
	return false
}
