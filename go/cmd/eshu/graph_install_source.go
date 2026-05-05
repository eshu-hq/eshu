package main

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

type nornicDBInstallSourceKind string

const (
	nornicDBInstallSourceLocalBinary       nornicDBInstallSourceKind = "local-binary"
	nornicDBInstallSourceLocalArchive      nornicDBInstallSourceKind = "local-archive"
	nornicDBInstallSourceLocalPackage      nornicDBInstallSourceKind = "local-package"
	nornicDBInstallSourceDownloadedBinary  nornicDBInstallSourceKind = "downloaded-binary"
	nornicDBInstallSourceDownloadedArchive nornicDBInstallSourceKind = "downloaded-archive"
	nornicDBInstallSourceDownloadedPackage nornicDBInstallSourceKind = "downloaded-package"
	nornicDBInstallTimeoutEnv                                        = "ESHU_NORNICDB_INSTALL_TIMEOUT"
)

var graphInstallExpandPackage = func(pkgPath, targetDir string) error {
	cmd := exec.Command("pkgutil", "--expand-full", pkgPath, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("expand package %q: %w: %s", pkgPath, err, strings.TrimSpace(string(output)))
	}
	return nil
}

type preparedNornicDBInstallSource struct {
	SourcePath      string
	SourceKind      nornicDBInstallSourceKind
	SourceSHA256    string
	LocalBinaryPath string
	BinarySHA256    string
	Version         string
	Headless        bool
	cleanup         func() error
}

func (s preparedNornicDBInstallSource) Close() error {
	if s.cleanup == nil {
		return nil
	}
	return s.cleanup()
}

func prepareNornicDBInstallSource(ctx context.Context, sourceRef string) (preparedNornicDBInstallSource, error) {
	sourceRef = strings.TrimSpace(sourceRef)
	localPath, kind, cleanup, err := materializeNornicDBInstallSource(ctx, sourceRef)
	if err != nil {
		return preparedNornicDBInstallSource{}, err
	}

	sourceSHA, err := sha256File(localPath)
	if err != nil {
		_ = cleanup()
		return preparedNornicDBInstallSource{}, err
	}

	prepared, err := inspectNornicDBInstallSource(sourceRef, localPath, kind)
	if err != nil {
		_ = cleanup()
		return preparedNornicDBInstallSource{}, err
	}
	prepared.SourceSHA256 = sourceSHA
	prepared.cleanup = combineNornicDBInstallCleanups(prepared.cleanup, cleanup)
	return prepared, nil
}

