// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bindings_test

import (
	"path/filepath"
	stdruntime "runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime/bindings"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime/internal/guardset"
)

// TestBindingsImportsEveryRuntimebindDir asserts the set of
// service/<service>/bind/ directories on disk matches the set of
// bind blank imports in all.go exactly. The expected set is DERIVED
// from the filesystem plus the all.go source, so a new scanner adds zero
// lines to this test.
//
// A non-empty "missing" diff means a scanner package exists on disk whose
// bind was never added to all.go (the real wave-1 failure mode). A
// non-empty "extra" diff means all.go imports a bind that no longer
// has a directory. Either way the scanner set is inconsistent and this guard
// fails. The Diff helper is unit-tested in internal/guardset, including the
// "dir present but not imported" negative case.
func TestBindingsImportsEveryRuntimebindDir(t *testing.T) {
	dirs, err := guardset.BindServiceDirs(servicesDir(t))
	if err != nil {
		t.Fatalf("BindServiceDirs() error = %v", err)
	}
	if len(dirs) == 0 {
		t.Fatalf("BindServiceDirs() = empty, want the live scanner set")
	}

	imports, err := guardset.BindingsImportServices(bindingsFile(t))
	if err != nil {
		t.Fatalf("BindingsImportServices() error = %v", err)
	}

	missing, extra := guardset.Diff(dirs, imports)
	for _, service := range missing {
		t.Errorf("service/%s/bind/ exists but all.go does not blank-import it", service)
	}
	for _, service := range extra {
		t.Errorf("all.go blank-imports service/%s/bind but no such directory exists", service)
	}
}

// TestBindingsRegistersEveryImportedKind confirms importing the aggregator
// populates the runtime registry with one builder per bind directory.
// It catches a binding that imports but fails to register at init, which the
// import set-diff alone cannot see.
func TestBindingsRegistersEveryImportedKind(t *testing.T) {
	dirs, err := guardset.BindServiceDirs(servicesDir(t))
	if err != nil {
		t.Fatalf("BindServiceDirs() error = %v", err)
	}
	if got := len(runtime.SupportedServiceKinds()); got != len(dirs) {
		t.Errorf("len(SupportedServiceKinds()) = %d, want %d (one per bind dir %v)", got, len(dirs), dirs)
	}
}

// servicesDir resolves go/internal/collector/cloud/aws/service from this test
// file's location so the directory walk does not depend on the go test working
// directory.
func servicesDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := stdruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// bindings_test.go lives in runtime/bindings/; services is two levels up
	// under aws/.
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "service")
}

// bindingsFile resolves the all.go source that lists the bind
// blank imports from this test file's location.
func bindingsFile(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := stdruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Join(filepath.Dir(currentFile), "all.go")
}
