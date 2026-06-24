// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestRuntimeProjectLocksPackageRegistryIdentitiesAroundCanonicalWrite(t *testing.T) {
	t.Parallel()

	locker := &recordingPackageRegistryIdentityLocker{}
	canonicalWriter := &lockAwareCanonicalWriter{locker: locker}
	runtime := Runtime{
		CanonicalWriter:               canonicalWriter,
		ContentWriter:                 &recordingContentWriter{},
		IntentWriter:                  &recordingIntentWriter{},
		PackageRegistryIdentityLocker: locker,
	}

	_, err := runtime.Project(
		context.Background(),
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), packageRegistryDependencyFact()),
	)
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}
	if got, want := locker.calls, [][]string{{
		"package://npm/registry.npmjs.org/@scope/pkg",
		"package://npm/registry.npmjs.org/left-pad",
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("locked package IDs = %#v, want %#v", got, want)
	}
	if !canonicalWriter.calledWhileLocked {
		t.Fatal("canonical writer was not called while package identity lock was active")
	}
}

func TestRuntimeProjectSkipsPackageRegistryIdentityLockWithoutPackageRows(t *testing.T) {
	t.Parallel()

	locker := &recordingPackageRegistryIdentityLocker{}
	runtime := Runtime{
		CanonicalWriter:               &recordingCanonicalWriter{},
		ContentWriter:                 &recordingContentWriter{},
		PackageRegistryIdentityLocker: locker,
	}

	_, err := runtime.Project(
		context.Background(),
		scope.IngestionScope{
			ScopeID:       "repo-scope-1",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-1",
			Metadata:      map[string]string{"repo_id": "repo-1"},
		},
		scope.ScopeGeneration{
			GenerationID: "repo-generation-1",
			ScopeID:      "repo-scope-1",
			ObservedAt:   time.Date(2026, time.June, 5, 10, 0, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.June, 5, 10, 1, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
		[]facts.Envelope{{
			FactID:       "repository-1",
			ScopeID:      "repo-scope-1",
			GenerationID: "repo-generation-1",
			FactKind:     "repository",
			ObservedAt:   time.Date(2026, time.June, 5, 10, 0, 0, 0, time.UTC),
			Payload: map[string]any{
				"repo_id": "repo-1",
				"path":    "org/repo",
			},
		}},
	)
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}
	if len(locker.calls) != 0 {
		t.Fatalf("package identity locker calls = %#v, want none", locker.calls)
	}
}

type recordingPackageRegistryIdentityLocker struct {
	active bool
	calls  [][]string
}

func (l *recordingPackageRegistryIdentityLocker) WithPackageRegistryIdentityLocks(
	ctx context.Context,
	packageIDs []string,
	fn func(context.Context) error,
) error {
	l.active = true
	l.calls = append(l.calls, append([]string(nil), packageIDs...))
	defer func() { l.active = false }()
	return fn(ctx)
}

type lockAwareCanonicalWriter struct {
	locker            *recordingPackageRegistryIdentityLocker
	calledWhileLocked bool
}

func (w *lockAwareCanonicalWriter) Write(_ context.Context, _ CanonicalMaterialization) error {
	w.calledWhileLocked = w.locker != nil && w.locker.active
	return nil
}
