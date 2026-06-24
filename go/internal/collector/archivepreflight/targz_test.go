// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package archivepreflight

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPreflightSafeTarGzipMetadata(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		sourceName string
	}{
		{name: "tar_gz", sourceName: "bundle.tar.gz"},
		{name: "tgz", sourceName: "bundle.tgz"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := safeTarGzip(t, []tarTestEntry{
				{name: "docs", typeflag: tar.TypeDir},
				{name: "docs/guide.md", body: "metadata only\n", typeflag: tar.TypeReg},
			})
			result, err := Preflight(context.Background(), tc.sourceName, bytes.NewReader(body), int64(len(body)), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			if result.Format != "tar.gz" {
				t.Fatalf("Format = %q, want tar.gz", result.Format)
			}
			if result.EntryCount != 2 {
				t.Fatalf("EntryCount = %d, want 2", result.EntryCount)
			}
			if result.RegularFileCount != 1 {
				t.Fatalf("RegularFileCount = %d, want 1", result.RegularFileCount)
			}
			if result.DirectoryCount != 1 {
				t.Fatalf("DirectoryCount = %d, want 1", result.DirectoryCount)
			}
			if result.SourceBytes != int64(len(body)) {
				t.Fatalf("SourceBytes = %d, want %d", result.SourceBytes, len(body))
			}
			if result.ExpandedBytes == 0 {
				t.Fatal("ExpandedBytes = 0, want tar member size counted")
			}
			assertNoWarning(t, result)
		})
	}
}

func TestPreflightClassifiesMalformedTarGzip(t *testing.T) {
	t.Parallel()

	t.Run("invalid_gzip", func(t *testing.T) {
		t.Parallel()

		body := []byte("not gzip")
		result, err := Preflight(context.Background(), "bundle.tar.gz", bytes.NewReader(body), int64(len(body)), Options{})
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningMalformedContainer)
	})

	t.Run("invalid_tar_inside_gzip", func(t *testing.T) {
		t.Parallel()

		body := gzipBytes(t, []byte("not tar"))
		result, err := Preflight(context.Background(), "bundle.tgz", bytes.NewReader(body), int64(len(body)), Options{})
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningMalformedContainer)
	})
}

func TestPreflightTarGzipResourceAndWarningClasses(t *testing.T) {
	t.Parallel()

	t.Run("source_limit", func(t *testing.T) {
		t.Parallel()

		body := safeTarGzip(t, []tarTestEntry{{name: "docs/guide.md", body: "doc", typeflag: tar.TypeReg}})
		result, err := Preflight(
			context.Background(),
			"bundle.tar.gz",
			bytes.NewReader(body),
			int64(len(body)),
			Options{MaxSourceBytes: int64(len(body) - 1)},
		)
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningResourceLimitExceeded)
	})

	t.Run("expanded_limit", func(t *testing.T) {
		t.Parallel()

		body := safeTarGzip(t, []tarTestEntry{
			{name: "docs/large.md", body: strings.Repeat("x", 32), typeflag: tar.TypeReg},
			{name: "docs/after-limit.md", body: "must not scan", typeflag: tar.TypeReg},
		})
		result, err := Preflight(
			context.Background(),
			"bundle.tar.gz",
			bytes.NewReader(body),
			int64(len(body)),
			Options{MaxExpandedBytes: 8},
		)
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningResourceLimitExceeded)
		if result.EntryCount != 1 {
			t.Fatalf("EntryCount = %d, want scan to stop at first over-limit tar header", result.EntryCount)
		}
	})

	t.Run("compression_ratio_limit", func(t *testing.T) {
		t.Parallel()

		body := safeTarGzip(t, []tarTestEntry{{
			name:     "docs/repeated.md",
			body:     strings.Repeat("a", 4096),
			typeflag: tar.TypeReg,
		}})
		result, err := Preflight(
			context.Background(),
			"bundle.tgz",
			bytes.NewReader(body),
			int64(len(body)),
			Options{MaxCompressionRatio: 1.1},
		)
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningCompressionRatioExceeded)
	})

	t.Run("entry_limit", func(t *testing.T) {
		t.Parallel()

		body := safeTarGzip(t, []tarTestEntry{
			{name: "docs/one.md", body: "one", typeflag: tar.TypeReg},
			{name: "docs/two.md", body: "two", typeflag: tar.TypeReg},
			{name: "docs/three.md", body: "three", typeflag: tar.TypeReg},
		})
		result, err := Preflight(
			context.Background(),
			"bundle.tar.gz",
			bytes.NewReader(body),
			int64(len(body)),
			Options{MaxEntries: 1},
		)
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningResourceLimitExceeded)
		if result.EntryCount != 2 {
			t.Fatalf("EntryCount = %d, want scan to stop at first over-limit entry", result.EntryCount)
		}
	})

	t.Run("unsafe_and_skipped_members", func(t *testing.T) {
		t.Parallel()

		body := safeTarGzip(t, []tarTestEntry{
			{name: "../escape.md", body: "skip", typeflag: tar.TypeReg},
			{name: "docs/link.md", typeflag: tar.TypeSymlink, linkname: "guide.md"},
			{name: "docs/device", typeflag: tar.TypeChar},
			{name: "docs/nested.zip", body: "skip", typeflag: tar.TypeReg},
			{name: "docs/credentials/readme.md", body: "skip", typeflag: tar.TypeReg},
		})
		result, err := Preflight(context.Background(), "bundle.tgz", bytes.NewReader(body), int64(len(body)), Options{})
		if err != nil {
			t.Fatalf("Preflight() error = %v, want nil", err)
		}
		assertWarning(t, result, WarningArchivePathEscape)
		assertWarning(t, result, WarningArchiveSymlinkSkipped)
		assertWarning(t, result, WarningArchiveSpecialFileSkipped)
		assertWarning(t, result, WarningArchiveNestedSkipped)
		assertWarning(t, result, WarningCredentialFileSkipped)
		if result.NestedCount != 1 {
			t.Fatalf("NestedCount = %d, want 1", result.NestedCount)
		}
		if result.CredentialCount != 1 {
			t.Fatalf("CredentialCount = %d, want 1", result.CredentialCount)
		}
	})
}

func TestPreflightTarGzipCanceledContextAsTimeout(t *testing.T) {
	t.Parallel()

	body := safeTarGzip(t, []tarTestEntry{{name: "docs/guide.md", body: "doc", typeflag: tar.TypeReg}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Preflight(ctx, "bundle.tar.gz", bytes.NewReader(body), int64(len(body)), Options{})
	if err == nil {
		t.Fatal("Preflight() error = nil, want canceled context error")
	}
	assertWarning(t, result, WarningTimeout)
}

func TestPreflightTarGzipResultJSONOmitsSourceAndMemberNames(t *testing.T) {
	t.Parallel()

	body := safeTarGzip(t, []tarTestEntry{{name: "docs/member-name-must-not-leak.md", body: "doc", typeflag: tar.TypeReg}})
	result, err := Preflight(context.Background(), "private-source-name.tar.gz", bytes.NewReader(body), int64(len(body)), Options{})
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

func safeTarGzip(t *testing.T, entries []tarTestEntry) []byte {
	t.Helper()

	return gzipBytes(t, safeTar(t, entries))
}

func gzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(body); err != nil {
		t.Fatalf("gzip Write() error = %v, want nil", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v, want nil", err)
	}
	return buffer.Bytes()
}
