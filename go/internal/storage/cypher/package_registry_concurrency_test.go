// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterSerializesConcurrentDuplicatePackageUIDs(t *testing.T) {
	t.Parallel()

	exec := newBlockingPackageUIDExecutor()
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- writer.Write(context.Background(), packageRegistryPackageOnlyMaterialization(
			"scope-a",
			"generation-a",
			"npm://registry.npmjs.org/eslint",
		))
	}()

	select {
	case <-exec.firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first package write did not enter executor")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- writer.Write(context.Background(), packageRegistryPackageOnlyMaterialization(
			"scope-b",
			"generation-b",
			"npm://registry.npmjs.org/eslint",
		))
	}()

	if !waitForPackageRegistryIdentityRefCount(writer, "npm://registry.npmjs.org/eslint", 2, 2*time.Second) {
		close(exec.release)
		t.Fatal("second package write did not queue on duplicate package UID lock")
	}
	select {
	case <-exec.secondEntered:
		close(exec.release)
		t.Fatal("second duplicate package UID write entered executor before the first write released")
	default:
	}
	close(exec.release)

	if err := <-firstDone; err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if exec.overlapped.Load() {
		t.Fatal("concurrent duplicate package UID writes overlapped in the backend")
	}
}

func TestCanonicalNodeWriterSerializesDependencyTargetPackageUIDs(t *testing.T) {
	t.Parallel()

	exec := newBlockingPackageUIDExecutor()
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- writer.Write(context.Background(), packageRegistryDependencyTargetMaterialization(
			"scope-a",
			"generation-a",
			"npm://registry.npmjs.org/@aws-sdk/property-provider",
		))
	}()

	select {
	case <-exec.firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first dependency-target write did not enter executor")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- writer.Write(context.Background(), packageRegistryPackageOnlyMaterialization(
			"scope-b",
			"generation-b",
			"npm://registry.npmjs.org/@aws-sdk/property-provider",
		))
	}()

	if !waitForPackageRegistryIdentityRefCount(writer, "npm://registry.npmjs.org/@aws-sdk/property-provider", 2, 2*time.Second) {
		close(exec.release)
		t.Fatal("second dependency-target package write did not queue on duplicate package UID lock")
	}
	select {
	case <-exec.secondEntered:
		close(exec.release)
		t.Fatal("second dependency-target package UID write entered executor before the first write released")
	default:
	}
	close(exec.release)

	if err := <-firstDone; err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if exec.overlapped.Load() {
		t.Fatal("concurrent dependency-target package UID writes overlapped in the backend")
	}
}

func TestCanonicalNodeWriterAllowsConcurrentDistinctPackageUIDs(t *testing.T) {
	t.Parallel()

	exec := newBlockingPackageUIDExecutor()
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- writer.Write(context.Background(), packageRegistryPackageOnlyMaterialization(
			"scope-a",
			"generation-a",
			"npm://registry.npmjs.org/eslint",
		))
	}()

	select {
	case <-exec.firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first package write did not enter executor")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- writer.Write(context.Background(), packageRegistryPackageOnlyMaterialization(
			"scope-b",
			"generation-b",
			"npm://registry.npmjs.org/mocha",
		))
	}()

	select {
	case <-exec.secondEntered:
	case <-time.After(2 * time.Second):
		close(exec.release)
		t.Fatal("distinct package UID write was serialized behind unrelated UID")
	}

	close(exec.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if exec.overlapped.Load() {
		t.Fatal("distinct package UID overlap should not be recorded as duplicate overlap")
	}
}

func TestPackageRegistryIdentityLockKeysCoverPackageSources(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		PackageRegistryPackages: []projector.PackageRegistryPackageRow{{
			UID: " npm://registry.npmjs.org/eslint ",
		}},
		PackageRegistryVersions: []projector.PackageRegistryVersionRow{{
			PackageID: "npm://registry.npmjs.org/mocha",
		}},
		PackageRegistryDependencies: []projector.PackageRegistryDependencyRow{{
			PackageID:           "npm://registry.npmjs.org/@aws-sdk/util-utf8-browser",
			DependencyPackageID: "npm://registry.npmjs.org/@aws-sdk/property-provider",
		}},
	}

	got := uniqueSortedPackageRegistryIdentityKeys(packageRegistryIdentityLockKeys(mat))
	want := []string{
		"npm://registry.npmjs.org/@aws-sdk/property-provider",
		"npm://registry.npmjs.org/@aws-sdk/util-utf8-browser",
		"npm://registry.npmjs.org/eslint",
		"npm://registry.npmjs.org/mocha",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("package registry identity lock keys = %#v, want %#v", got, want)
	}
}

