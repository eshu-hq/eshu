// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type installSourceKind string

const (
	sourceLocalBinary       installSourceKind = "local-binary"
	sourceLocalArchive      installSourceKind = "local-archive"
	sourceLocalPackage      installSourceKind = "local-package"
	sourceDownloadedBinary  installSourceKind = "downloaded-binary"
	sourceDownloadedArchive installSourceKind = "downloaded-archive"
	sourceDownloadedPackage installSourceKind = "downloaded-package"
	installTimeoutEnv                         = "ESHU_NORNICDB_INSTALL_TIMEOUT"
)

var expandPackage = func(pkgPath, targetDir string) error {
	cmd := exec.Command("pkgutil", "--expand-full", pkgPath, targetDir) // #nosec G204 -- fixed binary "pkgutil"; pkgPath and targetDir are program-managed temp paths constructed by this function
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("expand package %q: %w: %s", pkgPath, err, strings.TrimSpace(string(output)))
	}
	return nil
}

type preparedInstallSource struct {
	SourcePath      string
	SourceKind      installSourceKind
	SourceSHA256    string
	LocalBinaryPath string
	BinarySHA256    string
	Version         string
	Headless        bool
	cleanup         func() error
}

func (s preparedInstallSource) Close() error {
	if s.cleanup == nil {
		return nil
	}
	return s.cleanup()
}

func prepareInstallSource(ctx context.Context, sourceRef string, readVersion VersionReader) (preparedInstallSource, error) {
	sourceRef = strings.TrimSpace(sourceRef)
	localPath, kind, cleanup, err := materializeInstallSource(ctx, sourceRef)
	if err != nil {
		return preparedInstallSource{}, err
	}

	sourceSHA, err := sha256File(localPath)
	if err != nil {
		_ = cleanup()
		return preparedInstallSource{}, err
	}

	prepared, err := inspectInstallSource(sourceRef, localPath, kind, readVersion)
	if err != nil {
		_ = cleanup()
		return preparedInstallSource{}, err
	}
	prepared.SourceSHA256 = sourceSHA
	prepared.cleanup = combineInstallCleanups(prepared.cleanup, cleanup)
	return prepared, nil
}

