// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// retryableMat returns a minimal canonical materialization that exercises the
// repository, directory, file, and entity write phases so each dispatch path in
// CanonicalNodeWriter.Write reaches its executor.
func retryableMat() canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID:      "scope-retry",
		GenerationID: "gen-retry",
		RepoID:       "repo-retry",
		RepoPath:     "/repos/retry",
		Repository: &canonical.RepositoryRow{
			RepoID:    "repo-retry",
			Name:      "retry",
			Path:      "/repos/retry",
			LocalPath: "/repos/retry",
			RepoSlug:  "org/retry",
		},
		Directories: []canonical.DirectoryRow{
			{Path: "/repos/retry/src", Name: "src", ParentPath: "/repos/retry", RepoID: "repo-retry", Depth: 0},
		},
		Files: []canonical.FileRow{
			{Path: "/repos/retry/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-retry", DirPath: "/repos/retry/src"},
		},
		Entities: []canonical.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "main", FilePath: "/repos/retry/src/main.go", RelativePath: "src/main.go", StartLine: 5, EndLine: 10, Language: "go", RepoID: "repo-retry"},
		},
	}
}

// txLimitErr is the driver error session.ExecuteWrite returns after exhausting
// its internal retry budget under sustained NornicDB write contention. It is the
// production shape behind canonical-projection dead letters in issue #3483.
func txLimitErr() error {
	return &neo4jdriver.TransactionExecutionLimit{
		Cause: "timeout (exceeded max retry time: 30s)",
		Errors: []error{
			newNeo4jError("Neo.TransientError.Transaction.DeadlockDetected", "deadlock cycle"),
		},
	}
}

// deadletterGroupExecutor implements Executor and GroupExecutor and fails every
// call with a TransactionExecutionLimit, driving the atomic dispatch path.
type deadletterGroupExecutor struct{ err error }

func (e deadletterGroupExecutor) Execute(context.Context, Statement) error { return e.err }
func (e deadletterGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	return e.err
}

// deadletterPhaseGroupExecutor implements Executor and PhaseGroupExecutor and
// fails every call with a TransactionExecutionLimit, driving the phase-group
// path.
type deadletterPhaseGroupExecutor struct{ err error }

func (e deadletterPhaseGroupExecutor) Execute(context.Context, Statement) error { return e.err }
func (e deadletterPhaseGroupExecutor) ExecutePhaseGroup(context.Context, []Statement) error {
	return e.err
}

// deadletterExecutor implements only Executor and fails every call, driving the
// sequential dispatch path.
type deadletterExecutor struct{ err error }

func (e deadletterExecutor) Execute(context.Context, Statement) error { return e.err }

// TestCanonicalNodeWriterWritePropagatesRetryable proves that a transient
// NornicDB retry-exhaustion error escaping any canonical write dispatch path
// reaches the projector queue as a retryable failure, so the work item requeues
// with backpressure instead of silently dead-lettering. This is the #3483
// "no silent dead-letter on write conflict" contract.
func TestCanonicalNodeWriterWritePropagatesRetryable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		exec Executor
	}{
		{name: "atomic_group", exec: deadletterGroupExecutor{err: txLimitErr()}},
		{name: "phase_group", exec: deadletterPhaseGroupExecutor{err: txLimitErr()}},
		{name: "sequential", exec: deadletterExecutor{err: txLimitErr()}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			writer := NewCanonicalNodeWriter(tc.exec, 500, nil)
			err := writer.Write(context.Background(), retryableMat())
			if err == nil {
				t.Fatalf("Write() error = nil, want retryable failure")
			}
			if !failure.IsRetryable(err) {
				t.Fatalf("failure.IsRetryable(%v) = false, want true: canonical write retry-exhaustion must requeue, not dead-letter", err)
			}
			// A genuinely non-retryable schema error must stay terminal.
			var limit *neo4jdriver.TransactionExecutionLimit
			if !errors.As(err, &limit) {
				t.Fatalf("error chain lost the underlying TransactionExecutionLimit: %v", err)
			}
		})
	}
}

// TestCanonicalNodeWriterWriteKeepsTerminalErrorsTerminal proves the fix does
// not loosen retry classification: a constraint-validation error (genuinely
// terminal) must not be reclassified as retryable.
func TestCanonicalNodeWriterWriteKeepsTerminalErrorsTerminal(t *testing.T) {
	t.Parallel()

	terminal := newNeo4jError("Neo.ClientError.Schema.ConstraintValidationFailed", "constraint failed")
	writer := NewCanonicalNodeWriter(deadletterGroupExecutor{err: terminal}, 500, nil)

	err := writer.Write(context.Background(), retryableMat())
	if err == nil {
		t.Fatal("Write() error = nil, want terminal failure")
	}
	if failure.IsRetryable(err) {
		t.Fatalf("failure.IsRetryable(%v) = true, want false: schema constraint failures must stay terminal", err)
	}
}
