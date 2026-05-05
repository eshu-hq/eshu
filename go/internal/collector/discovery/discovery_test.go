package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveRepositoryFileSetsHonorsHiddenPolicySupportedMatcherAndOrdering(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")
	nestedRepo := filepath.Join(repoA, "submodule")

	mustMkdirGit(t, repoA)
	mustMkdirGit(t, repoB)
	mustMkdirGit(t, nestedRepo)

	mustWriteFile(t, filepath.Join(root, ".gitignore"), "*.py\n")
	mustWriteFile(t, filepath.Join(repoA, ".gitignore"), "ignored.py\n")
	mustWriteFile(t, filepath.Join(repoA, "kept.py"), "print('kept')\n")
	mustWriteFile(t, filepath.Join(repoA, "ignored.py"), "print('ignored')\n")
	mustWriteFile(t, filepath.Join(repoA, "Dockerfile"), "FROM scratch\n")
	mustWriteFile(t, filepath.Join(repoA, ".github", "workflows", "deploy.yaml"), "name: deploy\n")
	mustWriteFile(t, filepath.Join(repoA, ".github", "secret.py"), "print('secret')\n")
	mustWriteFile(t, filepath.Join(repoA, ".hidden", "skip.py"), "print('skip')\n")
	mustWriteFile(t, filepath.Join(nestedRepo, "nested.py"), "print('nested')\n")
	mustWriteFile(t, filepath.Join(repoB, "b.py"), "print('b')\n")

	got, err := ResolveRepositoryFileSets(
		root,
		supportedPathMatcher,
		Options{
			IgnoredDirs:             []string{".git", "node_modules", "vendor"},
			IgnoreHidden:            true,
			PreservedHiddenPrefixes: []string{".github/workflows"},
			HonorGitignore:          false,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSets() error = %v, want nil", err)
	}

	want := []RepoFileSet{
		{
			RepoRoot: mustResolvePath(t, repoA),
			Files: []string{
				mustResolvePath(t, filepath.Join(repoA, ".github", "workflows", "deploy.yaml")),
				mustResolvePath(t, filepath.Join(repoA, "Dockerfile")),
				mustResolvePath(t, filepath.Join(repoA, "ignored.py")),
				mustResolvePath(t, filepath.Join(repoA, "kept.py")),
			},
		},
		{
			RepoRoot: mustResolvePath(t, nestedRepo),
			Files: []string{
				mustResolvePath(t, filepath.Join(nestedRepo, "nested.py")),
			},
		},
		{
			RepoRoot: mustResolvePath(t, repoB),
			Files: []string{
				mustResolvePath(t, filepath.Join(repoB, "b.py")),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveRepositoryFileSets() = %#v, want %#v", got, want)
	}
}

func TestResolveRepositoryFileSetsHonorsRepoLocalGitignoreScopingAndNestedNegation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "generated")

	mustMkdirGit(t, repo)

	mustWriteFile(t, filepath.Join(root, ".gitignore"), "*.py\n")
	mustWriteFile(t, filepath.Join(repo, ".gitignore"), "ignored.py\n")
	mustWriteFile(t, filepath.Join(nested, ".gitignore"), "*\n!keep.py\n")
	mustWriteFile(t, filepath.Join(repo, "kept.py"), "print('kept')\n")
	mustWriteFile(t, filepath.Join(repo, "ignored.py"), "print('ignored')\n")
	mustWriteFile(t, filepath.Join(nested, "keep.py"), "print('keep')\n")
	mustWriteFile(t, filepath.Join(nested, "drop.py"), "print('drop')\n")

	got, err := ResolveRepositoryFileSets(
		root,
		func(path string) bool {
			return filepath.Ext(path) == ".py"
		},
		Options{
			IgnoredDirs:    []string{".git"},
			IgnoreHidden:   false,
			HonorGitignore: true,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSets() error = %v, want nil", err)
	}

	want := []RepoFileSet{
		{
			RepoRoot: mustResolvePath(t, repo),
			Files: []string{
				mustResolvePath(t, filepath.Join(repo, "generated", "keep.py")),
				mustResolvePath(t, filepath.Join(repo, "kept.py")),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveRepositoryFileSets() = %#v, want %#v", got, want)
	}
}

func TestResolveRepositoryFileSetsHonorsRepoLocalEshuIgnoreScopingAndNestedNegation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "generated")

	mustMkdirGit(t, repo)

	mustWriteFile(t, filepath.Join(root, ".eshuignore"), "*.py\n")
	mustWriteFile(t, filepath.Join(repo, ".eshuignore"), "ignored.py\n")
	mustWriteFile(t, filepath.Join(nested, ".eshuignore"), "*\n!keep.py\n")
	mustWriteFile(t, filepath.Join(repo, "kept.py"), "print('kept')\n")
	mustWriteFile(t, filepath.Join(repo, "ignored.py"), "print('ignored')\n")
	mustWriteFile(t, filepath.Join(nested, "keep.py"), "print('keep')\n")
	mustWriteFile(t, filepath.Join(nested, "drop.py"), "print('drop')\n")

	stats, got, err := ResolveRepositoryFileSetsWithStats(
		root,
		func(path string) bool {
			return filepath.Ext(path) == ".py"
		},
		Options{
			IgnoredDirs:     []string{".git"},
			IgnoreHidden:    false,
			HonorEshuIgnore: true,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSetsWithStats() error = %v, want nil", err)
	}

	want := []RepoFileSet{
		{
			RepoRoot: mustResolvePath(t, repo),
			Files: []string{
				mustResolvePath(t, filepath.Join(repo, "generated", "keep.py")),
				mustResolvePath(t, filepath.Join(repo, "kept.py")),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveRepositoryFileSetsWithStats() = %#v, want %#v", got, want)
	}
	if got, want := stats.FilesSkippedEshuIgnore, 2; got != want {
		t.Fatalf("FilesSkippedEshuIgnore = %d, want %d", got, want)
	}
	if got, want := stats.TotalFilesSkipped(), 2; got != want {
		t.Fatalf("TotalFilesSkipped() = %d, want %d", got, want)
	}
}

func TestResolveRepositoryFileSetsSkipsSymlinkTargetsOutsideRepoRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	nestedRepo := filepath.Join(repo, "packages", "nested")

	mustMkdirGit(t, repo)
	mustMkdirGit(t, nestedRepo)

	kept := filepath.Join(repo, "app.py")
	mustWriteFile(t, kept, "print('inside')\n")
	external := filepath.Join(root, "shared.py")
	mustWriteFile(t, external, "print('outside')\n")

	escaped := filepath.Join(repo, "currency.py")
	if err := os.Symlink(external, escaped); err != nil {
		t.Skipf("symlinks unavailable in test environment: %v", err)
	}

	mustWriteFile(t, filepath.Join(nestedRepo, "nested.py"), "print('nested')\n")

	got, err := ResolveRepositoryFileSets(
		repo,
		func(path string) bool {
			return filepath.Ext(path) == ".py"
		},
		Options{
			IgnoredDirs:    []string{".git"},
			IgnoreHidden:   false,
			HonorGitignore: false,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSets() error = %v, want nil", err)
	}

	want := []RepoFileSet{
		{
			RepoRoot: mustResolvePath(t, repo),
			Files: []string{
				mustResolvePath(t, kept),
			},
		},
		{
			RepoRoot: mustResolvePath(t, nestedRepo),
			Files: []string{
				mustResolvePath(t, filepath.Join(nestedRepo, "nested.py")),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveRepositoryFileSets() = %#v, want %#v", got, want)
	}
}

func TestResolveRepositoryFileSetsWithStatsReportsPerNameSkipCounts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustMkdirGit(t, repo)

	// Create files in ignored directories.
	mustWriteFile(t, filepath.Join(repo, "node_modules", "react", "index.js"), "module.exports = {}")
	mustWriteFile(t, filepath.Join(repo, "node_modules", "vue", "index.js"), "module.exports = {}")
	mustWriteFile(t, filepath.Join(repo, "vendor", "autoload.php"), "<?php")
	mustWriteFile(t, filepath.Join(repo, "__pycache__", "mod.cpython-311.pyc"), "")
	mustWriteFile(t, filepath.Join(repo, ".terraform", "providers", "main.tf"), "")

	// Create files with ignored extensions.
	mustWriteFile(t, filepath.Join(repo, "app.min.js"), "minified")
	mustWriteFile(t, filepath.Join(repo, "style.min.css"), "minified")
	mustWriteFile(t, filepath.Join(repo, "build.log"), "output")
	mustWriteFile(t, filepath.Join(repo, "result.out"), "output")
	mustWriteFile(t, filepath.Join(repo, "app.js.map"), "sourcemap")

	// Create kept files.
	mustWriteFile(t, filepath.Join(repo, "main.py"), "print('hello')")
	mustWriteFile(t, filepath.Join(repo, "lib", "utils.py"), "def f(): pass")

	stats, fileSets, err := ResolveRepositoryFileSetsWithStats(
		root,
		func(path string) bool { return true },
		Options{
			IgnoredDirs:       []string{".git", "node_modules", "vendor", "__pycache__", ".terraform"},
			IgnoredExtensions: []string{".min.js", ".min.css", ".log", ".out", ".map"},
			HonorGitignore:    false,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSetsWithStats() error = %v", err)
	}

	// Verify per-directory-name counts.
	if got := stats.DirsSkippedByName["node_modules"]; got != 1 {
		t.Errorf("DirsSkippedByName[node_modules] = %d, want 1", got)
	}
	if got := stats.DirsSkippedByName["vendor"]; got != 1 {
		t.Errorf("DirsSkippedByName[vendor] = %d, want 1", got)
	}
	if got := stats.DirsSkippedByName["__pycache__"]; got != 1 {
		t.Errorf("DirsSkippedByName[__pycache__] = %d, want 1", got)
	}
	if got := stats.DirsSkippedByName[".terraform"]; got != 1 {
		t.Errorf("DirsSkippedByName[.terraform] = %d, want 1", got)
	}
	if got := stats.TotalDirsSkipped(); got < 4 {
		t.Errorf("TotalDirsSkipped() = %d, want >= 4", got)
	}

	// Verify per-extension counts.
	if got := stats.FilesSkippedByExtension[".min.js"]; got != 1 {
		t.Errorf("FilesSkippedByExtension[.min.js] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByExtension[".min.css"]; got != 1 {
		t.Errorf("FilesSkippedByExtension[.min.css] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByExtension[".log"]; got != 1 {
		t.Errorf("FilesSkippedByExtension[.log] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByExtension[".out"]; got != 1 {
		t.Errorf("FilesSkippedByExtension[.out] = %d, want 1", got)
	}
	if got := stats.FilesSkippedByExtension[".map"]; got != 1 {
		t.Errorf("FilesSkippedByExtension[.map] = %d, want 1", got)
	}
	if got := stats.TotalFilesSkipped(); got != 5 {
		t.Errorf("TotalFilesSkipped() = %d, want 5", got)
	}

	// Verify the kept files made it through.
	if len(fileSets) == 0 {
		t.Fatal("expected at least one file set")
	}
	totalKept := 0
	for _, fs := range fileSets {
		totalKept += len(fs.Files)
	}
	if totalKept != 2 {
		t.Errorf("kept file count = %d, want 2", totalKept)
	}
}

func TestResolveRepositoryFileSetsPrunesUserPathGlobsWithPreservedSubtree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustMkdirGit(t, repo)
	mustWriteFile(t, filepath.Join(repo, "src", "app.php"), "<?php\nfunction app_authored() {}\n")
	mustWriteFile(t, filepath.Join(repo, "src", "wp-content", "plugins", "wordpress-seo", "src", "blocks.php"), "<?php\nfunction yoast_vendor() {}\n")
	mustWriteFile(t, filepath.Join(repo, "src", "wp-content", "plugins", "custom-authored", "plugin.php"), "<?php\nfunction custom_authored() {}\n")

	stats, fileSets, err := ResolveRepositoryFileSetsWithStats(
		repo,
		func(path string) bool { return filepath.Ext(path) == ".php" },
		Options{
			IgnoredDirs: []string{".git"},
			IgnoredPathGlobs: []PathGlobRule{
				{Pattern: "src/wp-content/plugins/**", Reason: "wordpress plugin"},
			},
			PreservedPathGlobs: []string{"src/wp-content/plugins/custom-authored/**"},
			HonorGitignore:     false,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSetsWithStats() error = %v", err)
	}

	if got := stats.DirsSkippedByUser["wordpress-plugin"]; got != 1 {
		t.Fatalf("DirsSkippedByUser[wordpress-plugin] = %d, want 1", got)
	}
	if got := stats.TotalDirsSkipped(); got < 2 {
		t.Fatalf("TotalDirsSkipped() = %d, want at least built-in .git plus user subtree", got)
	}

	if got, want := countFileSetFiles(fileSets), 2; got != want {
		t.Fatalf("kept file count = %d, want %d; fileSets=%v", got, want, fileSets)
	}
	for _, wantSuffix := range []string{
		"src/app.php",
		"src/wp-content/plugins/custom-authored/plugin.php",
	} {
		if !repoFileSetsContainSuffix(fileSets, wantSuffix) {
			t.Fatalf("fileSets missing %q; fileSets=%v", wantSuffix, fileSets)
		}
	}
}

func mustMkdirGit(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git for %q: %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}

func mustResolvePath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve path %q: %v", path, err)
	}
	return resolved
}

func countFileSetFiles(fileSets []RepoFileSet) int {
	total := 0
	for _, fileSet := range fileSets {
		total += len(fileSet.Files)
	}
	return total
}

func repoFileSetsContainSuffix(fileSets []RepoFileSet, suffix string) bool {
	for _, fileSet := range fileSets {
		for _, file := range fileSet.Files {
			if strings.HasSuffix(filepath.ToSlash(file), suffix) {
				return true
			}
		}
	}
	return false
}

func supportedPathMatcher(path string) bool {
	switch filepath.Base(path) {
	case "Dockerfile":
		return true
	}
	switch filepath.Ext(path) {
	case ".py", ".yaml":
		return true
	default:
		return false
	}
}
