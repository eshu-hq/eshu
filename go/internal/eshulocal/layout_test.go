package eshulocal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveWorkspaceRoot(t *testing.T) {
	t.Run("explicit workspace root wins", func(t *testing.T) {
		base := t.TempDir()
		workspaceRoot := filepath.Join(base, "monofolder")
		startPath := filepath.Join(workspaceRoot, "repo", "pkg")
		if err := os.MkdirAll(startPath, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v, want nil", err)
		}

		got, err := ResolveWorkspaceRoot(startPath, workspaceRoot)
		if err != nil {
			t.Fatalf("ResolveWorkspaceRoot() error = %v, want nil", err)
		}

		want := mustEvalSymlinks(t, workspaceRoot)
		if got != want {
			t.Fatalf("ResolveWorkspaceRoot() = %q, want %q", got, want)
		}
	})

	t.Run("eshu yaml ancestor beats git ancestor", func(t *testing.T) {
		base := t.TempDir()
		workspaceRoot := filepath.Join(base, "monofolder")
		repoRoot := filepath.Join(workspaceRoot, "repo")
		startPath := filepath.Join(repoRoot, "pkg")
		if err := os.MkdirAll(startPath, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v, want nil", err)
		}
		if err := os.WriteFile(filepath.Join(workspaceRoot, ".eshu.yaml"), []byte("workspace: true\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(.eshu.yaml) error = %v, want nil", err)
		}
		if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
			t.Fatalf("Mkdir(.git) error = %v, want nil", err)
		}

		got, err := ResolveWorkspaceRoot(startPath, "")
		if err != nil {
			t.Fatalf("ResolveWorkspaceRoot() error = %v, want nil", err)
		}

		want := mustEvalSymlinks(t, workspaceRoot)
		if got != want {
			t.Fatalf("ResolveWorkspaceRoot() = %q, want %q", got, want)
		}
	})

	t.Run("nearest git ancestor is used when no eshu yaml exists", func(t *testing.T) {
		base := t.TempDir()
		repoRoot := filepath.Join(base, "repo")
		startPath := filepath.Join(repoRoot, "pkg")
		if err := os.MkdirAll(startPath, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v, want nil", err)
		}
		if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
			t.Fatalf("Mkdir(.git) error = %v, want nil", err)
		}

		got, err := ResolveWorkspaceRoot(startPath, "")
		if err != nil {
			t.Fatalf("ResolveWorkspaceRoot() error = %v, want nil", err)
		}

		want := mustEvalSymlinks(t, repoRoot)
		if got != want {
			t.Fatalf("ResolveWorkspaceRoot() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to invocation directory when no markers exist", func(t *testing.T) {
		startPath := filepath.Join(t.TempDir(), "repo", "pkg")
		if err := os.MkdirAll(startPath, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v, want nil", err)
		}

		got, err := ResolveWorkspaceRoot(startPath, "")
		if err != nil {
			t.Fatalf("ResolveWorkspaceRoot() error = %v, want nil", err)
		}

		want := mustEvalSymlinks(t, startPath)
		if got != want {
			t.Fatalf("ResolveWorkspaceRoot() = %q, want %q", got, want)
		}
	})

	t.Run("file input resolves to parent directory", func(t *testing.T) {
		repoRoot := filepath.Join(t.TempDir(), "repo")
		filePath := filepath.Join(repoRoot, "pkg", "main.go")
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v, want nil", err)
		}
		if err := os.WriteFile(filePath, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v, want nil", err)
		}
		if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
			t.Fatalf("Mkdir(.git) error = %v, want nil", err)
		}

		got, err := ResolveWorkspaceRoot(filePath, "")
		if err != nil {
			t.Fatalf("ResolveWorkspaceRoot() error = %v, want nil", err)
		}

		want := mustEvalSymlinks(t, repoRoot)
		if got != want {
			t.Fatalf("ResolveWorkspaceRoot() = %q, want %q", got, want)
		}
	})
}

func TestResolveHomeDir(t *testing.T) {
	t.Run("uses ESHU_HOME override", func(t *testing.T) {
		want := filepath.Join(t.TempDir(), "eshu-home")
		got, err := ResolveHomeDir(func(key string) string {
			if key == "ESHU_HOME" {
				return want
			}
			return ""
		}, os.UserHomeDir, runtime.GOOS)
		if err != nil {
			t.Fatalf("ResolveHomeDir() error = %v, want nil", err)
		}
		if got != want {
			t.Fatalf("ResolveHomeDir() = %q, want %q", got, want)
		}
	})

	t.Run("uses platform default when ESHU_HOME is unset", func(t *testing.T) {
		homeDir := filepath.Join(t.TempDir(), "home")
		got, err := ResolveHomeDir(func(string) string { return "" }, func() (string, error) {
			return homeDir, nil
		}, "linux")
		if err != nil {
			t.Fatalf("ResolveHomeDir() error = %v, want nil", err)
		}

		want := filepath.Join(homeDir, ".local", "share", "eshu")
		if got != want {
			t.Fatalf("ResolveHomeDir() = %q, want %q", got, want)
		}
	})
}

func TestBuildLayoutUsesStableWorkspaceIDForSymlinks(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "workspace")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRoot) error = %v, want nil", err)
	}

	symlinkRoot := filepath.Join(base, "workspace-link")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatalf("Symlink() error = %v, want nil", err)
	}

	layoutA, err := BuildLayout(func(string) string { return filepath.Join(base, "eshu-home") }, os.UserHomeDir, runtime.GOOS, realRoot)
	if err != nil {
		t.Fatalf("BuildLayout(realRoot) error = %v, want nil", err)
	}
	layoutB, err := BuildLayout(func(string) string { return filepath.Join(base, "eshu-home") }, os.UserHomeDir, runtime.GOOS, symlinkRoot)
	if err != nil {
		t.Fatalf("BuildLayout(symlinkRoot) error = %v, want nil", err)
	}

	if layoutA.WorkspaceID != layoutB.WorkspaceID {
		t.Fatalf("workspace IDs differ: %q != %q", layoutA.WorkspaceID, layoutB.WorkspaceID)
	}
	if layoutA.RootDir != layoutB.RootDir {
		t.Fatalf("root dirs differ: %q != %q", layoutA.RootDir, layoutB.RootDir)
	}
	if len(layoutA.WorkspaceID) != 40 {
		t.Fatalf("workspace ID length = %d, want 40 hex chars", len(layoutA.WorkspaceID))
	}
	if layoutA.GraphDir != filepath.Join(layoutA.RootDir, "graph") {
		t.Fatalf("GraphDir = %q, want %q", layoutA.GraphDir, filepath.Join(layoutA.RootDir, "graph"))
	}
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v, want nil", path, err)
	}
	return resolved
}