// combineNornicDBInstallCleanups preserves independent source and extraction
// cleanup callbacks so archive/package installs do not leak temporary trees.
func combineNornicDBInstallCleanups(cleanups ...func() error) func() error {
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

func materializeNornicDBInstallSource(ctx context.Context, sourceRef string) (string, nornicDBInstallSourceKind, func() error, error) {
	parsed, err := url.Parse(sourceRef)
	if err == nil && parsed.Scheme != "" && parsed.Scheme != "file" {
		path, err := downloadNornicDBInstallSource(ctx, sourceRef)
		if err != nil {
			return "", "", nil, err
		}
		kind := nornicDBInstallSourceDownloadedBinary
		if looksLikeNornicDBArchive(sourceRef) {
			kind = nornicDBInstallSourceDownloadedArchive
		} else if looksLikeNornicDBPackage(sourceRef) {
			kind = nornicDBInstallSourceDownloadedPackage
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
	kind := nornicDBInstallSourceLocalBinary
	if looksLikeNornicDBArchive(path) {
		kind = nornicDBInstallSourceLocalArchive
	} else if looksLikeNornicDBPackage(path) {
		kind = nornicDBInstallSourceLocalPackage
	}
	return path, kind, func() error { return nil }, nil
}

func inspectNornicDBInstallSource(sourceRef, localPath string, kind nornicDBInstallSourceKind) (preparedNornicDBInstallSource, error) {
	switch kind {
	case nornicDBInstallSourceLocalArchive, nornicDBInstallSourceDownloadedArchive:
		extractedBinary, extractedName, cleanup, err := extractNornicDBBinaryFromArchive(localPath)
		if err != nil {
			return preparedNornicDBInstallSource{}, err
		}
		version, err := localGraphReadVersion(extractedBinary)
		if err != nil {
			_ = cleanup()
			return preparedNornicDBInstallSource{}, fmt.Errorf("verify nornicdb source binary %q: %w", sourceRef, err)
		}
		binarySHA, err := sha256File(extractedBinary)
		if err != nil {
			_ = cleanup()
			return preparedNornicDBInstallSource{}, err
		}
		return preparedNornicDBInstallSource{
			SourcePath:      sourceRef,
			SourceKind:      kind,
			LocalBinaryPath: extractedBinary,
			BinarySHA256:    binarySHA,
			Version:         version,
			Headless:        filepath.Base(extractedName) == managedNornicDBBinaryName,
			cleanup:         cleanup,
		}, nil
	case nornicDBInstallSourceLocalPackage, nornicDBInstallSourceDownloadedPackage:
		extractedBinary, extractedName, cleanup, err := extractNornicDBBinaryFromPackage(localPath)
		if err != nil {
			return preparedNornicDBInstallSource{}, err
		}
		version, err := localGraphReadVersion(extractedBinary)
		if err != nil {
			_ = cleanup()
			return preparedNornicDBInstallSource{}, fmt.Errorf("verify nornicdb source binary %q: %w", sourceRef, err)
		}
		binarySHA, err := sha256File(extractedBinary)
		if err != nil {
			_ = cleanup()
			return preparedNornicDBInstallSource{}, err
		}
		return preparedNornicDBInstallSource{
			SourcePath:      sourceRef,
			SourceKind:      kind,
			LocalBinaryPath: extractedBinary,
			BinarySHA256:    binarySHA,
			Version:         version,
			Headless:        filepath.Base(extractedName) == managedNornicDBBinaryName,
			cleanup:         cleanup,
		}, nil
	default:
		version, err := localGraphReadVersion(localPath)
		if err != nil {
			return preparedNornicDBInstallSource{}, fmt.Errorf("verify nornicdb source binary %q: %w", sourceRef, err)
		}
		binarySHA, err := sha256File(localPath)
		if err != nil {
			return preparedNornicDBInstallSource{}, err
		}
		return preparedNornicDBInstallSource{
			SourcePath:      localPath,
			SourceKind:      kind,
			LocalBinaryPath: localPath,
			BinarySHA256:    binarySHA,
			Version:         version,
			Headless:        filepath.Base(localPath) == managedNornicDBBinaryName,
		}, nil
	}
}

func downloadNornicDBInstallSource(ctx context.Context, sourceURL string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout, err := nornicDBInstallDownloadTimeout()
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
	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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

func nornicDBInstallDownloadTimeout() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(nornicDBInstallTimeoutEnv))
	if raw == "" {
		return 30 * time.Second, nil
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", nornicDBInstallTimeoutEnv, raw, err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be greater than zero", nornicDBInstallTimeoutEnv, raw)
	}
	return timeout, nil
}

func extractNornicDBBinaryFromArchive(archivePath string) (string, string, func() error, error) {
	file, err := os.Open(archivePath)
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
		if name != managedNornicDBBinaryName && name != "nornicdb" {
			continue
		}
		targetPath := filepath.Join(tempDir, name)
		targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return "", "", nil, fmt.Errorf("create extracted nornicdb binary: %w", err)
		}
		if _, err := io.Copy(targetFile, tarReader); err != nil {
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
		if name == managedNornicDBBinaryName {
			break
		}
	}
	if extractedPath == "" {
		_ = os.RemoveAll(tempDir)
		return "", "", nil, fmt.Errorf("nornicdb archive %q did not contain a usable NornicDB binary", archivePath)
	}
	return extractedPath, extractedName, func() error { return os.RemoveAll(tempDir) }, nil
}

func extractNornicDBBinaryFromPackage(packagePath string) (string, string, func() error, error) {
	if runtime.GOOS != "darwin" {
		return "", "", nil, fmt.Errorf("nornicdb package sources are only supported on darwin today")
	}
	tempDir, err := os.MkdirTemp("", "eshu-nornicdb-package-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create package extraction directory: %w", err)
	}
	expandedDir := filepath.Join(tempDir, "expanded")
	if err := graphInstallExpandPackage(packagePath, expandedDir); err != nil {
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
		if name == managedNornicDBBinaryName {
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

func looksLikeNornicDBArchive(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar")
}

func looksLikeNornicDBPackage(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".pkg")
}
