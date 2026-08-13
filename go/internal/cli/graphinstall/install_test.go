// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallNornicDBCopiesVerifiedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)
	source := writeFakeNornicDBBinary(t, "NornicDB v1.0.42\n")
	wantSHA := fileSHA256Hex(t, source)

	result, err := Install(Options{
		From:        source,
		SHA256:      wantSHA,
		ReadVersion: execNornicDBVersion,
	})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if result.Version != "v1.0.42" {
		t.Fatalf("Version = %q, want %q", result.Version, "v1.0.42")
	}
	if result.SHA256 != wantSHA {
		t.Fatalf("SHA256 = %q, want %q", result.SHA256, wantSHA)
	}
	wantBinary := filepath.Join(homeDir, "bin", "nornicdb-headless")
	if result.BinaryPath != wantBinary {
		t.Fatalf("BinaryPath = %q, want %q", result.BinaryPath, wantBinary)
	}
	info, err := os.Stat(wantBinary)
	if err != nil {
		t.Fatalf("os.Stat(installed binary) error = %v, want nil", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("installed binary mode = %v, want 0755", info.Mode().Perm())
	}

	manifestPath := filepath.Join(homeDir, "graph-backends", "nornicdb", "manifest.json")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("os.ReadFile(manifest) error = %v, want nil", err)
	}
	var manifest installManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("json.Unmarshal(manifest) error = %v, want nil", err)
	}
	if manifest.Version != "v1.0.42" || manifest.SHA256 != wantSHA || manifest.BinaryPath != wantBinary {
		t.Fatalf("manifest = %+v, want installed version/checksum/path", manifest)
	}
}

func TestInstallNornicDBRejectsChecksumMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	t.Setenv("ESHU_HOME", t.TempDir())
	source := writeFakeNornicDBBinary(t, "NornicDB v1.0.42\n")

	_, err := Install(Options{
		From:        source,
		SHA256:      strings.Repeat("0", 64),
		ReadVersion: execNornicDBVersion,
	})
	if err == nil {
		t.Fatal("Install() error = nil, want checksum error")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("Install() error = %q, want sha256 mismatch", err.Error())
	}
}

func TestInstallNornicDBRequiresForceToReplaceDifferentBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)
	first := writeFakeNornicDBBinary(t, "NornicDB v1.0.42\n")
	second := writeFakeNornicDBBinary(t, "NornicDB v1.0.43\n")

	if _, err := Install(Options{From: first, ReadVersion: execNornicDBVersion}); err != nil {
		t.Fatalf("first Install() error = %v, want nil", err)
	}
	_, err := Install(Options{From: second, ReadVersion: execNornicDBVersion})
	if err == nil {
		t.Fatal("second Install() error = nil, want replace guidance")
	}
	if !strings.Contains(err.Error(), "pass --force to replace it") {
		t.Fatalf("second Install() error = %q, want --force guidance", err.Error())
	}
}

func TestInstallNornicDBForceReplacesDifferentBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)
	first := writeFakeNornicDBBinary(t, "NornicDB v1.0.42\n")
	second := writeFakeNornicDBBinary(t, "NornicDB v1.0.43\n")

	if _, err := Install(Options{From: first, ReadVersion: execNornicDBVersion}); err != nil {
		t.Fatalf("first Install() error = %v, want nil", err)
	}
	result, err := Install(Options{From: second, Force: true, ReadVersion: execNornicDBVersion})
	if err != nil {
		t.Fatalf("forced Install() error = %v, want nil", err)
	}
	if result.Reused {
		t.Fatal("Reused = true after forced replacement, want false")
	}
	gotVersion, err := execNornicDBVersion(filepath.Join(homeDir, "bin", "nornicdb-headless"))
	if err != nil {
		t.Fatalf("execNornicDBVersion(installed) error = %v, want nil", err)
	}
	if gotVersion != "v1.0.43" {
		t.Fatalf("installed version = %q, want %q", gotVersion, "v1.0.43")
	}
}

func TestInstallNornicDBReusesManagedSourcePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)
	source := writeFakeNornicDBBinary(t, "NornicDB v1.0.42\n")

	first, err := Install(Options{From: source, ReadVersion: execNornicDBVersion})
	if err != nil {
		t.Fatalf("first Install() error = %v, want nil", err)
	}
	second, err := Install(Options{From: first.BinaryPath, ReadVersion: execNornicDBVersion})
	if err != nil {
		t.Fatalf("second Install() error = %v, want nil", err)
	}
	if !second.Reused {
		t.Fatal("Reused = false for source-equals-target install, want true")
	}
	if second.BinaryPath != first.BinaryPath {
		t.Fatalf("second BinaryPath = %q, want %q", second.BinaryPath, first.BinaryPath)
	}
}

func TestInstallNornicDBRequiresLocalSourcePath(t *testing.T) {
	originalManifest := pinnedReleaseManifestData
	originalHostOS := installHostOS
	originalHostArch := installHostArch
	t.Cleanup(func() {
		pinnedReleaseManifestData = originalManifest
		installHostOS = originalHostOS
		installHostArch = originalHostArch
	})

	t.Setenv("ESHU_HOME", t.TempDir())
	pinnedReleaseManifestData = []byte(`{"version":1,"backend":"nornicdb","releases":[]}`)
	installHostOS = "linux"
	installHostArch = "amd64"

	_, err := Install(Options{ReadVersion: execNornicDBVersion})
	if err == nil {
		t.Fatal("Install() error = nil, want missing source error")
	}
	if !strings.Contains(err.Error(), "no embedded headless NornicDB release asset") ||
		!strings.Contains(err.Error(), "latest NornicDB main branch") {
		t.Fatalf("Install() error = %q, want latest-main explicit-source guidance", err.Error())
	}
}

func TestInstallNornicDBUsesPinnedReleaseManifestWhenFromEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	originalManifest := pinnedReleaseManifestData
	originalHostOS := installHostOS
	originalHostArch := installHostArch
	t.Cleanup(func() {
		pinnedReleaseManifestData = originalManifest
		installHostOS = originalHostOS
		installHostArch = originalHostArch
	})

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)

	sourceBinary := filepath.Join(t.TempDir(), "nornicdb-headless")
	writeFakeNornicDBBinaryAt(t, sourceBinary, "NornicDB v1.0.42\n")
	archivePath := filepath.Join(t.TempDir(), "nornicdb-headless-darwin-arm64.tar.gz")
	archiveContent := writeTarGzWithBinary(t, archivePath, "release/bin/nornicdb-headless", sourceBinary)
	archiveSHA := sha256BytesHex(archiveContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archiveContent)
	}))
	defer server.Close()

	installHostOS = "darwin"
	installHostArch = "arm64"
	pinnedReleaseManifestData = []byte(fmt.Sprintf(`{
  "version": 1,
  "backend": "nornicdb",
  "releases": [
    {
      "eshu_version": "dev",
      "release_tag": "v1.0.42-hotfix",
      "assets": [
        {
          "os": "darwin",
          "arch": "arm64",
          "format": "tar.gz",
          "headless": true,
          "url": %q,
          "sha256": %q
        }
      ]
    }
  ]
}`, server.URL+"/nornicdb-headless-darwin-arm64.tar.gz", archiveSHA))

	result, err := Install(Options{ReadVersion: execNornicDBVersion})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if result.SourceKind != string(sourceDownloadedArchive) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, sourceDownloadedArchive)
	}
	if result.SourceSHA256 != archiveSHA {
		t.Fatalf("SourceSHA256 = %q, want %q", result.SourceSHA256, archiveSHA)
	}
	if result.Version != "v1.0.42" {
		t.Fatalf("Version = %q, want %q", result.Version, "v1.0.42")
	}
}

