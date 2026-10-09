// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"reflect"
	"testing"
)

func TestGoImportPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		key  string
		want string
	}{
		{"function", "scip-go gomod github.com/acme/lib/client Do().", "github.com/acme/lib/client"},
		{"method", "scip-go gomod github.com/acme/lib/client Client#Request().", "github.com/acme/lib/client"},
		{"standard library", "scip-go gomod context Background().", "context"},
		{"module root package", "scip-go gomod github.com/acme/lib New().", "github.com/acme/lib"},
		{"scip-go index key with a version", "scip-go gomod github.com/acme/lib v1.2.3 client/Do().", "github.com/acme/lib"},
		{"extra spaces before the symbol", "scip-go gomod github.com/acme/lib   Do().", "github.com/acme/lib"},
		{"package key", "package:@acme/logging#Logger", ""},
		{"other indexer", "scip-java maven org.acme/core org.acme/Thing#run().", ""},
		{"prefix only", "scip-go gomod ", ""},
		{"prefix without the trailing space", "scip-go gomod", ""},
		{"import path without a symbol", "scip-go gomod github.com/acme/lib", ""},
		{"import path with trailing spaces only", "scip-go gomod github.com/acme/lib  ", ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := GoImportPath(tc.key); got != tc.want {
				t.Fatalf("GoImportPath(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestGoModuleCandidates(t *testing.T) {
	t.Parallel()

	want := []string{
		"github.com/acme/mono/svc/api",
		"github.com/acme/mono/svc",
		"github.com/acme/mono",
		"github.com/acme",
		"github.com",
	}
	if got := GoModuleCandidates("github.com/acme/mono/svc/api"); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if got := GoModuleCandidates("context"); !reflect.DeepEqual(got, []string{"context"}) {
		t.Fatalf("single-segment candidates = %v, want [context]", got)
	}
	if got := GoModuleCandidates(""); len(got) != 0 {
		t.Fatalf("empty import path candidates = %v, want none", got)
	}
	// A shared string prefix without a path boundary is not a candidate.
	for _, candidate := range GoModuleCandidates("github.com/acme/libext") {
		if candidate == "github.com/acme/lib" {
			t.Fatalf("candidates %v include github.com/acme/lib, which is not a path prefix of libext", GoModuleCandidates("github.com/acme/libext"))
		}
	}
}

func TestSplit(t *testing.T) {
	t.Parallel()

	packageKeys, goKeys, otherKeys := Split([]string{
		"scip-go gomod github.com/acme/lib Do().",
		"package:@acme/logging#Logger",
		"scip-java maven org.acme/core org.acme/Thing#run().",
		"scip-go gomod context Background().",
		"scip-go gomod github.com/acme/lib",
		"scip-python python . . django/`conf/`settings#DEBUG.",
	})
	if want := []string{"package:@acme/logging#Logger"}; !reflect.DeepEqual(packageKeys, want) {
		t.Fatalf("package keys = %v, want %v", packageKeys, want)
	}
	if want := []string{"scip-go gomod github.com/acme/lib Do().", "scip-go gomod context Background()."}; !reflect.DeepEqual(goKeys, want) {
		t.Fatalf("go keys = %v, want %v", goKeys, want)
	}
	wantOther := []string{
		"scip-java maven org.acme/core org.acme/Thing#run().",
		"scip-go gomod github.com/acme/lib",
		"scip-python python . . django/`conf/`settings#DEBUG.",
	}
	if !reflect.DeepEqual(otherKeys, wantOther) {
		t.Fatalf("other keys = %v, want %v (a scip-go key without a symbol is not a definition key and keeps the corpus-wide scan)", otherKeys, wantOther)
	}
}

func TestPackageName(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{
		"package:@acme/logging#Logger": "@acme/logging",
		"package:lodash#map":           "lodash",
		"package: lodash #map":         "lodash",
		"package:lodash":               "",
		"package:#map":                 "",
		"package:lodash#":              "",
		"package:":                     "",
	} {
		if got := PackageName(key); got != want {
			t.Errorf("PackageName(%q) = %q, want %q", key, got, want)
		}
	}
}
