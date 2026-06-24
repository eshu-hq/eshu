package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositorySelectorCanonicalizesOnlyPathFields(t *testing.T) {
	root := t.TempDir()
	selector := filepath.Join(root, "repo")
	cleanEquivalentPath := root + string(os.PathSeparator) + "nested" + //nolint:gocritic // preferFilepathJoin: the test deliberately constructs an un-cleaned path equivalent to filepath.Join, to assert the selector canonicalizes both forms.
		string(os.PathSeparator) + ".." + string(os.PathSeparator) + "repo"

	repo := repositorySelectorEntry{
		ID:       "repo-id",
		Name:     cleanEquivalentPath,
		RepoSlug: cleanEquivalentPath,
	}
	if repositorySelectorMatches(repo, selector) {
		t.Fatalf("repositorySelectorMatches() matched path-equivalent name/slug, want exact non-path matching only")
	}

	repo.LocalPath = cleanEquivalentPath
	if !repositorySelectorMatches(repo, selector) {
		t.Fatalf("repositorySelectorMatches() = false, want canonical local_path match")
	}
}

func TestRepositorySelectorMatchesExactNonPathSelectors(t *testing.T) {
	repo := repositorySelectorEntry{
		ID:       "repo-id",
		Name:     "repo-name",
		RepoSlug: "org/repo-name",
	}
	for _, selector := range []string{repo.ID, repo.Name, repo.RepoSlug} {
		if !repositorySelectorMatches(repo, selector) {
			t.Fatalf("repositorySelectorMatches(%q) = false, want exact non-path match", selector)
		}
	}
}
