package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	managedNornicDBBinaryName = "nornicdb-headless"
)

var (
	graphResolveHomeDir = func() (string, error) {
		return eshulocal.ResolveHomeDir(os.Getenv, os.UserHomeDir, runtime.GOOS)
	}
	graphInstallNow = time.Now
)

type installNornicDBOptions struct {
	Context context.Context
	From    string
	SHA256  string
	Force   bool
	Full    bool
}

type installNornicDBResult struct {
	Installed    bool   `json:"installed"`
	Reused       bool   `json:"reused"`
	Backend      string `json:"backend"`
	BinaryPath   string `json:"binary_path"`
	ManifestPath string `json:"manifest_path"`
	Version      string `json:"version"`
	SHA256       string `json:"sha256"`
	SourcePath   string `json:"source_path,omitempty"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
	SourceKind   string `json:"source_kind,omitempty"`
	Headless     bool   `json:"headless"`
	InstalledAt  string `json:"installed_at"`
}

type nornicDBInstallManifest struct {
	Backend      string `json:"backend"`
	BinaryPath   string `json:"binary_path"`
	Version      string `json:"version"`
	SHA256       string `json:"sha256"`
	SourcePath   string `json:"source_path,omitempty"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
	SourceKind   string `json:"source_kind,omitempty"`
	InstalledAt  string `json:"installed_at"`
	InstallMode  string `json:"install_mode"`
	Headless     bool   `json:"headless"`
}

func runInstallNornicDB(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("eshu install nornicdb accepts flags only, got %d argument(s)", len(args))
	}
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}
	expectedSHA, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return err
	}
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	full, err := cmd.Flags().GetBool("full")
	if err != nil {
		return err
	}
	if full && strings.TrimSpace(from) != "" {
		return errors.New("--full is reserved for future no-argument release installs; install full NornicDB binaries with --from")
	}

	result, err := installNornicDB(installNornicDBOptions{
		Context: cmd.Context(),
		From:    from,
		SHA256:  expectedSHA,
		Force:   force,
		Full:    full,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func installNornicDB(opts installNornicDBOptions) (installNornicDBResult, error) {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	sourceRef := strings.TrimSpace(opts.From)
	if sourceRef == "" {
		resolvedSource, resolvedSHA, err := resolvePinnedNornicDBReleaseSource(!opts.Full)
		if err != nil {
			return installNornicDBResult{}, err
		}
		sourceRef = resolvedSource
		if strings.TrimSpace(opts.SHA256) == "" {
			opts.SHA256 = resolvedSHA
		}
	}

	source, err := prepareNornicDBInstallSource(opts.Context, sourceRef)
	if err != nil {
		return installNornicDBResult{}, err
	}
	defer func() {
		_ = source.Close()
	}()

	if expected := strings.ToLower(strings.TrimSpace(opts.SHA256)); expected != "" && expected != source.SourceSHA256 {
		return installNornicDBResult{}, fmt.Errorf("sha256 mismatch for %q: got %s, want %s", source.SourcePath, source.SourceSHA256, expected)
	}

	targetPath, err := managedNornicDBBinaryPath()
	if err != nil {
		return installNornicDBResult{}, err
	}
	manifestPath, err := nornicDBInstallManifestPath()
	if err != nil {
		return installNornicDBResult{}, err
	}

	if source.SourceKind == nornicDBInstallSourceLocalBinary && samePath(source.LocalBinaryPath, targetPath) {
		return writeNornicDBInstallManifest(targetPath, manifestPath, source, true)
	}
	if _, err := os.Stat(targetPath); err == nil && !opts.Force {
		existingVersion, versionErr := localGraphReadVersion(targetPath)
		existingSHA, checksumErr := sha256File(targetPath)
		if versionErr == nil && checksumErr == nil && existingVersion == source.Version && existingSHA == source.BinarySHA256 {
			return writeNornicDBInstallManifest(targetPath, manifestPath, source, true)
		}
		return installNornicDBResult{}, fmt.Errorf("managed nornicdb binary already exists at %q; pass --force to replace it", targetPath)
	} else if err != nil && !os.IsNotExist(err) {
		return installNornicDBResult{}, fmt.Errorf("stat managed nornicdb binary %q: %w", targetPath, err)
	}

	if err := copyExecutableFile(source.LocalBinaryPath, targetPath); err != nil {
		return installNornicDBResult{}, err
	}
	return writeNornicDBInstallManifest(targetPath, manifestPath, source, false)
}

func managedNornicDBBinaryPath() (string, error) {
	homeDir, err := graphResolveHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, "bin", managedNornicDBBinaryName), nil
}

func nornicDBInstallManifestPath() (string, error) {
	homeDir, err := graphResolveHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, "graph-backends", "nornicdb", "manifest.json"), nil
}

func managedNornicDBBinaryIfPresent() (string, error) {
	path, err := managedNornicDBBinaryPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	if _, err := localGraphReadVersion(path); err != nil {
		return "", fmt.Errorf("verify managed nornicdb binary %q: %w", path, err)
	}
	return path, nil
}

func writeNornicDBInstallManifest(targetPath, manifestPath string, source preparedNornicDBInstallSource, reused bool) (installNornicDBResult, error) {
	installedAt := graphInstallNow().UTC().Format(time.RFC3339Nano)
	manifest := nornicDBInstallManifest{
		Backend:      string(query.GraphBackendNornicDB),
		BinaryPath:   targetPath,
		Version:      source.Version,
		SHA256:       source.BinarySHA256,
		SourcePath:   source.SourcePath,
		SourceSHA256: source.SourceSHA256,
		SourceKind:   string(source.SourceKind),
		InstalledAt:  installedAt,
		InstallMode:  string(source.SourceKind),
		Headless:     source.Headless,
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return installNornicDBResult{}, fmt.Errorf("encode nornicdb install manifest: %w", err)
	}
	content = append(content, '\n')
	if err := atomicWriteFile(manifestPath, content, 0o600); err != nil {
		return installNornicDBResult{}, err
	}
	return installNornicDBResult{
		Installed:    true,
		Reused:       reused,
		Backend:      manifest.Backend,
		BinaryPath:   targetPath,
		ManifestPath: manifestPath,
		Version:      source.Version,
		SHA256:       source.BinarySHA256,
		SourcePath:   source.SourcePath,
		SourceSHA256: source.SourceSHA256,
		SourceKind:   string(source.SourceKind),
		Headless:     source.Headless,
		InstalledAt:  installedAt,
	}, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q for sha256: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyExecutableFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open nornicdb source binary: %w", err)
	}
	defer func() {
		_ = source.Close()
	}()
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return fmt.Errorf("create nornicdb binary directory: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create nornicdb binary temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := io.Copy(tempFile, source); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("copy nornicdb binary: %w", err)
	}
	if err := tempFile.Chmod(0o755); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod nornicdb binary temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync nornicdb binary temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close nornicdb binary temp file: %w", err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("replace managed nornicdb binary: %w", err)
	}
	return nil
}

func atomicWriteFile(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create directory for %q: %w", path, err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %q: %w", path, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp file for %q: %w", path, err)
	}
	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temp file for %q: %w", path, err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temp file for %q: %w", path, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file for %q: %w", path, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return left == right
	}
	leftEval, leftErr := filepath.EvalSymlinks(leftAbs)
	rightEval, rightErr := filepath.EvalSymlinks(rightAbs)
	if leftErr == nil {
		leftAbs = leftEval
	}
	if rightErr == nil {
		rightAbs = rightEval
	}
	return leftAbs == rightAbs
}
