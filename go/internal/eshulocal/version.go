package eshulocal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrIncompatibleLayoutVersion indicates the local data-root version is unsupported.
var ErrIncompatibleLayoutVersion = errors.New("incompatible local data-root version")

// EnsureLayoutVersion creates the layout directories and validates the VERSION file.
func EnsureLayoutVersion(layout Layout, currentVersion string) error {
	if strings.TrimSpace(currentVersion) == "" {
		return fmt.Errorf("current layout version is required")
	}

	nonEmptyRoot, err := dirHasEntriesExcept(layout.RootDir, filepath.Base(layout.OwnerLockPath))
	if err != nil {
		return err
	}

	for _, dir := range []string{layout.RootDir, layout.PostgresDir, layout.LogsDir, layout.CacheDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create local layout directory %q: %w", dir, err)
		}
	}

	version, err := ReadLayoutVersion(layout.VersionPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if nonEmptyRoot {
				return fmt.Errorf("%w: missing VERSION file in non-empty data root %q", ErrIncompatibleLayoutVersion, layout.RootDir)
			}
			return WriteLayoutVersion(layout.VersionPath, currentVersion)
		}
		return err
	}

	if version != currentVersion {
		return fmt.Errorf("%w: data_root=%q version=%q current=%q", ErrIncompatibleLayoutVersion, layout.RootDir, version, currentVersion)
	}
	return nil
}

// ReadLayoutVersion loads and normalizes the VERSION file contents.
func ReadLayoutVersion(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

// WriteLayoutVersion atomically persists the VERSION file.
func WriteLayoutVersion(path, version string) error {
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("layout version is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create version directory: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create version temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.WriteString(strings.TrimSpace(version) + "\n"); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write version temp file: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod version temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync version temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close version temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace version file: %w", err)
	}
	return nil
}

func dirHasEntriesExcept(path string, ignoredNames ...string) (bool, error) {
	dir, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open directory %q: %w", path, err)
	}
	defer func() {
		_ = dir.Close()
	}()

	ignored := make(map[string]struct{}, len(ignoredNames))
	for _, name := range ignoredNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ignored[name] = struct{}{}
	}

	for {
		names, err := dir.Readdirnames(1)
		if err == nil {
			if _, skip := ignored[names[0]]; skip {
				continue
			}
			return true, nil
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, fmt.Errorf("read directory %q: %w", path, err)
	}
}
