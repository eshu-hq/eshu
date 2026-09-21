// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import "testing"

func TestExactOwnedDependencyVersionAllowsSemverPrereleaseVersions(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"1.0.0-next.1",
		"1.0.0-git.1",
		"1.0.0+git.sha",
	} {
		got, ok := ExactOwnedDependencyVersion(raw)
		if !ok {
			t.Fatalf("ExactOwnedDependencyVersion(%q) ok = false, want true", raw)
		}
		if got != raw {
			t.Fatalf("ExactOwnedDependencyVersion(%q) = %q, want %q", raw, got, raw)
		}
	}

	if got, ok := ExactOwnedDependencyVersion("^1.0.0-next.1"); ok {
		t.Fatalf("ExactOwnedDependencyVersion() = %q, want range rejection", got)
	}
	if got, ok := ExactOwnedDependencyVersion("git+https://github.com/acme/pkg.git"); ok {
		t.Fatalf("ExactOwnedDependencyVersion() = %q, want git URL rejection", got)
	}
	if got, ok := ExactOwnedDependencyVersion("git://github.com/acme/pkg.git"); ok {
		t.Fatalf("ExactOwnedDependencyVersion() = %q, want git URL rejection", got)
	}
	if got, ok := ExactOwnedDependencyVersion("gitlab:acme/pkg"); ok {
		t.Fatalf("ExactOwnedDependencyVersion() = %q, want git URL rejection", got)
	}
	if got, ok := ExactOwnedDependencyVersion("release-2026-05-24"); ok {
		t.Fatalf("ExactOwnedDependencyVersion() = %q, want non-semver rejection", got)
	}
}
