package eshulocal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureLayoutVersion(t *testing.T) {
	t.Run("creates fresh layout and version file", func(t *testing.T) {
		layout := testLayout(t)

		if err := EnsureLayoutVersion(layout, "v1"); err != nil {
			t.Fatalf("EnsureLayoutVersion() error = %v, want nil", err)
		}

		for _, dir := range []string{layout.RootDir, layout.PostgresDir, layout.LogsDir, layout.CacheDir} {
			info, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("Stat(%q) error = %v, want nil", dir, err)
			}
			if !info.IsDir() {
				t.Fatalf("Stat(%q).IsDir() = false, want true", dir)
			}
		}

		version, err := ReadLayoutVersion(layout.VersionPath)
		if err != nil {
			t.Fatalf("ReadLayoutVersion() error = %v, want nil", err)
		}
		if version != "v1" {
			t.Fatalf("ReadLayoutVersion() = %q, want %q", version, "v1")
		}
	})

	t.Run("accepts matching existing version", func(t *testing.T) {
		layout := testLayout(t)
		if err := WriteLayoutVersion(layout.VersionPath, "v1"); err != nil {
			t.Fatalf("WriteLayoutVersion() error = %v, want nil", err)
		}

		if err := EnsureLayoutVersion(layout, "v1"); err != nil {
			t.Fatalf("EnsureLayoutVersion() error = %v, want nil", err)
		}
	})

	t.Run("rejects incompatible existing version", func(t *testing.T) {
		layout := testLayout(t)
		if err := WriteLayoutVersion(layout.VersionPath, "v0"); err != nil {
			t.Fatalf("WriteLayoutVersion() error = %v, want nil", err)
		}

		err := EnsureLayoutVersion(layout, "v1")
		if !errors.Is(err, ErrIncompatibleLayoutVersion) {
			t.Fatalf("EnsureLayoutVersion() error = %v, want %v", err, ErrIncompatibleLayoutVersion)
		}
	})

	t.Run("normalizes trailing newline and whitespace", func(t *testing.T) {
		layout := testLayout(t)
		if err := os.MkdirAll(layout.RootDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v, want nil", err)
		}
		if err := os.WriteFile(layout.VersionPath, []byte("v1 \n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v, want nil", err)
		}

		if err := EnsureLayoutVersion(layout, "v1"); err != nil {
			t.Fatalf("EnsureLayoutVersion() error = %v, want nil", err)
		}
	})

	t.Run("rejects empty current version", func(t *testing.T) {
		layout := testLayout(t)

		err := EnsureLayoutVersion(layout, "")
		if err == nil {
			t.Fatal("EnsureLayoutVersion() error = nil, want non-nil")
		}
	})

	t.Run("missing version on non-empty root fails closed", func(t *testing.T) {
		layout := testLayout(t)
		if err := os.MkdirAll(layout.RootDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v, want nil", err)
		}
		if err := os.WriteFile(filepath.Join(layout.RootDir, "existing.txt"), []byte("present"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v, want nil", err)
		}

		err := EnsureLayoutVersion(layout, "v1")
		if !errors.Is(err, ErrIncompatibleLayoutVersion) {
			t.Fatalf("EnsureLayoutVersion() error = %v, want %v", err, ErrIncompatibleLayoutVersion)
		}
	})

	t.Run("owner lock alone does not make first boot incompatible", func(t *testing.T) {
		layout := testLayout(t)
		if err := os.MkdirAll(layout.RootDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v, want nil", err)
		}
		if err := os.WriteFile(layout.OwnerLockPath, []byte(""), 0o600); err != nil {
			t.Fatalf("WriteFile(owner.lock) error = %v, want nil", err)
		}

		if err := EnsureLayoutVersion(layout, "v3"); err != nil {
			t.Fatalf("EnsureLayoutVersion() error = %v, want nil", err)
		}

		got, err := ReadLayoutVersion(layout.VersionPath)
		if err != nil {
			t.Fatalf("ReadLayoutVersion() error = %v, want nil", err)
		}
		if got != "v3" {
			t.Fatalf("VERSION = %q, want %q", got, "v3")
		}
	})
}

func testLayout(t *testing.T) Layout {
	t.Helper()

	root := filepath.Join(t.TempDir(), "workspace-root")
	return Layout{
		RootDir:         root,
		VersionPath:     filepath.Join(root, "VERSION"),
		OwnerLockPath:   filepath.Join(root, "owner.lock"),
		OwnerRecordPath: filepath.Join(root, "owner.json"),
		PostgresDir:     filepath.Join(root, "postgres"),
		LogsDir:         filepath.Join(root, "logs"),
		CacheDir:        filepath.Join(root, "cache"),
	}
}
