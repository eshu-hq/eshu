// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opdigest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteArtifactWritesStableJSON(t *testing.T) {
	options, err := OptionsFromFlags("repo:demo/service-api", "local_authoritative", DefaultQuestionMax)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}
	digest := BuildDigest(options)

	firstPath := filepath.Join(t.TempDir(), "operator-digest.json")
	secondPath := filepath.Join(t.TempDir(), "operator-digest.json")
	if err := WriteArtifact(firstPath, digest); err != nil {
		t.Fatalf("WriteArtifact(first): %v", err)
	}
	if err := WriteArtifact(secondPath, digest); err != nil {
		t.Fatalf("WriteArtifact(second): %v", err)
	}

	first := readArtifactFile(t, firstPath)
	second := readArtifactFile(t, secondPath)
	if !bytes.Equal(first, second) {
		t.Fatalf("artifact output is not stable:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	var artifact Artifact
	if err := json.Unmarshal(first, &artifact); err != nil {
		t.Fatalf("unmarshal artifact JSON: %v\n%s", err, first)
	}
	if artifact.Schema != artifactSchema {
		t.Fatalf("schema = %q, want %q", artifact.Schema, artifactSchema)
	}
	if artifact.Digest.Schema != Schema {
		t.Fatalf("digest schema = %q, want %q", artifact.Digest.Schema, Schema)
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
	if !hasSourceRef(artifact.SourceRefs, "mcp:get_service_story") {
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

// TestWriteArtifactDoesNotDoubleWrapPathInError pins the operator-facing text
// of a failed `eshu report --artifact-out`. The os/File errors are already
// *fs.PathError, which renders as "open <path>: <cause>", and WriteArtifact
// adds the "write operator digest artifact: " prefix -- so wrapping inside
// writeArtifactFile would print the path twice.
//
// This is a regression test for the extraction into internal/cli/opdigest.
// go/.golangci.yml switches wrapcheck off for cmd/ by file path, through the
// `- path: 'cmd/'` entry under linters.exclusions.rules, so moving this code
// out of cmd/eshu made the linter demand a wrap, and satisfying it silently
// changed what an operator sees. No build, vet, test, or lint gate compares
// error strings against the previous binary, so nothing else catches this.
func TestWriteArtifactDoesNotDoubleWrapPathInError(t *testing.T) {
	dir := t.TempDir()

	options, err := OptionsFromFlags("repo:demo/service-api", DefaultProfile, DefaultQuestionMax)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}
	digest := BuildDigest(options)

	sealed := filepath.Join(dir, "sealed")
	if err := os.Mkdir(sealed, 0o500); err != nil {
		t.Fatalf("seed read-only parent directory: %v", err)
	}
	// Restore the write bit so the t.TempDir cleanup can remove it.
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o700) })

	for _, testCase := range []struct {
		name    string
		path    string
		precond func(t *testing.T)
	}{
		{name: "target_is_a_directory", path: dir},
		{name: "parent_does_not_exist", path: filepath.Join(dir, "absent", "artifact.json")},
		{
			name: "parent_is_not_writable",
			path: filepath.Join(sealed, "artifact.json"),
			precond: func(t *testing.T) {
				t.Helper()
				// Root ignores the directory write bit, so the create
				// succeeds and there is no error to inspect. CI containers
				// commonly run as uid 0.
				if os.Geteuid() == 0 {
					t.Skip("running as root: a 0500 parent directory still accepts the write")
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.precond != nil {
				testCase.precond(t)
			}
			err := WriteArtifact(testCase.path, digest)
			if err == nil {
				t.Fatalf("WriteArtifact(%q) = nil, want an error", testCase.path)
			}
			var pathErr *fs.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("error %v does not carry an *fs.PathError", err)
			}
			if pathErr.Path != testCase.path {
				t.Fatalf("*fs.PathError names %q, want %q", pathErr.Path, testCase.path)
			}
			// Compare the whole operator-facing string, not a prefix and
			// not a count of path occurrences. A wrap that neither repeats
			// the path nor disturbs the prefix -- "open artifact file: %w"
			// is the obvious one -- passes both of those weaker checks
			// while still changing what the operator reads.
			want := "write operator digest artifact: " + pathErr.Error()
			if got := err.Error(); got != want {
				t.Fatalf("error text =\n  %q\nwant\n  %q", got, want)
			}
		})
	}
}

func TestWriteArtifactTightensExistingFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator-digest.json")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed existing artifact: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod existing artifact: %v", err)
	}

	options, err := OptionsFromFlags("repo:demo/service-api", DefaultProfile, DefaultQuestionMax)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}
	if err := WriteArtifact(path, BuildDigest(options)); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("artifact mode = %v, want %v", got, want)
	}
}

func TestBuildArtifactRejectsInvalidDigest(t *testing.T) {
	_, err := BuildArtifact(Digest{Schema: "wrong"})
	if err == nil {
		t.Fatal("BuildArtifact succeeded with invalid digest, want error")
	}
	if !strings.Contains(err.Error(), "operator digest schema") {
		t.Fatalf("error = %v, want schema validation error", err)
	}
}

func TestBuildArtifactAllowsZeroQuestionLimit(t *testing.T) {
	options, err := OptionsFromFlags("repo:demo/service-api", DefaultProfile, 0)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}
	artifact, err := BuildArtifact(BuildDigest(options))
	if err != nil {
		t.Fatalf("BuildArtifact: %v", err)
	}
	if len(artifact.Digest.SuggestedQuestions) != 0 {
		t.Fatalf("suggested questions = %d, want 0", len(artifact.Digest.SuggestedQuestions))
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

func hasSourceRef(refs []SourceRef, id string) bool {
	for _, ref := range refs {
		if ref.ID == id {
			return true
		}
	}
	return false
}
