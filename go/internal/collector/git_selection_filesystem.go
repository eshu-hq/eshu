package collector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func syncFilesystemRepositories(
	ctx context.Context,
	config RepoSyncConfig,
	repositoryIDs []string,
) ([]string, error) {
	if strings.TrimSpace(config.FilesystemRoot) == "" {
		return nil, fmt.Errorf("filesystem source mode requires ESHU_FILESYSTEM_ROOT")
	}
	currentManifest, err := fingerprintTree(config.FilesystemRoot)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(config.ReposDir, ".eshu-fixture-manifest")
	previousManifest, err := os.ReadFile(manifestPath)
	if err == nil && strings.TrimSpace(string(previousManifest)) == currentManifest {
		return nil, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read filesystem manifest: %w", err)
	}

	if err := os.MkdirAll(config.ReposDir, 0o755); err != nil {
		return nil, fmt.Errorf("create repos dir %q: %w", config.ReposDir, err)
	}
	if config.FilesystemDirect {
		selectedPaths := make([]string, 0, len(repositoryIDs))
		for _, repoID := range repositoryIDs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			sourcePath, _, err := filesystemRepoPaths(config, repoID)
			if err != nil {
				return nil, err
			}
			selectedPaths = append(selectedPaths, sourcePath)
		}
		if err := os.WriteFile(manifestPath, []byte(currentManifest), 0o644); err != nil {
			return nil, fmt.Errorf("write filesystem manifest: %w", err)
		}
		return selectedPaths, nil
	}
	if err := cleanManagedWorkspace(config.ReposDir); err != nil {
		return nil, err
	}

	selectedPaths := make([]string, 0, len(repositoryIDs))
	for _, repoID := range repositoryIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sourcePath, targetPath, err := filesystemRepoPaths(config, repoID)
		if err != nil {
			return nil, err
		}
		if err := copyRepositoryTree(sourcePath, targetPath); err != nil {
			return nil, fmt.Errorf("copy filesystem repository %q: %w", repoID, err)
		}
		selectedPaths = append(selectedPaths, targetPath)
	}

	if err := os.WriteFile(manifestPath, []byte(currentManifest), 0o644); err != nil {
		return nil, fmt.Errorf("write filesystem manifest: %w", err)
	}
	return selectedPaths, nil
}

func filesystemRepoPaths(
	config RepoSyncConfig,
	repoID string,
) (string, string, error) {
	if strings.TrimSpace(repoID) == "." {
		checkoutName := filepath.Base(filepath.Clean(config.FilesystemRoot))
		if strings.TrimSpace(checkoutName) == "" || checkoutName == "." || checkoutName == string(filepath.Separator) {
			return "", "", fmt.Errorf("invalid filesystem root %q", config.FilesystemRoot)
		}
		return config.FilesystemRoot, filepath.Join(config.ReposDir, checkoutName), nil
	}

	checkoutName, err := repoCheckoutName(repoID)
	if err != nil {
		return "", "", err
	}
	sourcePath := filepath.Join(config.FilesystemRoot, filepath.FromSlash(repoID))
	targetPath := filepath.Join(config.ReposDir, filepath.FromSlash(checkoutName))
	return sourcePath, targetPath, nil
}

func fingerprintTree(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve fingerprint root %q: %w", root, err)
	}

	files := make([]string, 0)
	ignoreCaches := newCollectorIgnoreCaches()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip entries we cannot read (permission denied, etc.)
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		name := entry.Name()

		// Ignore-control files affect the next collection shape and must
		// participate in the manifest even though normal hidden files do not.
		if isCollectorIgnoreControlFile(name) {
			if !entry.IsDir() {
				files = append(files, path)
			}
			return nil
		}
		if shouldSkipFilesystemEntry(root, path, rel, name, entry.IsDir(), ignoreCaches) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	}); err != nil {
		return "", fmt.Errorf("walk fingerprint root %q: %w", root, err)
	}
	sort.Strings(files)

	digest := sha256.New()
	for _, filePath := range files {
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			continue
		}
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		_, _ = digest.Write([]byte(filepath.ToSlash(rel)))
		_, _ = fmt.Fprintf(digest, "%d:%d", info.ModTime().UnixNano(), info.Size())
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func cleanManagedWorkspace(reposDir string) error {
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read managed workspace %q: %w", reposDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".eshu-") || name == ".eshuignore" {
			continue
		}
		target := filepath.Join(reposDir, name)
		if entry.IsDir() {
			if err := os.RemoveAll(target); err != nil {
				return fmt.Errorf("remove managed directory %q: %w", target, err)
			}
			continue
		}
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("remove managed file %q: %w", target, err)
		}
	}
	return nil
}

