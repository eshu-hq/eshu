// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallNornicDBExtractsHeadlessBinaryFromTarGz(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)

	sourceBinary := filepath.Join(t.TempDir(), "nornicdb-headless")
	writeFakeNornicDBBinaryAt(t, sourceBinary, "NornicDB v1.0.42\n")
	archivePath := filepath.Join(t.TempDir(), "nornicdb-headless-darwin-arm64.tar.gz")
	archiveContent := writeTarGzWithBinary(t, archivePath, "release/bin/nornicdb-headless", sourceBinary)
	wantSourceSHA := sha256BytesHex(archiveContent)

	result, err := Install(Options{
		From:        archivePath,
		SHA256:      wantSourceSHA,
		ReadVersion: execNornicDBVersion,
	})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if result.Version != "v1.0.42" {
		t.Fatalf("Version = %q, want %q", result.Version, "v1.0.42")
	}
	if result.SourceSHA256 != wantSourceSHA {
		t.Fatalf("SourceSHA256 = %q, want %q", result.SourceSHA256, wantSourceSHA)
	}
	if result.SourceKind != string(sourceLocalArchive) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, sourceLocalArchive)
	}
	if !result.Headless {
		t.Fatal("Headless = false, want true")
	}
}

func TestPrepareNornicDBInstallSourceClosesArchiveExtraction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	sourceBinary := filepath.Join(t.TempDir(), "nornicdb-headless")
	writeFakeNornicDBBinaryAt(t, sourceBinary, "NornicDB v1.0.42\n")
	archivePath := filepath.Join(t.TempDir(), "nornicdb-headless-darwin-arm64.tar.gz")
	writeTarGzWithBinary(t, archivePath, "release/bin/nornicdb-headless", sourceBinary)

	prepared, err := prepareInstallSource(context.Background(), archivePath, execNornicDBVersion)
	if err != nil {
		t.Fatalf("prepareInstallSource() error = %v, want nil", err)
	}
	extractionDir := filepath.Dir(prepared.LocalBinaryPath)
	if _, err := os.Stat(extractionDir); err != nil {
		t.Fatalf("os.Stat(extractionDir) error = %v, want extracted directory before Close", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("prepared.Close() error = %v, want nil", err)
	}
	if _, err := os.Stat(extractionDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Stat(extractionDir) error = %v, want removed extraction directory", err)
	}
}

func TestInstallNornicDBDownloadsArchiveFromURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)

	sourceBinary := filepath.Join(t.TempDir(), "nornicdb-headless")
	writeFakeNornicDBBinaryAt(t, sourceBinary, "NornicDB v1.0.42\n")
	archivePath := filepath.Join(t.TempDir(), "nornicdb-headless-darwin-arm64.tar.gz")
	archiveContent := writeTarGzWithBinary(t, archivePath, "release/bin/nornicdb-headless", sourceBinary)
	wantSourceSHA := sha256BytesHex(archiveContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archiveContent)
	}))
	defer server.Close()

	result, err := Install(Options{
		From:        server.URL + "/nornicdb-headless-darwin-arm64.tar.gz",
		SHA256:      wantSourceSHA,
		ReadVersion: execNornicDBVersion,
	})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if result.Version != "v1.0.42" {
		t.Fatalf("Version = %q, want %q", result.Version, "v1.0.42")
	}
	if result.SourceSHA256 != wantSourceSHA {
		t.Fatalf("SourceSHA256 = %q, want %q", result.SourceSHA256, wantSourceSHA)
	}
	if result.SourceKind != string(sourceDownloadedArchive) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, sourceDownloadedArchive)
	}
}

func TestInstallNornicDBMarksFullPackageAsNonHeadless(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("package install path is only supported on darwin")
	}

	originalExpandPackage := expandPackage
	t.Cleanup(func() {
		expandPackage = originalExpandPackage
	})

	t.Setenv("ESHU_HOME", t.TempDir())
	packagePath := filepath.Join(t.TempDir(), "NornicDB-1.0.42-hotfix-arm64-full.pkg")
	if err := os.WriteFile(packagePath, []byte("pkg"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", packagePath, err)
	}

	sourceBinary := filepath.Join(t.TempDir(), "nornicdb")
	writeFakeNornicDBBinaryAt(t, sourceBinary, "NornicDB v1.0.42\n")
	expandPackage = func(pkgPath, targetDir string) error {
		target := filepath.Join(targetDir, "full.pkg", "Payload", "usr", "local", "bin", "nornicdb")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		content, err := os.ReadFile(sourceBinary)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o755)
	}

	result, err := Install(Options{From: packagePath, ReadVersion: execNornicDBVersion})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if result.Headless {
		t.Fatal("Headless = true, want false for full package install")
	}
	if result.SourceKind != string(sourceLocalPackage) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, sourceLocalPackage)
	}
}

func TestPrepareNornicDBInstallSourceDownloadHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := prepareInstallSource(ctx, server.URL+"/nornicdb-headless-darwin-arm64.tar.gz", execNornicDBVersion)
	if err == nil {
		t.Fatal("prepareInstallSource() error = nil, want context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("prepareInstallSource() error = %v, want context.Canceled", err)
	}
}

func TestNornicDBInstallDownloadTimeout(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		got, err := installDownloadTimeout()
		if err != nil {
			t.Fatalf("installDownloadTimeout() error = %v, want nil", err)
		}
		if got != 30*time.Second {
			t.Fatalf("installDownloadTimeout() = %s, want %s", got, 30*time.Second)
		}
	})

	t.Run("override", func(t *testing.T) {
		t.Setenv(installTimeoutEnv, "2m15s")

		got, err := installDownloadTimeout()
		if err != nil {
			t.Fatalf("installDownloadTimeout() error = %v, want nil", err)
		}
		if got != 2*time.Minute+15*time.Second {
			t.Fatalf("installDownloadTimeout() = %s, want %s", got, 2*time.Minute+15*time.Second)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		t.Setenv(installTimeoutEnv, "not-a-duration")

		_, err := installDownloadTimeout()
		if err == nil {
			t.Fatal("installDownloadTimeout() error = nil, want parse error")
		}
		if !strings.Contains(err.Error(), installTimeoutEnv) {
			t.Fatalf("installDownloadTimeout() error = %q, want env guidance", err.Error())
		}
	})
}

func TestInstallNornicDBRejectsArchiveWithoutNornicDBBinary(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)

	archivePath := filepath.Join(t.TempDir(), "nornicdb-headless-darwin-arm64.tar.gz")
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte("hello\n")
	header := &tar.Header{
		Name: "release/README.txt",
		Mode: 0o644,
		Size: int64(len(content)),
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("tarWriter.WriteHeader() error = %v, want nil", err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatalf("tarWriter.Write() error = %v, want nil", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tarWriter.Close() error = %v, want nil", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzipWriter.Close() error = %v, want nil", err)
	}
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", archivePath, err)
	}

	_, err := Install(Options{From: archivePath, ReadVersion: execNornicDBVersion})
	if err == nil {
		t.Fatal("Install() error = nil, want archive extraction error")
	}
	if !strings.Contains(err.Error(), "did not contain a usable NornicDB binary") {
		t.Fatalf("Install() error = %q, want missing binary guidance", err.Error())
	}
}
