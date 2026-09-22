// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package guardset_test

import (
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime/internal/guardset"
)

// TestServiceFromImportPath covers the extraction of the service token from a
// bind blank-import path, including the non-bind and malformed
// cases the parser must ignore.
func TestServiceFromImportPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		{
			name: "canonical bind import",
			path: "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/iam/bind",
			want: "iam",
			ok:   true,
		},
		{
			name: "multi-word service token",
			path: "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/cloudwatchlogs/bind",
			want: "cloudwatchlogs",
			ok:   true,
		},
		{
			name: "non-bind service import is ignored",
			path: "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/iam",
			want: "",
			ok:   false,
		},
		{
			name: "unrelated import is ignored",
			path: "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime",
			want: "",
			ok:   false,
		},
		{
			name: "deeper nested package under bind is ignored",
			path: "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/iam/bind/extra",
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
// protects against: a scanner author adds services/<x>/bind/ but forgets
// the all.go blank import.
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
			name:        "bind dir present but not imported is missing",
			dirs:        []string{"iam", "s3", "newscanner"},
			imports:     []string{"iam", "s3"},
			wantMissing: []string{"newscanner"},
		},
		{
			name:      "import present but no bind dir is extra",
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

// TestBindServiceDirs proves the filesystem reader finds the real
// service bind directories from the live tree, independent of any
// hardcoded list or the registry.
func TestBindServiceDirs(t *testing.T) {
	serviceDir := liveServiceDir(t)
	dirs, err := guardset.BindServiceDirs(serviceDir)
	if err != nil {
		t.Fatalf("BindServiceDirs() error = %v", err)
	}
	if len(dirs) == 0 {
		t.Fatalf("BindServiceDirs() = empty, want the live scanner set")
	}
	if !contains(dirs, "iam") {
		t.Fatalf("BindServiceDirs() = %v, want it to include iam", dirs)
	}
}

// TestBindingsImportServices proves the source reader extracts the service set
// from the live all.go and that it agrees with the directory walk. This is
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

	dirs, err := guardset.BindServiceDirs(liveServiceDir(t))
	if err != nil {
		t.Fatalf("BindServiceDirs() error = %v", err)
	}
	missing, extra := guardset.Diff(dirs, imports)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("live dirs vs imports diff: missing=%v extra=%v", missing, extra)
	}
}

// liveServiceDir resolves go/internal/collector/cloud/aws/service from this
// test file's location so the walk does not depend on the working directory.
func liveServiceDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// guardset_test.go -> guardset -> internal -> runtime -> aws
	awsDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "..")
	return filepath.Join(awsDir, "service")
}

// liveBindingsFile resolves the live all.go source from this test file's
// location.
func liveBindingsFile(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// guardset_test.go -> guardset -> internal -> runtime
	runtimeDir := filepath.Join(filepath.Dir(currentFile), "..", "..")
	return filepath.Join(runtimeDir, "bindings", "all.go")
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
