// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bindings_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/internal/guardset"
)

// TestBindingsImportsEveryRuntimebindDir asserts the set of
// services/<service>/runtimebind/ directories on disk matches the set of
// runtimebind blank imports in bindings.go exactly. The expected set is DERIVED
// from the filesystem plus the bindings.go source, so a new scanner adds zero
// lines to this test.
//
// A non-empty "missing" diff means a scanner package exists on disk whose
// runtimebind was never added to bindings.go (the real wave-1 failure mode). A
// non-empty "extra" diff means bindings.go imports a runtimebind that no longer
// has a directory. Either way the scanner set is inconsistent and this guard
// fails. The Diff helper is unit-tested in internal/guardset, including the
// "dir present but not imported" negative case.
func TestBindingsImportsEveryRuntimebindDir(t *testing.T) {
	dirs, err := guardset.RuntimebindServiceDirs(servicesDir(t))
	if err != nil {
		t.Fatalf("RuntimebindServiceDirs() error = %v", err)
	}
	if len(dirs) == 0 {
		t.Fatalf("RuntimebindServiceDirs() = empty, want the live scanner set")
	}

	imports, err := guardset.BindingsImportServices(bindingsFile(t))
	if err != nil {
		t.Fatalf("BindingsImportServices() error = %v", err)
	}

	missing, extra := guardset.Diff(dirs, imports)
	for _, service := range missing {
		t.Errorf("services/%s/runtimebind/ exists but bindings.go does not blank-import it", service)
	}
	for _, service := range extra {
		t.Errorf("bindings.go blank-imports services/%s/runtimebind but no such directory exists", service)
	}
}

// TestBindingsRegistersEveryImportedKind confirms importing the aggregator
// populates the awsruntime registry with one builder per runtimebind directory.
// It catches a binding that imports but fails to register at init, which the
// import set-diff alone cannot see.
func TestBindingsRegistersEveryImportedKind(t *testing.T) {
	dirs, err := guardset.RuntimebindServiceDirs(servicesDir(t))
	if err != nil {
		t.Fatalf("RuntimebindServiceDirs() error = %v", err)
	}
	if got := len(awsruntime.SupportedServiceKinds()); got != len(dirs) {
		t.Errorf("len(SupportedServiceKinds()) = %d, want %d (one per runtimebind dir %v)", got, len(dirs), dirs)
	}
}

// servicesDir resolves go/internal/collector/awscloud/services from this test
// file's location so the directory walk does not depend on the go test working
// directory.
func servicesDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// bindings_test.go lives in awsruntime/bindings/; services is two levels up
	// under awscloud/.
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "services")
}

// bindingsFile resolves the bindings.go source that lists the runtimebind
// blank imports from this test file's location.
func bindingsFile(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Join(filepath.Dir(currentFile), "bindings.go")
}
