// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package guardset_test

import (
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/internal/guardset"
)

// TestServiceFromImportPath covers the extraction of the service token from a
// runtimebind blank-import path, including the non-runtimebind and malformed
// cases the parser must ignore.
func TestServiceFromImportPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		{
			name: "canonical runtimebind import",
			path: "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam/runtimebind",
			want: "iam",
			ok:   true,
		},
		{
			name: "multi-word service token",
			path: "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatchlogs/runtimebind",
			want: "cloudwatchlogs",
			ok:   true,
		},
		{
			name: "non-runtimebind service import is ignored",
			path: "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam",
			want: "",
			ok:   false,
		},
		{
			name: "unrelated import is ignored",
			path: "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime",
			want: "",
			ok:   false,
		},
		{
			name: "deeper nested package under runtimebind is ignored",
			path: "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam/runtimebind/extra",
			want: "",
			ok:   false,
		},
		{
			name: "empty path",
			path: "",
			want: "",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := guardset.ServiceFromImportPath(tc.path)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ServiceFromImportPath(%q) = (%q, %v), want (%q, %v)", tc.path, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestDiff is the core guard proof. It must report a difference whenever the
// directory set and the import set disagree, and report none when they match.
// The "dir present but not imported" case is the real failure mode the guard
// protects against: a scanner author adds services/<x>/runtimebind/ but forgets
// the bindings.go blank import.
func TestDiff(t *testing.T) {
	cases := []struct {
		name        string
		dirs        []string
		imports     []string
		wantMissing []string // in dirs, absent from imports
		wantExtra   []string // in imports, absent from dirs
	}{
		{
			name:    "identical sets have no diff",
			dirs:    []string{"iam", "s3", "ec2"},
			imports: []string{"ec2", "iam", "s3"},
		},
		{
			name:        "runtimebind dir present but not imported is missing",
			dirs:        []string{"iam", "s3", "newscanner"},
			imports:     []string{"iam", "s3"},
			wantMissing: []string{"newscanner"},
		},
		{
			name:      "import present but no runtimebind dir is extra",
			dirs:      []string{"iam", "s3"},
			imports:   []string{"iam", "s3", "ghost"},
			wantExtra: []string{"ghost"},
		},
		{
			name:        "both directions can diff at once",
			dirs:        []string{"iam", "onlydir"},
			imports:     []string{"iam", "onlyimport"},
			wantMissing: []string{"onlydir"},
			wantExtra:   []string{"onlyimport"},
		},
		{
			name:    "both empty have no diff",
			dirs:    nil,
			imports: nil,
		},
		{
			name:        "duplicate entries are de-duplicated before diffing",
			dirs:        []string{"iam", "iam", "s3"},
			imports:     []string{"iam"},
			wantMissing: []string{"s3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			missing, extra := guardset.Diff(tc.dirs, tc.imports)
			if !equalSorted(missing, tc.wantMissing) {
				t.Errorf("Diff missing = %v, want %v", missing, tc.wantMissing)
			}
			if !equalSorted(extra, tc.wantExtra) {
				t.Errorf("Diff extra = %v, want %v", extra, tc.wantExtra)
			}
		})
	}
}

// TestRuntimebindServiceDirs proves the filesystem reader finds the real
// service runtimebind directories from the live tree, independent of any
// hardcoded list or the registry.
func TestRuntimebindServiceDirs(t *testing.T) {
	servicesDir := liveServicesDir(t)
	dirs, err := guardset.RuntimebindServiceDirs(servicesDir)
	if err != nil {
		t.Fatalf("RuntimebindServiceDirs() error = %v", err)
	}
	if len(dirs) == 0 {
		t.Fatalf("RuntimebindServiceDirs() = empty, want the live scanner set")
	}
	if !contains(dirs, "iam") {
		t.Fatalf("RuntimebindServiceDirs() = %v, want it to include iam", dirs)
	}
}

// TestBindingsImportServices proves the source reader extracts the service set
// from the live bindings.go and that it agrees with the directory walk. This is
// the assertion the guard tests rely on, exercised here against real inputs so
// the helpers are proven before the guard tests wire them together.
func TestBindingsImportServices(t *testing.T) {
	bindingsFile := liveBindingsFile(t)
	imports, err := guardset.BindingsImportServices(bindingsFile)
	if err != nil {
		t.Fatalf("BindingsImportServices() error = %v", err)
	}
	if len(imports) == 0 {
		t.Fatalf("BindingsImportServices() = empty, want the live import set")
	}

	dirs, err := guardset.RuntimebindServiceDirs(liveServicesDir(t))
	if err != nil {
		t.Fatalf("RuntimebindServiceDirs() error = %v", err)
	}
	missing, extra := guardset.Diff(dirs, imports)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("live dirs vs imports diff: missing=%v extra=%v", missing, extra)
	}
}

// liveServicesDir resolves go/internal/collector/awscloud/services from this
// test file's location so the walk does not depend on the working directory.
func liveServicesDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// guardset_test.go -> guardset -> internal -> awsruntime -> awscloud
	awscloudDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "..")
	return filepath.Join(awscloudDir, "services")
}

// liveBindingsFile resolves the live bindings.go source from this test file's
// location.
func liveBindingsFile(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// guardset_test.go -> guardset -> internal -> awsruntime
	awsruntimeDir := filepath.Join(filepath.Dir(currentFile), "..", "..")
	return filepath.Join(awsruntimeDir, "bindings", "bindings.go")
}

func contains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}

func equalSorted(a, b []string) bool {
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	if len(ac) == 0 && len(bc) == 0 {
		return true
	}
	return reflect.DeepEqual(ac, bc)
}
