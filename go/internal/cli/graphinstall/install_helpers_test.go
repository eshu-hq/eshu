// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// execNornicDBVersion is the test double for VersionReader: it really
// invokes `<binary> version` and parses the "NornicDB <version>" output, the
// same way go/cmd/eshu's readLocalGraphVersion (local_graph_process.go) does
// for the production CLI. It is duplicated here rather than imported because
// readLocalGraphVersion lives in package main and cannot be imported; see
// doc.go for why that stays out of scope for this package.
func execNornicDBVersion(binaryPath string) (string, error) {
	output, err := exec.Command(binaryPath, "version").Output() // #nosec G204 -- test-only invocation of a fixture binary written by writeFakeNornicDBBinary(At)
	if err != nil {
		return "", fmt.Errorf("read nornicdb version: %w", err)
	}
	version := strings.TrimSpace(string(output))
	const prefix = "NornicDB "
	if !strings.HasPrefix(version, prefix) {
		return "", fmt.Errorf("unexpected output %q", version)
	}
	version = strings.TrimSpace(strings.TrimPrefix(version, prefix))
	if version == "" {
		return "", fmt.Errorf("missing version in output %q", output)
	}
	return version, nil
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