// combineInstallCleanups preserves independent source and extraction cleanup
// callbacks so archive/package installs do not leak temporary trees.
func combineInstallCleanups(cleanups ...func() error) func() error {
	return func() error {
		var errs []error
		for _, cleanup := range cleanups {
			if cleanup == nil {
				continue
			}
			if err := cleanup(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}

func materializeInstallSource(ctx context.Context, sourceRef string) (string, installSourceKind, func() error, error) {
	parsed, err := url.Parse(sourceRef)
	if err == nil && parsed.Scheme != "" && parsed.Scheme != "file" {
		path, err := downloadInstallSource(ctx, sourceRef)
		if err != nil {
			return "", "", nil, err
		}
		kind := sourceDownloadedBinary
		if looksLikeArchive(sourceRef) {
			kind = sourceDownloadedArchive
		} else if looksLikePackage(sourceRef) {
			kind = sourceDownloadedPackage
		}
		return path, kind, func() error { return os.RemoveAll(filepath.Dir(path)) }, nil
	}

	if err == nil && parsed.Scheme == "file" {
		sourceRef = parsed.Path
	}

	path, err := filepath.Abs(sourceRef)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve nornicdb source path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", nil, fmt.Errorf("stat nornicdb source %q: %w", path, err)
	}
	if info.IsDir() {
		return "", "", nil, fmt.Errorf("nornicdb source path %q is a directory; pass the binary or tarball path", path)
	}
	kind := sourceLocalBinary
	if looksLikeArchive(path) {
		kind = sourceLocalArchive
	} else if looksLikePackage(path) {
		kind = sourceLocalPackage
	}
	return path, kind, func() error { return nil }, nil
}

func inspectInstallSource(sourceRef, localPath string, kind installSourceKind, readVersion VersionReader) (preparedInstallSource, error) {
	switch kind {
	case sourceLocalArchive, sourceDownloadedArchive:
		extractedBinary, extractedName, cleanup, err := extractBinaryFromArchive(localPath)
		if err != nil {
			return preparedInstallSource{}, err
		}
		version, err := readVersion(extractedBinary)
		if err != nil {
			_ = cleanup()
			return preparedInstallSource{}, fmt.Errorf("verify nornicdb source binary %q: %w", sourceRef, err)
		}
		binarySHA, err := sha256File(extractedBinary)
		if err != nil {
			_ = cleanup()
			return preparedInstallSource{}, err
		}
		return preparedInstallSource{
			SourcePath:      sourceRef,
			SourceKind:      kind,
			LocalBinaryPath: extractedBinary,
			BinarySHA256:    binarySHA,
			Version:         version,
			Headless:        filepath.Base(extractedName) == managedBinaryName,
			cleanup:         cleanup,
		}, nil
	case sourceLocalPackage, sourceDownloadedPackage:
		extractedBinary, extractedName, cleanup, err := extractBinaryFromPackage(localPath)
		if err != nil {
			return preparedInstallSource{}, err
		}
		version, err := readVersion(extractedBinary)
		if err != nil {
			_ = cleanup()
			return preparedInstallSource{}, fmt.Errorf("verify nornicdb source binary %q: %w", sourceRef, err)
		}
		binarySHA, err := sha256File(extractedBinary)
		if err != nil {
			_ = cleanup()
			return preparedInstallSource{}, err
		}
		return preparedInstallSource{
			SourcePath:      sourceRef,
			SourceKind:      kind,
			LocalBinaryPath: extractedBinary,
			BinarySHA256:    binarySHA,
			Version:         version,
			Headless:        filepath.Base(extractedName) == managedBinaryName,
			cleanup:         cleanup,
		}, nil
	default:
		version, err := readVersion(localPath)
		if err != nil {
			return preparedInstallSource{}, fmt.Errorf("verify nornicdb source binary %q: %w", sourceRef, err)
		}
		binarySHA, err := sha256File(localPath)
		if err != nil {
			return preparedInstallSource{}, err
		}
		return preparedInstallSource{
			SourcePath:      localPath,
			SourceKind:      kind,
			LocalBinaryPath: localPath,
			BinarySHA256:    binarySHA,
			Version:         version,
			Headless:        filepath.Base(localPath) == managedBinaryName,
		}, nil
	}
}

func downloadInstallSource(ctx context.Context, sourceURL string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout, err := installDownloadTimeout()
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", fmt.Errorf("build nornicdb download request: %w", err)
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download nornicdb source %q: %w", sourceURL, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download nornicdb source %q: unexpected status %s", sourceURL, response.Status)
	}

	tempDir, err := os.MkdirTemp("", "eshu-nornicdb-install-*")
	if err != nil {
		return "", fmt.Errorf("create nornicdb source temp directory: %w", err)
	}
	name := filepath.Base(request.URL.Path)
	if strings.TrimSpace(name) == "" || name == "." || name == "/" {
		name = "nornicdb-source"
	}
	targetPath := filepath.Join(tempDir, name)
	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- targetPath is filepath.Join of an os.MkdirTemp dir and a filepath.Base-cleaned URL filename; contained within the temp dir
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("create downloaded nornicdb source file: %w", err)
	}
	if _, err := io.Copy(file, response.Body); err != nil {
		_ = file.Close()
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("download nornicdb source body: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("close downloaded nornicdb source file: %w", err)
	}
	return targetPath, nil
}

func installDownloadTimeout() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(installTimeoutEnv))
	if raw == "" {
		return 30 * time.Second, nil
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", installTimeoutEnv, raw, err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be greater than zero", installTimeoutEnv, raw)
	}
	return timeout, nil
}

