package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestInstallNornicDBCopiesVerifiedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)
	source := writeFakeNornicDBBinary(t, "NornicDB v1.0.42\n")
	wantSHA := fileSHA256Hex(t, source)

	result, err := installNornicDB(installNornicDBOptions{
		From:   source,
		SHA256: wantSHA,
	})
	if err != nil {
		t.Fatalf("installNornicDB() error = %v, want nil", err)
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
	var manifest nornicDBInstallManifest
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

	_, err := installNornicDB(installNornicDBOptions{
		From:   source,
		SHA256: strings.Repeat("0", 64),
	})
	if err == nil {
		t.Fatal("installNornicDB() error = nil, want checksum error")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("installNornicDB() error = %q, want sha256 mismatch", err.Error())
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

	if _, err := installNornicDB(installNornicDBOptions{From: first}); err != nil {
		t.Fatalf("first installNornicDB() error = %v, want nil", err)
	}
	_, err := installNornicDB(installNornicDBOptions{From: second})
	if err == nil {
		t.Fatal("second installNornicDB() error = nil, want replace guidance")
	}
	if !strings.Contains(err.Error(), "pass --force to replace it") {
		t.Fatalf("second installNornicDB() error = %q, want --force guidance", err.Error())
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

	if _, err := installNornicDB(installNornicDBOptions{From: first}); err != nil {
		t.Fatalf("first installNornicDB() error = %v, want nil", err)
	}
	result, err := installNornicDB(installNornicDBOptions{From: second, Force: true})
	if err != nil {
		t.Fatalf("forced installNornicDB() error = %v, want nil", err)
	}
	if result.Reused {
		t.Fatal("Reused = true after forced replacement, want false")
	}
	gotVersion, err := readLocalGraphVersion(filepath.Join(homeDir, "bin", "nornicdb-headless"))
	if err != nil {
		t.Fatalf("readLocalGraphVersion(installed) error = %v, want nil", err)
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

	first, err := installNornicDB(installNornicDBOptions{From: source})
	if err != nil {
		t.Fatalf("first installNornicDB() error = %v, want nil", err)
	}
	second, err := installNornicDB(installNornicDBOptions{From: first.BinaryPath})
	if err != nil {
		t.Fatalf("second installNornicDB() error = %v, want nil", err)
	}
	if !second.Reused {
		t.Fatal("Reused = false for source-equals-target install, want true")
	}
	if second.BinaryPath != first.BinaryPath {
		t.Fatalf("second BinaryPath = %q, want %q", second.BinaryPath, first.BinaryPath)
	}
}

func TestInstallNornicDBRequiresLocalSourcePath(t *testing.T) {
	originalManifest := graphPinnedNornicDBReleaseManifest
	originalHostOS := graphInstallHostOS
	originalHostArch := graphInstallHostArch
	t.Cleanup(func() {
		graphPinnedNornicDBReleaseManifest = originalManifest
		graphInstallHostOS = originalHostOS
		graphInstallHostArch = originalHostArch
	})

	t.Setenv("ESHU_HOME", t.TempDir())
	graphPinnedNornicDBReleaseManifest = []byte(`{"version":1,"backend":"nornicdb","releases":[]}`)
	graphInstallHostOS = "linux"
	graphInstallHostArch = "amd64"

	_, err := installNornicDB(installNornicDBOptions{})
	if err == nil {
		t.Fatal("installNornicDB() error = nil, want missing source error")
	}
	if !strings.Contains(err.Error(), "no embedded headless NornicDB release asset") ||
		!strings.Contains(err.Error(), "latest NornicDB main branch") {
		t.Fatalf("installNornicDB() error = %q, want latest-main explicit-source guidance", err.Error())
	}
}

func TestInstallNornicDBUsesPinnedReleaseManifestWhenFromEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	originalManifest := graphPinnedNornicDBReleaseManifest
	originalHostOS := graphInstallHostOS
	originalHostArch := graphInstallHostArch
	t.Cleanup(func() {
		graphPinnedNornicDBReleaseManifest = originalManifest
		graphInstallHostOS = originalHostOS
		graphInstallHostArch = originalHostArch
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

	graphInstallHostOS = "darwin"
	graphInstallHostArch = "arm64"
	graphPinnedNornicDBReleaseManifest = []byte(fmt.Sprintf(`{
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

	result, err := installNornicDB(installNornicDBOptions{})
	if err != nil {
		t.Fatalf("installNornicDB() error = %v, want nil", err)
	}
	if result.SourceKind != string(nornicDBInstallSourceDownloadedArchive) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, nornicDBInstallSourceDownloadedArchive)
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

	originalManifest := graphPinnedNornicDBReleaseManifest
	originalHostOS := graphInstallHostOS
	originalHostArch := graphInstallHostArch
	t.Cleanup(func() {
		graphPinnedNornicDBReleaseManifest = originalManifest
		graphInstallHostOS = originalHostOS
		graphInstallHostArch = originalHostArch
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

	graphInstallHostOS = "darwin"
	graphInstallHostArch = "arm64"
	graphPinnedNornicDBReleaseManifest = []byte(fmt.Sprintf(`{
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

	result, err := installNornicDB(installNornicDBOptions{Full: true})
	if err != nil {
		t.Fatalf("installNornicDB() error = %v, want nil", err)
	}
	if result.Headless {
		t.Fatal("Headless = true, want false for full release install")
	}
	if result.SourceKind != string(nornicDBInstallSourceDownloadedArchive) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, nornicDBInstallSourceDownloadedArchive)
	}
	if result.SourceSHA256 != archiveSHA {
		t.Fatalf("SourceSHA256 = %q, want %q", result.SourceSHA256, archiveSHA)
	}
}

func TestInstallNornicDBWithoutSourceRejectsMissingPinnedFullAsset(t *testing.T) {
	originalManifest := graphPinnedNornicDBReleaseManifest
	originalHostOS := graphInstallHostOS
	originalHostArch := graphInstallHostArch
	t.Cleanup(func() {
		graphPinnedNornicDBReleaseManifest = originalManifest
		graphInstallHostOS = originalHostOS
		graphInstallHostArch = originalHostArch
	})

	t.Setenv("ESHU_HOME", t.TempDir())
	graphInstallHostOS = "darwin"
	graphInstallHostArch = "arm64"
	graphPinnedNornicDBReleaseManifest = []byte(`{
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

	_, err := installNornicDB(installNornicDBOptions{Full: true})
	if err == nil {
		t.Fatal("installNornicDB() error = nil, want missing full-asset error")
	}
	if !strings.Contains(err.Error(), "no embedded full NornicDB release asset") ||
		!strings.Contains(err.Error(), "<path-to-nornicdb>") ||
		strings.Contains(err.Error(), "<path-to-nornicdb-headless>") {
		t.Fatalf("installNornicDB() error = %q, want full-binary guidance", err.Error())
	}
}

func TestInstallNornicDBWithoutSourceRejectsUnsupportedHost(t *testing.T) {
	originalManifest := graphPinnedNornicDBReleaseManifest
	originalHostOS := graphInstallHostOS
	originalHostArch := graphInstallHostArch
	t.Cleanup(func() {
		graphPinnedNornicDBReleaseManifest = originalManifest
		graphInstallHostOS = originalHostOS
		graphInstallHostArch = originalHostArch
	})

	t.Setenv("ESHU_HOME", t.TempDir())
	graphInstallHostOS = "linux"
	graphInstallHostArch = "amd64"
	graphPinnedNornicDBReleaseManifest = []byte(`{
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

	_, err := installNornicDB(installNornicDBOptions{})
	if err == nil {
		t.Fatal("installNornicDB() error = nil, want unsupported host error")
	}
	if !strings.Contains(err.Error(), "no embedded headless NornicDB release asset") {
		t.Fatalf("installNornicDB() error = %q, want unsupported host guidance", err.Error())
	}
}

func TestRunInstallNornicDBRejectsFullWithExplicitSource(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("from", "/tmp/nornicdb-headless", "")
	cmd.Flags().String("sha256", "", "")
	cmd.Flags().Bool("force", false, "")
	cmd.Flags().Bool("full", true, "")

	err := runInstallNornicDB(cmd, nil)
	if err == nil {
		t.Fatal("runInstallNornicDB() error = nil, want full/source conflict")
	}
	if !strings.Contains(err.Error(), "--full is reserved for future no-argument release installs") {
		t.Fatalf("runInstallNornicDB() error = %q, want actionable conflict", err.Error())
	}
}

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

	result, err := installNornicDB(installNornicDBOptions{
		From:   archivePath,
		SHA256: wantSourceSHA,
	})
	if err != nil {
		t.Fatalf("installNornicDB() error = %v, want nil", err)
	}
	if result.Version != "v1.0.42" {
		t.Fatalf("Version = %q, want %q", result.Version, "v1.0.42")
	}
	if result.SourceSHA256 != wantSourceSHA {
		t.Fatalf("SourceSHA256 = %q, want %q", result.SourceSHA256, wantSourceSHA)
	}
	if result.SourceKind != string(nornicDBInstallSourceLocalArchive) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, nornicDBInstallSourceLocalArchive)
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

	prepared, err := prepareNornicDBInstallSource(context.Background(), archivePath)
	if err != nil {
		t.Fatalf("prepareNornicDBInstallSource() error = %v, want nil", err)
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

	result, err := installNornicDB(installNornicDBOptions{
		From:   server.URL + "/nornicdb-headless-darwin-arm64.tar.gz",
		SHA256: wantSourceSHA,
	})
	if err != nil {
		t.Fatalf("installNornicDB() error = %v, want nil", err)
	}
	if result.Version != "v1.0.42" {
		t.Fatalf("Version = %q, want %q", result.Version, "v1.0.42")
	}
	if result.SourceSHA256 != wantSourceSHA {
		t.Fatalf("SourceSHA256 = %q, want %q", result.SourceSHA256, wantSourceSHA)
	}
	if result.SourceKind != string(nornicDBInstallSourceDownloadedArchive) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, nornicDBInstallSourceDownloadedArchive)
	}
}

func TestInstallNornicDBMarksFullPackageAsNonHeadless(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("package install path is only supported on darwin")
	}

	originalExpandPackage := graphInstallExpandPackage
	t.Cleanup(func() {
		graphInstallExpandPackage = originalExpandPackage
	})

	t.Setenv("ESHU_HOME", t.TempDir())
	packagePath := filepath.Join(t.TempDir(), "NornicDB-1.0.42-hotfix-arm64-full.pkg")
	if err := os.WriteFile(packagePath, []byte("pkg"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", packagePath, err)
	}

	sourceBinary := filepath.Join(t.TempDir(), "nornicdb")
	writeFakeNornicDBBinaryAt(t, sourceBinary, "NornicDB v1.0.42\n")
	graphInstallExpandPackage = func(pkgPath, targetDir string) error {
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

	result, err := installNornicDB(installNornicDBOptions{From: packagePath})
	if err != nil {
		t.Fatalf("installNornicDB() error = %v, want nil", err)
	}
	if result.Headless {
		t.Fatal("Headless = true, want false for full package install")
	}
	if result.SourceKind != string(nornicDBInstallSourceLocalPackage) {
		t.Fatalf("SourceKind = %q, want %q", result.SourceKind, nornicDBInstallSourceLocalPackage)
	}
}

func TestPrepareNornicDBInstallSourceDownloadHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := prepareNornicDBInstallSource(ctx, server.URL+"/nornicdb-headless-darwin-arm64.tar.gz")
	if err == nil {
		t.Fatal("prepareNornicDBInstallSource() error = nil, want context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("prepareNornicDBInstallSource() error = %v, want context.Canceled", err)
	}
}

func TestNornicDBInstallDownloadTimeout(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		got, err := nornicDBInstallDownloadTimeout()
		if err != nil {
			t.Fatalf("nornicDBInstallDownloadTimeout() error = %v, want nil", err)
		}
		if got != 30*time.Second {
			t.Fatalf("nornicDBInstallDownloadTimeout() = %s, want %s", got, 30*time.Second)
		}
	})

	t.Run("override", func(t *testing.T) {
		t.Setenv(nornicDBInstallTimeoutEnv, "2m15s")

		got, err := nornicDBInstallDownloadTimeout()
		if err != nil {
			t.Fatalf("nornicDBInstallDownloadTimeout() error = %v, want nil", err)
		}
		if got != 2*time.Minute+15*time.Second {
			t.Fatalf("nornicDBInstallDownloadTimeout() = %s, want %s", got, 2*time.Minute+15*time.Second)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		t.Setenv(nornicDBInstallTimeoutEnv, "not-a-duration")

		_, err := nornicDBInstallDownloadTimeout()
		if err == nil {
			t.Fatal("nornicDBInstallDownloadTimeout() error = nil, want parse error")
		}
		if !strings.Contains(err.Error(), nornicDBInstallTimeoutEnv) {
			t.Fatalf("nornicDBInstallDownloadTimeout() error = %q, want env guidance", err.Error())
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

	_, err := installNornicDB(installNornicDBOptions{From: archivePath})
	if err == nil {
		t.Fatal("installNornicDB() error = nil, want archive extraction error")
	}
	if !strings.Contains(err.Error(), "did not contain a usable NornicDB binary") {
		t.Fatalf("installNornicDB() error = %q, want missing binary guidance", err.Error())
	}
}

func TestResolveNornicDBBinaryPrefersManagedInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	originalLookPath := localGraphLookPath
	t.Cleanup(func() {
		localGraphLookPath = originalLookPath
	})

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)
	t.Setenv("ESHU_NORNICDB_BINARY", "")
	managedPath := filepath.Join(homeDir, "bin", "nornicdb-headless")
	writeFakeNornicDBBinaryAt(t, managedPath, "NornicDB v1.0.43\n")
	localGraphLookPath = func(file string) (string, error) {
		t.Fatalf("localGraphLookPath(%q) called; managed install should win", file)
		return "", nil
	}

	got, err := resolveNornicDBBinary()
	if err != nil {
		t.Fatalf("resolveNornicDBBinary() error = %v, want nil", err)
	}
	if got != managedPath {
		t.Fatalf("resolveNornicDBBinary() = %q, want %q", got, managedPath)
	}
}

func TestRunInstallNornicDBPrintsJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	t.Setenv("ESHU_HOME", t.TempDir())
	source := writeFakeNornicDBBinary(t, "NornicDB v1.0.42\n")
	cmd := &cobra.Command{}
	cmd.Flags().String("from", source, "")
	cmd.Flags().String("sha256", "", "")
	cmd.Flags().Bool("force", false, "")
	cmd.Flags().Bool("full", false, "")

	output := captureStdout(t, func() {
		if err := runInstallNornicDB(cmd, nil); err != nil {
			t.Fatalf("runInstallNornicDB() error = %v, want nil", err)
		}
	})

	var got installNornicDBResult
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("json.Unmarshal(output) error = %v, output=%q", err, output)
	}
	if got.Version != "v1.0.42" {
		t.Fatalf("Version = %q, want %q", got.Version, "v1.0.42")
	}
	if !got.Installed {
		t.Fatal("Installed = false for first install, want true")
	}
}

func writeFakeNornicDBBinary(t *testing.T, versionOutput string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nornicdb-headless")
	writeFakeNornicDBBinaryAt(t, path, versionOutput)
	return path
}

func writeFakeNornicDBBinaryAt(t *testing.T, path, versionOutput string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then printf '" + versionOutput + "'; exit 0; fi\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile(fake binary) error = %v, want nil", err)
	}
}

func fileSHA256Hex(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func sha256BytesHex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func writeTarGzWithBinary(t *testing.T, archivePath, entryName, sourceBinary string) []byte {
	t.Helper()
	content, err := os.ReadFile(sourceBinary)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", sourceBinary, err)
	}

	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{
		Name: entryName,
		Mode: 0o755,
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
	return archive.Bytes()
}
