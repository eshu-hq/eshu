// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReducerPortRootsRequireMatchingSignatures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, relative := range []string{"internal/reducer", "internal/storage/cypher"} {
		if err := os.MkdirAll(filepath.Join(dir, relative), 0o700); err != nil {
			t.Fatalf("create %s: %v", relative, err)
		}
	}
	files := map[string]string{
		"go.mod": "module example.com/rootscan\n\ngo 1.26.6\n",
		"internal/reducer/ports.go": `package reducer

import "context"

type Writer interface {
	Write(context.Context, string) error
}

type ClassifiedFailure interface {
	FailureClass() string
}

type RetryableError interface {
	error
	Retryable() bool
}
`,
		"internal/storage/cypher/writer.go": `package cypher

import "context"

const relationshipTemplate = "MERGE (a)-[rel:REVIEW_PROBE_FLOWS_TO]->(b)"

type WrongWriter struct{}

func (WrongWriter) Write(value int) error {
	_ = relationshipTemplate
	return nil
}

type RightWriter struct{}

func (RightWriter) Write(context.Context, string) error {
	return nil
}

type ClassifiedError struct{}

func (ClassifiedError) Error() string {
	return relationshipTemplate
}

func (ClassifiedError) FailureClass() string {
	return relationshipTemplate
}

func (ClassifiedError) Retryable() bool {
	return true
}
`,
	}
	for relative, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, relative), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", relative, err)
		}
	}

	source, ports := parseCypherPackageWithReducerPorts(t, dir)
	classifications := classifyCypherPorts(source, ports)
	if len(classifications) != 1 {
		t.Fatalf("classifications = %v, want only the signature-matched Write port", classifications)
	}
	if classifications[0].Port != "Write" {
		t.Fatalf("classified port = %q, want Write", classifications[0].Port)
	}
	if classifications[0].WritesEdges {
		t.Errorf("Write reached Cypher through the same-named method with the wrong signature: %q", classifications[0].Evidence)
	}
}
