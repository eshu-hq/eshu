// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package archivepreflight

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
)

func TestPreflightSafeZipAndTarMetadata(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		sourceName string
		body       []byte
		wantFormat string
	}{
		{
			name:       "zip",
			sourceName: "bundle.zip",
			body: safeZip(t, []zipTestEntry{
				{name: "docs/", body: "", mode: fs.ModeDir | 0o755},
				{name: "docs/guide.md", body: "metadata only\n", mode: 0o644},
			}),
			wantFormat: "zip",
		},
		{
			name:       "tar",
			sourceName: "bundle.tar",
			body: safeTar(t, []tarTestEntry{
				{name: "docs", typeflag: tar.TypeDir},
				{name: "docs/guide.md", body: "metadata only\n", typeflag: tar.TypeReg},
			}),
			wantFormat: "tar",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tc.sourceName, bytes.NewReader(tc.body), int64(len(tc.body)), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			if result.Format != tc.wantFormat {
				t.Fatalf("Format = %q, want %q", result.Format, tc.wantFormat)
			}
			if result.EntryCount != 2 {
				t.Fatalf("EntryCount = %d, want 2", result.EntryCount)
			}
			if result.RegularFileCount != 1 {
				t.Fatalf("RegularFileCount = %d, want 1", result.RegularFileCount)
			}
			if result.ExpandedBytes == 0 {
				t.Fatal("ExpandedBytes = 0, want safe member size counted")
			}
			assertNoWarning(t, result)
		})
	}
}

func TestPreflightClassifiesUnsupportedAndMalformedContainers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		sourceName string
		body       []byte
		wantClass  WarningClass
	}{
		{name: "unsupported", sourceName: "bundle.rar", body: []byte("not inspected"), wantClass: WarningUnsupportedFormat},
		{name: "malformed_zip", sourceName: "bundle.zip", body: []byte("not a zip"), wantClass: WarningMalformedContainer},
		{name: "malformed_tar", sourceName: "bundle.tar", body: []byte("not a tar"), wantClass: WarningMalformedContainer},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tc.sourceName, bytes.NewReader(tc.body), int64(len(tc.body)), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			assertWarning(t, result, tc.wantClass)
		})
	}
}

func TestPreflightClassifiesResourceLimits(t *testing.T) {
	t.Parallel()

	sourceLimited := safeZip(t, []zipTestEntry{{name: "docs/guide.md", body: "doc", mode: 0o644}})
	entryLimited := safeZip(t, []zipTestEntry{
		{name: "docs/one.md", body: "one", mode: 0o644},
		{name: "docs/two.md", body: "two", mode: 0o644},
	})
	expandedLimited := safeZip(t, []zipTestEntry{{name: "docs/large.md", body: strings.Repeat("x", 32), mode: 0o644}})

	for _, tc := range []struct {
		name    string
		body    []byte
		options Options
	}{
		{
			name:    "source_bytes",
			body:    sourceLimited,
			options: Options{MaxSourceBytes: int64(len(sourceLimited) - 1)},
		},
		{
			name:    "entries",
			body:    entryLimited,
			options: Options{MaxEntries: 1},
		},
		{
			name:    "expanded_bytes",
			body:    expandedLimited,
			options: Options{MaxExpandedBytes: 8},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), "bundle.zip", bytes.NewReader(tc.body), int64(len(tc.body)), tc.options)
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			assertWarning(t, result, WarningResourceLimitExceeded)
		})
	}
}

func TestPreflightClassifiesCompressionRatioLimit(t *testing.T) {
	t.Parallel()

	body := safeZip(t, []zipTestEntry{{
		name: "docs/repeated.md",
		body: strings.Repeat("a", 4096),
		mode: 0o644,
	}})
	result, err := Preflight(
		context.Background(),
		"bundle.zip",
		bytes.NewReader(body),
		int64(len(body)),
		Options{MaxCompressionRatio: 1.1},
	)
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningCompressionRatioExceeded)
}