func extractBinaryFromArchive(archivePath string) (string, string, func() error, error) {
	file, err := os.Open(archivePath) // #nosec G304 -- archivePath is a program-managed temp file path created by downloadInstallSource, not user-supplied input
	if err != nil {
		return "", "", nil, fmt.Errorf("open nornicdb archive %q: %w", archivePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	var tarReader *tar.Reader
	switch {
	case strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz"), strings.HasSuffix(strings.ToLower(archivePath), ".tgz"):
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return "", "", nil, fmt.Errorf("open gzip nornicdb archive %q: %w", archivePath, err)
		}
		defer func() {
			_ = gzipReader.Close()
		}()
		tarReader = tar.NewReader(gzipReader)
	case strings.HasSuffix(strings.ToLower(archivePath), ".tar"):
		tarReader = tar.NewReader(file)
	default:
		return "", "", nil, fmt.Errorf("nornicdb source %q is not a supported archive; use .tar, .tar.gz, or .tgz", archivePath)
	}

	tempDir, err := os.MkdirTemp("", "eshu-nornicdb-archive-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create archive extraction directory: %w", err)
	}

	var extractedPath string
	var extractedName string
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return "", "", nil, fmt.Errorf("read nornicdb archive %q: %w", archivePath, err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Base(header.Name)
		if name != managedBinaryName && name != "nornicdb" {
			continue
		}
		targetPath := filepath.Join(tempDir, name)
		targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o750) // #nosec G302 G304 -- 0o750 is intentional: extracted NornicDB binary requires owner execute; targetPath is filepath.Join of os.MkdirTemp dir and filepath.Base(header.Name) validated against a fixed allowlist
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return "", "", nil, fmt.Errorf("create extracted nornicdb binary: %w", err)
		}
		const nornicDBBinaryMaxBytes = 512 << 20 // 512 MiB — guards against decompression bombs
		if _, err := io.Copy(targetFile, io.LimitReader(tarReader, nornicDBBinaryMaxBytes)); err != nil {
			_ = targetFile.Close()
			_ = os.RemoveAll(tempDir)
			return "", "", nil, fmt.Errorf("extract nornicdb binary from archive: %w", err)
		}
		if err := targetFile.Close(); err != nil {
			_ = os.RemoveAll(tempDir)
			return "", "", nil, fmt.Errorf("close extracted nornicdb binary: %w", err)
		}
		extractedPath = targetPath
		extractedName = name
		if name == managedBinaryName {
			break
		}
	}
	if extractedPath == "" {
		_ = os.RemoveAll(tempDir)
		return "", "", nil, fmt.Errorf("nornicdb archive %q did not contain a usable NornicDB binary", archivePath)
	}
	return extractedPath, extractedName, func() error { return os.RemoveAll(tempDir) }, nil
}

func extractBinaryFromPackage(packagePath string) (string, string, func() error, error) {
	if runtime.GOOS != "darwin" {
		return "", "", nil, fmt.Errorf("nornicdb package sources are only supported on darwin today")
	}
	tempDir, err := os.MkdirTemp("", "eshu-nornicdb-package-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create package extraction directory: %w", err)
	}
	expandedDir := filepath.Join(tempDir, "expanded")
	if err := expandPackage(packagePath, expandedDir); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", "", nil, err
	}

	candidates, err := filepath.Glob(filepath.Join(expandedDir, "*", "Payload", "usr", "local", "bin", "*"))
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return "", "", nil, fmt.Errorf("scan expanded nornicdb package %q: %w", packagePath, err)
	}

	var chosen string
	for _, candidate := range candidates {
		name := filepath.Base(candidate)
		if name == managedBinaryName {
			chosen = candidate
			break
		}
		if name == "nornicdb" && chosen == "" {
			chosen = candidate
		}
	}
	if chosen == "" {
		_ = os.RemoveAll(tempDir)
		return "", "", nil, fmt.Errorf("nornicdb package %q did not contain a usable NornicDB binary", packagePath)
	}
	return chosen, filepath.Base(chosen), func() error { return os.RemoveAll(tempDir) }, nil
}

func looksLikeArchive(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar")
}

func looksLikePackage(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".pkg")
}
