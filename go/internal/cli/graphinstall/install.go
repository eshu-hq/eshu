// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	managedBinaryName = "nornicdb-headless"
)

var (
	resolveHomeDir = func() (string, error) {
		return eshulocal.ResolveHomeDir(os.Getenv, os.UserHomeDir, runtime.GOOS)
	}
	installNow = time.Now
)

// VersionReader reports the version string a NornicDB binary prints in
// response to `<binary> version`. graphinstall never executes a binary
// itself: running a subprocess is owned by the local_graph process-supervision
// cluster in go/cmd/eshu (readLocalGraphVersion in local_graph_process.go),
// which is out of scope for this extraction (package-restructure.md calls it
// out as a real bidirectional cycle that moves as one unit or not at all).
// Callers supply their real implementation through Options.ReadVersion and
// ManagedBinaryIfPresent so this package can verify an install without a
// process-execution dependency of its own.
type VersionReader func(binaryPath string) (string, error)

// Options are the resolved inputs to Install. From selects the source: a
// local binary/archive/package path, a download URL, or empty to resolve the
// pinned release manifest for the running Eshu version. ReadVersion is
// required; Install returns an error immediately if it is nil.
type Options struct {
	Context     context.Context
	From        string
	SHA256      string
	Force       bool
	Full        bool
	ReadVersion VersionReader
}

// Result reports what Install did: whether a binary was installed or an
// existing one reused, and the resolved backend/version/checksum/manifest
// metadata. JSON tags are a stable wire contract -- `eshu install nornicdb`
// prints Result as-is, so field names must not change independently of that
// CLI output.
type Result struct {
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

// installManifest is the on-disk shape persisted at
// ~/.eshu/graph-backends/nornicdb/manifest.json after a successful install.
type installManifest struct {
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

// Install verifies opts.From (or the pinned release manifest when From is
// empty) and copies the resulting NornicDB binary into Eshu's managed home,
// writing an install manifest alongside it. It reuses the existing managed
// binary in place -- without rewriting it -- when the requested source
// already resolves to the same version and checksum, unless opts.Force is
// set.
func Install(opts Options) (Result, error) {
	if opts.ReadVersion == nil {
		return Result{}, fmt.Errorf("graphinstall: Options.ReadVersion is required")
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	sourceRef := strings.TrimSpace(opts.From)
	if sourceRef == "" {
		resolvedSource, resolvedSHA, err := resolvePinnedReleaseSource(!opts.Full)
		if err != nil {
			return Result{}, err
		}
		sourceRef = resolvedSource
		if strings.TrimSpace(opts.SHA256) == "" {
			opts.SHA256 = resolvedSHA
		}
	}

	source, err := prepareInstallSource(opts.Context, sourceRef, opts.ReadVersion)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_ = source.Close()
	}()

	if expected := strings.ToLower(strings.TrimSpace(opts.SHA256)); expected != "" && expected != source.SourceSHA256 {
		return Result{}, fmt.Errorf("sha256 mismatch for %q: got %s, want %s", source.SourcePath, source.SourceSHA256, expected)
	}

	targetPath, err := managedBinaryPath()
	if err != nil {
		return Result{}, err
	}
	manifestPath, err := installManifestPath()
	if err != nil {
		return Result{}, err
	}

	if source.SourceKind == sourceLocalBinary && samePath(source.LocalBinaryPath, targetPath) {
		return writeInstallManifest(targetPath, manifestPath, source, true)
	}
	if _, err := os.Stat(targetPath); err == nil && !opts.Force {
		existingVersion, versionErr := opts.ReadVersion(targetPath)
		existingSHA, checksumErr := sha256File(targetPath)
		if versionErr == nil && checksumErr == nil && existingVersion == source.Version && existingSHA == source.BinarySHA256 {
			return writeInstallManifest(targetPath, manifestPath, source, true)
		}
		return Result{}, fmt.Errorf("managed nornicdb binary already exists at %q; pass --force to replace it", targetPath)
	} else if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("stat managed nornicdb binary %q: %w", targetPath, err)
	}

	if err := copyExecutableFile(source.LocalBinaryPath, targetPath); err != nil {
		return Result{}, err
	}
	return writeInstallManifest(targetPath, manifestPath, source, false)
}

func managedBinaryPath() (string, error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, "bin", managedBinaryName), nil
}

func installManifestPath() (string, error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, "graph-backends", "nornicdb", "manifest.json"), nil
}

// ManagedBinaryIfPresent returns the path to the managed NornicDB binary if
// Eshu has one installed and it still passes version verification via
// readVersion. It returns an error satisfying os.IsNotExist when no managed
// binary is installed, matching os.Stat's contract for the same case.
// go/cmd/eshu's local_graph_process.go is the sole external caller, wiring
// readLocalGraphVersion as readVersion when resolving which NornicDB binary
// to run.
func ManagedBinaryIfPresent(readVersion VersionReader) (string, error) {
	if readVersion == nil {
		return "", fmt.Errorf("graphinstall: ManagedBinaryIfPresent requires a non-nil VersionReader")
	}
	path, err := managedBinaryPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err //nolint:wrapcheck // callers (resolveNornicDBBinary) depend on os.IsNotExist(err) matching the raw *PathError; wrapping with %w would break that detection.
	}
	if _, err := readVersion(path); err != nil {
		return "", fmt.Errorf("verify managed nornicdb binary %q: %w", path, err)
	}
	return path, nil
}

func writeInstallManifest(targetPath, manifestPath string, source preparedInstallSource, reused bool) (Result, error) {
	installedAt := installNow().UTC().Format(time.RFC3339Nano)
	manifest := installManifest{
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
		return Result{}, fmt.Errorf("encode nornicdb install manifest: %w", err)
	}
	content = append(content, '\n')
	if err := atomicWriteFile(manifestPath, content, 0o600); err != nil {
		return Result{}, err
	}
	return Result{
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
	file, err := os.Open(path) // #nosec G703 G304 -- path is the program-managed nornicdb install source path, not user input
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
	source, err := os.Open(sourcePath) // #nosec G304 -- sourcePath is the program-managed nornicdb install source path, not user input
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
