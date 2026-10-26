// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestManifestsAreAdmittedWithTheStorePolicy pins what the manifest readers
// rely on. The emitter reads the body the content walk hands it, so a registry
// change that stops admitting package.json or go.mod, or binds one to the
// metadata or redact content-store policy, would leave this package reading an
// empty or altered body and emitting nothing, with no error. The test fails
// there instead (#8819: both files are `store`-policy sources).
func TestManifestsAreAdmittedWithTheStorePolicy(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	for path, wantParser := range map[string]string{
		"package.json":                       "json",
		"packages/web/package.json":          "json",
		"go.mod":                             "gomod",
		"services/api/internal/tools/go.mod": "gomod",
		"Dockerfile":                         "__dockerfile__",
		"services/api/Dockerfile.prod":       "__dockerfile__",
		".github/workflows/ci.yml":           "yaml",
		".github/workflows/ci.yaml":          "yaml",
	} {
		definition, ok := registry.LookupByPath(path)
		if !ok || definition.ParserKey != wantParser {
			t.Errorf("registry admits %q as %q (%v), want %q", path, definition.ParserKey, ok, wantParser)
		}
		if got := registry.ContentStoreForPath(path).Policy; got != parser.ContentStoreStore {
			t.Errorf("content-store policy of %q = %q, want %q", path, got, parser.ContentStoreStore)
		}
		if ClassifyPath(path) != FileManifest {
			t.Errorf("ClassifyPath(%q) = %v, want FileManifest", path, ClassifyPath(path))
		}
	}
}

// TestClassifyPathMatchesTheSourceTables keeps ClassifyPath and the two source
// lookups from drifting: every pin name is FilePin, the two manifest names are
// FileManifest, and a path the registry binds to a credential carrier is not
// admitted.
func TestClassifyPathMatchesTheSourceTables(t *testing.T) {
	t.Parallel()

	for name := range pinFileSourceKinds {
		if got := ClassifyPath("a/b/" + name); got != FilePin {
			t.Errorf("ClassifyPath(%q) = %v, want FilePin", name, got)
		}
		if _, ok := SourceKindForPath("a/b/" + name); !ok {
			t.Errorf("SourceKindForPath(%q) = false", name)
		}
	}
	for _, name := range []string{".npmrc", ".yarnrc.yml", ".pypirc", ".netrc", "mise.toml", "package-lock.json", "go.sum", "go.work", "composer.json"} {
		if got := ClassifyPath("a/" + name); got != FileNotAdmitted {
			t.Errorf("ClassifyPath(%q) = %v, want FileNotAdmitted", name, got)
		}
	}
}

// TestClassifyPathAdmitsDockerfileAndWorkflowFiles pins the #8818 admission:
// Dockerfiles (exact and suffixed, any case, outside dependency directories)
// and GitHub Actions workflow files read as manifests. Anything else,
// including composite action files and nested workflow directories, does not.
func TestClassifyPathAdmitsDockerfileAndWorkflowFiles(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"Dockerfile", "dockerfile", "services/api/Dockerfile", "Dockerfile.prod",
		".github/workflows/ci.yml", ".github/workflows/ci.yaml",
	} {
		if got := ClassifyPath(path); got != FileManifest {
			t.Errorf("ClassifyPath(%q) = %v, want FileManifest", path, got)
		}
	}
	for _, path := range []string{
		"vendor/acme/Dockerfile", "node_modules/dep/Dockerfile",
		".github/actions/setup/action.yml", ".github/workflows/team/ci.yml",
		"examples/.github/workflows/ci.yml", "docker-compose.yml",
		"Dockerfile2", "MyDockerfile",
	} {
		if got := ClassifyPath(path); got != FileNotAdmitted {
			t.Errorf("ClassifyPath(%q) = %v, want FileNotAdmitted", path, got)
		}
	}
}