func copyRepositoryTree(sourceRoot string, targetRoot string) error {
	sourceRoot, err := filepath.Abs(sourceRoot)
	if err != nil {
		return fmt.Errorf("resolve source repo %q: %w", sourceRoot, err)
	}
	info, err := os.Stat(sourceRoot)
	if err != nil {
		return fmt.Errorf("stat source repo %q: %w", sourceRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source repo %q is not a directory", sourceRoot)
	}

	if err := os.RemoveAll(targetRoot); err != nil {
		return fmt.Errorf("reset target repo %q: %w", targetRoot, err)
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return fmt.Errorf("create target repo %q: %w", targetRoot, err)
	}

	ignoreCaches := newCollectorIgnoreCaches()
	return filepath.WalkDir(sourceRoot, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip entries we cannot read (permission denied, etc.)
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if current == sourceRoot {
			return nil
		}
		rel, err := filepath.Rel(sourceRoot, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		name := entry.Name()

		if shouldSkipFilesystemEntry(sourceRoot, current, rel, name, entry.IsDir(), ignoreCaches) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip symlinks — they cannot be reliably copied into the
		// managed workspace (a symlink-to-directory looks like a file
		// to WalkDir but cannot be read with io.Copy).
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}

		targetPath := filepath.Join(targetRoot, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		if err := copyRepositoryFile(current, targetPath); err != nil {
			// Skip files we cannot read (permission denied, etc.)
			return nil
		}
		return nil
	})
}

func copyRepositoryFile(sourcePath string, targetPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	targetFile, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = targetFile.Close()
	}()
	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}
	return targetFile.Chmod(0o644)
}

func shouldSkipFilesystemEntry(
	repoRoot string,
	fullPath string,
	rel string,
	name string,
	isDir bool,
	caches collectorIgnoreCaches,
) bool {
	if name == ".DS_Store" {
		return true
	}
	if strings.HasPrefix(name, ".") && !preserveFilesystemHiddenPath(rel) {
		return true
	}
	if isCollectorGitignoredInRepo(repoRoot, fullPath, caches.gitignore) ||
		isCollectorEshuignoredInRepo(repoRoot, fullPath, caches.eshuignore) {
		return true
	}
	if isDir {
		probePath := filepath.Join(fullPath, "__eshu_dir_probe__")
		if isCollectorGitignoredInRepo(repoRoot, probePath, caches.gitignore) ||
			isCollectorEshuignoredInRepo(repoRoot, probePath, caches.eshuignore) {
			return true
		}
	}
	return rel == "."
}

func isCollectorIgnoreControlFile(name string) bool {
	return name == ".gitignore" || name == ".eshuignore"
}

func preserveFilesystemHiddenPath(rel string) bool {
	normalized := path.Clean(filepath.ToSlash(rel))
	if normalized == "." {
		return false
	}

	return normalized == ".github" ||
		normalized == ".github/workflows" ||
		strings.HasPrefix(normalized, ".github/workflows/")
}

type collectorIgnoreCaches struct {
	gitignore  map[string]*collectorGitignoreSpec
	eshuignore map[string]*collectorGitignoreSpec
}

func newCollectorIgnoreCaches() collectorIgnoreCaches {
	return collectorIgnoreCaches{
		gitignore:  make(map[string]*collectorGitignoreSpec),
		eshuignore: make(map[string]*collectorGitignoreSpec),
	}
}