func TestInstallNornicDBUsesPinnedFullReleaseWhenRequested(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	originalManifest := pinnedReleaseManifestData
	originalHostOS := installHostOS
	originalHostArch := installHostArch
	t.Cleanup(func() {
		pinnedReleaseManifestData = originalManifest
		installHostOS = originalHostOS
		installHostArch = originalHostArch
	})

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)

	sourceBinary := filepath.Join(t.TempDir(), "nornicdb")
	writeFakeNornicDBBinaryAt(t, sourceBinary, "NornicDB v1.0.42\n")
	archivePath := filepath.Join(t.TempDir(), "nornicdb-darwin-arm64.tar.gz")
	archiveContent := writeTarGzWithBinary(t, archivePath, "release/bin/nornicdb", sourceBinary)
	archiveSHA := sha256BytesHex(archiveContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archiveContent)
	}))
	defer server.Close()

	installHostOS = "darwin"
	installHostArch = "arm64"
	pinnedReleaseManifestData = []byte(fmt.Sprintf(`{
  "version": 1,
  "backend": "nornicdb",
  "releases": [
    {
      "eshu_version": "dev",
      "release_tag": "v1.0.42-hotfix",
      "assets": [
        {
          "os": "darwin",
          "arch": "arm64",
          "format": "pkg",
          "headless": true,
          "url": "https://example.com/NornicDB-1.0.42-hotfix-arm64-lite.pkg",
          "sha256": "deadbeef"
        },
        {
          "os": "darwin",
          "arch": "arm64",
          "format": "tar.gz",
          "headless": false,
          "url": %q,
          "sha256": %q
        }
      ]
    }
  ]
}`, server.URL+"/nornicdb-darwin-arm64.tar.gz", archiveSHA))

	result, err := Install(Options{Full: true, ReadVersion: execNornicDBVersion})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if result.Headless {
		t.Fatal("Headless = true, want false for full release install")
	}
	if result.SourceKind != string(sourceDownloadedArchive) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, sourceDownloadedArchive)
	}
	if result.SourceSHA256 != archiveSHA {
		t.Fatalf("SourceSHA256 = %q, want %q", result.SourceSHA256, archiveSHA)
	}
}

func TestInstallNornicDBWithoutSourceRejectsMissingPinnedFullAsset(t *testing.T) {
	originalManifest := pinnedReleaseManifestData
	originalHostOS := installHostOS
	originalHostArch := installHostArch
	t.Cleanup(func() {
		pinnedReleaseManifestData = originalManifest
		installHostOS = originalHostOS
		installHostArch = originalHostArch
	})

	t.Setenv("ESHU_HOME", t.TempDir())
	installHostOS = "darwin"
	installHostArch = "arm64"
	pinnedReleaseManifestData = []byte(`{
  "version": 1,
  "backend": "nornicdb",
  "releases": [
    {
      "eshu_version": "dev",
      "release_tag": "v1.0.42-hotfix",
      "assets": [
        {
          "os": "darwin",
          "arch": "arm64",
          "format": "tar.gz",
          "headless": true,
          "url": "https://example.com/nornicdb-headless-darwin-arm64.tar.gz",
          "sha256": "deadbeef"
        }
      ]
    }
  ]
}`)

	_, err := Install(Options{Full: true, ReadVersion: execNornicDBVersion})
	if err == nil {
		t.Fatal("Install() error = nil, want missing full-asset error")
	}
	if !strings.Contains(err.Error(), "no embedded full NornicDB release asset") ||
		!strings.Contains(err.Error(), "<path-to-nornicdb>") ||
		strings.Contains(err.Error(), "<path-to-nornicdb-headless>") {
		t.Fatalf("Install() error = %q, want full-binary guidance", err.Error())
	}
}

func TestInstallNornicDBWithoutSourceRejectsUnsupportedHost(t *testing.T) {
	originalManifest := pinnedReleaseManifestData
	originalHostOS := installHostOS
	originalHostArch := installHostArch
	t.Cleanup(func() {
		pinnedReleaseManifestData = originalManifest
		installHostOS = originalHostOS
		installHostArch = originalHostArch
	})

	t.Setenv("ESHU_HOME", t.TempDir())
	installHostOS = "linux"
	installHostArch = "amd64"
	pinnedReleaseManifestData = []byte(`{
  "version": 1,
  "backend": "nornicdb",
  "releases": [
    {
      "eshu_version": "dev",
      "release_tag": "v1.0.42-hotfix",
      "assets": [
        {
          "os": "darwin",
          "arch": "arm64",
          "format": "pkg",
          "headless": true,
          "url": "https://example.com/NornicDB-1.0.42-hotfix-arm64-lite.pkg",
          "sha256": "deadbeef"
        }
      ]
    }
  ]
}`)

	_, err := Install(Options{ReadVersion: execNornicDBVersion})
	if err == nil {
		t.Fatal("Install() error = nil, want unsupported host error")
	}
	if !strings.Contains(err.Error(), "no embedded headless NornicDB release asset") {
		t.Fatalf("Install() error = %q, want unsupported host guidance", err.Error())
	}
}
