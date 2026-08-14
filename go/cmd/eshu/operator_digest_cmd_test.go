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

	"github.com/eshu-hq/eshu/go/internal/cli/opdigest"
)

func TestOperatorDigestCommandHasArtifactOutFlag(t *testing.T) {
	cmd := newOperatorDigestCommand()
	if flag := cmd.Flags().Lookup("artifact-out"); flag == nil {
		t.Fatal("operator digest command missing --artifact-out flag")
	}
}

func TestOperatorDigestCommandRejectsEmptyScope(t *testing.T) {
	cmd := newOperatorDigestCommand()
	cmd.SetArgs([]string{"--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("command succeeded with empty scope, want error")
	}
	if !strings.Contains(err.Error(), "scope is required") {
		t.Fatalf("error = %v, want required scope error", err)
	}
}

// TestOperatorDigestCommandForwardsFlagsToDigest proves the wrapper reads
// --scope/--profile/--question-limit and passes them through to
// opdigest.OptionsFromFlags/BuildDigest unchanged; the validation and
// rendering behavior itself is opdigest's, covered in
// go/internal/cli/opdigest.
func TestOperatorDigestCommandForwardsFlagsToDigest(t *testing.T) {
	cmd := newOperatorDigestCommand()
	cmd.SetArgs([]string{"--scope", "service:payments-api", "--profile", "local_authoritative", "--question-limit", "2", "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v\n%s", err, out.String())
	}
	var digest opdigest.Digest
	if err := json.Unmarshal(out.Bytes(), &digest); err != nil {
		t.Fatalf("stdout is not digest JSON: %v\n%s", err, out.String())
	}
	if digest.Scope.Type != "service" || digest.Scope.Label != "payments-api" {
		t.Fatalf("scope = %+v, want service payments-api", digest.Scope)
	}
	if digest.Profile != "local_authoritative" {
		t.Fatalf("profile = %q, want local_authoritative", digest.Profile)
	}
	if got := len(digest.SuggestedQuestions); got != 2 {
		t.Fatalf("suggested questions = %d, want 2 (--question-limit not forwarded)", got)
	}
}

// TestOperatorDigestCommandDefaultOutputIsText proves the wrapper renders
// opdigest.RenderText (not JSON) when --json is not set.
func TestOperatorDigestCommandDefaultOutputIsText(t *testing.T) {
	cmd := newOperatorDigestCommand()
	cmd.SetArgs([]string{"--scope", "repo:demo/service-api"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Operator digest") {
		t.Fatalf("default output is not the text report:\n%s", out.String())
	}
	var probe json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &probe); err == nil {
		t.Fatalf("default output parsed as JSON, want plain text:\n%s", out.String())
	}
}

// TestOperatorDigestJSONWithArtifactOutKeepsStdoutDigest proves --artifact-out
// does not change stdout's --json digest, and that the write-status line
// goes to stderr, not stdout.
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
	var digest opdigest.Digest
	if err := json.Unmarshal(out.Bytes(), &digest); err != nil {
		t.Fatalf("stdout is not digest JSON: %v\n%s", err, out.String())
	}
	if digest.Schema != opdigest.Schema {
		t.Fatalf("stdout schema = %q, want %q", digest.Schema, opdigest.Schema)
	}
	if !strings.Contains(errOut.String(), "wrote operator digest artifact") {
		t.Fatalf("stderr missing artifact write status: %q", errOut.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var artifact opdigest.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("artifact is not JSON: %v", err)
	}
}

// TestOperatorDigestArtifactWriterRejectsUnsafeScopeBeforeWrite proves the
// wrapper validates the scope (via opdigest.OptionsFromFlags) before it
// ever calls opdigest.WriteArtifact, so a rejected scope leaves no file
// behind.
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