func packageRegistryPackageOnlyMaterialization(scopeID, generationID, uid string) projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:      scopeID,
		GenerationID: generationID,
		PackageRegistryPackages: []projector.PackageRegistryPackageRow{{
			UID:              uid,
			Ecosystem:        "npm",
			Registry:         "https://registry.npmjs.org",
			RawName:          uid,
			NormalizedName:   uid,
			SourceFactID:     generationID + "-package",
			StableFactKey:    uid,
			SourceSystem:     "package_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			CollectorKind:    "package_registry",
			ObservedAt:       time.Date(2026, time.June, 2, 14, 21, 10, 0, time.UTC),
		}},
	}
}

func packageRegistryDependencyTargetMaterialization(scopeID, generationID, uid string) projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:      scopeID,
		GenerationID: generationID,
		PackageRegistryDependencies: []projector.PackageRegistryDependencyRow{{
			UID:                  generationID + "-dependency",
			DependencyPackageID:  uid,
			DependencyEcosystem:  "npm",
			DependencyRegistry:   "https://registry.npmjs.org",
			DependencyNormalized: uid,
			SourceFactID:         generationID + "-dependency",
			StableFactKey:        generationID + "-dependency",
			SourceSystem:         "package_registry",
			SourceConfidence:     facts.SourceConfidenceReported,
			CollectorKind:        "package_registry",
		}},
	}
}

type blockingPackageUIDExecutor struct {
	mu            sync.Mutex
	active        map[string]int
	calls         atomic.Int32
	overlapped    atomic.Bool
	firstEntered  chan struct{}
	secondEntered chan struct{}
	release       chan struct{}
}

func newBlockingPackageUIDExecutor() *blockingPackageUIDExecutor {
	return &blockingPackageUIDExecutor{
		active:        make(map[string]int),
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		release:       make(chan struct{}),
	}
}

func waitForPackageRegistryIdentityRefCount(
	writer *CanonicalNodeWriter,
	key string,
	want int,
	timeout time.Duration,
) bool {
	deadline := time.After(timeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		if packageRegistryIdentityRefCount(writer, key) >= want {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-ticker.C:
		}
	}
}

func packageRegistryIdentityRefCount(writer *CanonicalNodeWriter, key string) int {
	if writer == nil || writer.packageRegistryLocks == nil {
		return 0
	}
	writer.packageRegistryLocks.mu.Lock()
	defer writer.packageRegistryLocks.mu.Unlock()

	lock := writer.packageRegistryLocks.locks[key]
	if lock == nil {
		return 0
	}
	return lock.refs
}

func (e *blockingPackageUIDExecutor) Execute(context.Context, Statement) error {
	return nil
}

func (e *blockingPackageUIDExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	uids := packageUIDsFromStatements(stmts)
	e.mu.Lock()
	for _, uid := range uids {
		if e.active[uid] > 0 {
			e.overlapped.Store(true)
		}
		e.active[uid]++
	}
	call := e.calls.Add(1)
	e.mu.Unlock()

	switch call {
	case 1:
		close(e.firstEntered)
		select {
		case <-ctx.Done():
			e.deactivate(uids)
			return ctx.Err()
		case <-e.release:
		}
	case 2:
		close(e.secondEntered)
	}

	e.deactivate(uids)
	return nil
}

func (e *blockingPackageUIDExecutor) deactivate(uids []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, uid := range uids {
		e.active[uid]--
		if e.active[uid] <= 0 {
			delete(e.active, uid)
		}
	}
}

func packageUIDsFromStatements(stmts []Statement) []string {
	seen := make(map[string]struct{})
	for _, stmt := range stmts {
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			continue
		}
		for _, row := range rows {
			for _, key := range []string{"uid", "dependency_package_id"} {
				uid, ok := row[key].(string)
				if !ok || strings.TrimSpace(uid) == "" {
					continue
				}
				seen[uid] = struct{}{}
			}
		}
	}
	uids := make([]string, 0, len(seen))
	for uid := range seen {
		uids = append(uids, uid)
	}
	return uids
}
