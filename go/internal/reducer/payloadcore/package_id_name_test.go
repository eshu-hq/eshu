// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// A Maven package ID is maven://<registry>/<group>:<artifact>, and the
// registry may itself carry a path ("repo.maven.apache.org/maven2"). The
// package name is the coordinate alone; the registry path must never leak into
// it (issue #9286). Other ecosystems keep the whole path after the authority
// because their names can contain slashes.
func TestPackageNameFromPackageIDDropsMavenRegistryPath(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"default registry with path", "maven://repo.maven.apache.org/maven2/com.google.code.gson:gson", "com.google.code.gson:gson"},
		{"registry without path", "maven://repo1.maven.org/org.example:lib", "org.example:lib"},
		{"deep custom registry path", "maven://nexus.example/repository/maven-public/org.example:lib", "org.example:lib"},
		{"scheme is matched case-insensitively", "MAVEN://repo.maven.apache.org/maven2/org.example:lib", "org.example:lib"},
		{"npm scoped name keeps its slash", "npm://registry.npmjs.org/@scope/pkg", "@scope/pkg"},
		{"golang module keeps its path", "gomod://proxy.golang.org/github.com/org/repo", "github.com/org/repo"},
		{"no path after the authority", "maven://repo.maven.apache.org", ""},
		{"no scheme", "org.example:lib", ""},
		{"blank", "  ", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := payloadcore.PackageNameFromPackageID(test.in); got != test.want {
				t.Fatalf("PackageNameFromPackageID(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