func TestPreflightClassifiesUnsafeMemberMetadata(t *testing.T) {
	t.Parallel()

	t.Run("zip_path_escape", func(t *testing.T) {
		t.Parallel()

		body := safeZip(t, []zipTestEntry{{name: "../escape.md", body: "skip", mode: 0o644}})
		result, err := Preflight(context.Background(), "bundle.zip", bytes.NewReader(body), int64(len(body)), Options{})
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningArchivePathEscape)
	})

	t.Run("zip_symlink_and_special_files", func(t *testing.T) {
		t.Parallel()

		body := safeZip(t, []zipTestEntry{
			{name: "docs/link.md", body: "guide.md", mode: fs.ModeSymlink | 0o777},
			{name: "docs/device", body: "", mode: fs.ModeDevice | fs.ModeCharDevice | 0o600},
		})
		result, err := Preflight(context.Background(), "bundle.zip", bytes.NewReader(body), int64(len(body)), Options{})
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningArchiveSymlinkSkipped)
		assertWarning(t, result, WarningArchiveSpecialFileSkipped)
	})

	t.Run("tar_member_classes", func(t *testing.T) {
		t.Parallel()

		body := safeTar(t, []tarTestEntry{
			{name: "../escape.md", body: "skip", typeflag: tar.TypeReg},
			{name: "docs/link.md", typeflag: tar.TypeSymlink, linkname: "guide.md"},
			{name: "docs/device", typeflag: tar.TypeChar},
			{name: "docs/nested.zip", body: "skip", typeflag: tar.TypeReg},
			{name: "docs/credentials/readme.md", body: "skip", typeflag: tar.TypeReg},
		})
		result, err := Preflight(context.Background(), "bundle.tar", bytes.NewReader(body), int64(len(body)), Options{})
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningArchivePathEscape)
		assertWarning(t, result, WarningArchiveSymlinkSkipped)
		assertWarning(t, result, WarningArchiveSpecialFileSkipped)
		assertWarning(t, result, WarningArchiveNestedSkipped)
		assertWarning(t, result, WarningCredentialFileSkipped)
	})
}

func TestPreflightClassifiesCanceledContextAsTimeout(t *testing.T) {
	t.Parallel()

	body := safeZip(t, []zipTestEntry{{name: "docs/guide.md", body: "doc", mode: 0o644}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Preflight(ctx, "bundle.zip", bytes.NewReader(body), int64(len(body)), Options{})
	if err == nil {
		t.Fatal("Preflight() error = nil, want canceled context error")
	}
	assertWarning(t, result, WarningTimeout)
}

func TestPreflightResultJSONOmitsSourceAndMemberNames(t *testing.T) {
	t.Parallel()

	body := safeZip(t, []zipTestEntry{{name: "docs/member-name-must-not-leak.md", body: "doc", mode: 0o644}})
	result, err := Preflight(context.Background(), "private-source-name.zip", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v, want nil", err)
	}
	jsonText := string(encoded)
	for _, disallowed := range []string{"member-name-must-not-leak", "private-source-name", "guide.md"} {
		if strings.Contains(jsonText, disallowed) {
			t.Fatalf("result JSON leaked %q: %s", disallowed, jsonText)
		}
	}
}

type zipTestEntry struct {
	name string
	body string
	mode fs.FileMode
}

func safeZip(t *testing.T, entries []zipTestEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		fileWriter, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("CreateHeader(%q) error = %v, want nil", entry.name, err)
		}
		if _, err := fileWriter.Write([]byte(entry.body)); err != nil {
			t.Fatalf("Write(%q) error = %v, want nil", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

type tarTestEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
}

func safeTar(t *testing.T, entries []tarTestEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Typeflag: entry.typeflag,
			Size:     int64(len(entry.body)),
			Mode:     0o644,
			Linkname: entry.linkname,
		}
		if entry.typeflag == tar.TypeDir {
			header.Mode = 0o755
		}
		if entry.typeflag == tar.TypeSymlink || entry.typeflag == tar.TypeChar {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v, want nil", entry.name, err)
		}
		if entry.body != "" {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatalf("Write(%q) error = %v, want nil", entry.name, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func assertWarning(t *testing.T, result Result, class WarningClass) {
	t.Helper()

	for _, warning := range result.Warnings {
		if warning.Class == class && warning.Count > 0 {
			return
		}
	}
	t.Fatalf("missing warning %q in %#v", class, result.Warnings)
}

func assertNoWarning(t *testing.T, result Result) {
	t.Helper()

	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none", result.Warnings)
	}
}
