// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsTARArchiveDocumentationPackets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		path       string
		body       func(*testing.T, []archiveTAREntry) []byte
		wantFormat string
	}{
		{name: "tar", path: "docs/support-packet.tar", body: buildTestArchiveTAR, wantFormat: "tar"},
		{name: "tar.gz", path: "docs/support-packet.tar.gz", body: buildTestArchiveTARGZ, wantFormat: "tar.gz"},
		{name: "tgz", path: "docs/support-packet.tgz", body: buildTestArchiveTARGZ, wantFormat: "tar.gz"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoPath := t.TempDir()
			body := tc.body(t, []archiveTAREntry{
				{name: "runbook.md", body: "# Restore Service\n\nFollow the recovery checklist.\n", typeflag: tar.TypeReg, mode: 0o644},
				{name: "tables/service-inventory.csv", body: "service,dependency\npayments-api,postgres\n", typeflag: tar.TypeReg, mode: 0o644},
				{name: "notes/debug.bin", body: "not documentation", typeflag: tar.TypeReg, mode: 0o644},
			})
			writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(tc.path)), body)

			envelopes := streamArchiveFacts(t, repoPath, tc.path, "sha256:archive")

			documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
			if got, want := len(documents), 3; got != want {
				t.Fatalf("documentation_document count = %d, want %d", got, want)
			}
			outer := documentByID(documents, "doc:git:repository:r_12345678:"+tc.path)
			if outer == nil {
				t.Fatalf("missing outer archive document in %#v", documents)
			}
			if got := payloadString(outer.Payload, "format"); got != tc.wantFormat {
				t.Fatalf("outer format = %q, want %q", got, tc.wantFormat)
			}
			if got := payloadSourceMetadataValue(outer.Payload, "archive_format"); got != tc.wantFormat {
				t.Fatalf("archive_format = %q, want %q", got, tc.wantFormat)
			}
			if got := payloadSourceMetadataValue(outer.Payload, "supported_member_count"); got != "2" {
				t.Fatalf("supported_member_count = %q, want 2", got)
			}
			assertPayloadWarning(t, outer.Payload, "unsupported_format")
			assertTarMemberDocument(t, documents, repoPath, tc.path, "runbook.md")
			assertTarMemberDocument(t, documents, repoPath, tc.path, "tables/service-inventory.csv")

			sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
			if got, want := len(sections), 2; got != want {
				t.Fatalf("documentation_section count = %d, want %d", got, want)
			}
			if sectionByHeading(sections, "Restore Service") == nil {
				t.Fatalf("missing Restore Service section in %#v", sections)
			}
			assertSectionContentContains(t, sections, "payments-api")
		})
	}
}

func TestStreamFactsRejectsUnsafeTARArchiveMembers(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/unsafe-packet.tar"
	body := buildTestArchiveTAR(t, []archiveTAREntry{
		{name: "../escape.md", body: "# Escape\n\nmust not emit\n", typeflag: tar.TypeReg, mode: 0o644},
		{name: "docs/good.md", body: "# Good\n\nalso blocked by unsafe package\n", typeflag: tar.TypeReg, mode: 0o644},
	})
	writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(archivePath)), body)

	envelopes := streamArchiveFacts(t, repoPath, archivePath, "sha256:unsafe")

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "archive_path_escape")
	if got := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)); got != 0 {
		t.Fatalf("documentation_section count = %d, want 0", got)
	}
	for _, envelope := range envelopes {
		if strings.Contains(fmt.Sprint(envelope.Payload), "must not emit") {
			t.Fatalf("unsafe archive content leaked into payload: %#v", envelope.Payload)
		}
	}
}

func TestStreamFactsSkipsUnsafeTARMembersWithoutBlockingSafeMembers(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/mixed-packet.tar.gz"
	body := buildTestArchiveTARGZ(t, []archiveTAREntry{
		{name: "docs/readme.md", body: "# Public Notes\n\nSafe summary.\n", typeflag: tar.TypeReg, mode: 0o644},
		{name: "nested.tar", body: "nested bytes", typeflag: tar.TypeReg, mode: 0o644},
		{name: ".env", body: "SECRET_TOKEN=do-not-emit\n", typeflag: tar.TypeReg, mode: 0o644},
		{name: "secret.md", body: "# Secret Packet\n\nmust-not-emit\n", typeflag: tar.TypeReg, mode: 0o644},
		{name: "docs/link.md", body: "docs/readme.md", typeflag: tar.TypeSymlink, mode: 0o777},
		{name: "docs/hardlink.md", linkname: "docs/readme.md", typeflag: tar.TypeLink, mode: 0o777},
		{name: "docs/fifo", typeflag: tar.TypeFifo, mode: fs.ModeNamedPipe | 0o644},
	})
	writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(archivePath)), body)

	envelopes := streamArchiveFacts(t, repoPath, archivePath, "sha256:mixed")

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "archive_nested_skipped")
	assertPayloadWarning(t, documents[0].Payload, "credential_file_skipped")
	assertPayloadWarning(t, documents[0].Payload, "archive_symlink_skipped")
	assertPayloadWarning(t, documents[0].Payload, "archive_special_file_skipped")
	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	if sectionByHeading(sections, "Public Notes") == nil {
		t.Fatalf("missing Public Notes section in %#v", sections)
	}
	for _, envelope := range envelopes {
		payload := fmt.Sprint(envelope.Payload)
		if strings.Contains(payload, "SECRET_TOKEN") ||
			strings.Contains(payload, "nested bytes") ||
			strings.Contains(payload, "must-not-emit") {
			t.Fatalf("skipped member content leaked into payload: %#v", envelope.Payload)
		}
	}
}

type archiveTAREntry struct {
	name     string
	body     string
	linkname string
	typeflag byte
	mode     fs.FileMode
}

func buildTestArchiveTAR(t *testing.T, entries []archiveTAREntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	writeTestTAREntries(t, writer, entries)
	if err := writer.Close(); err != nil {
		t.Fatalf("tar Close() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func buildTestArchiveTARGZ(t *testing.T, entries []archiveTAREntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	writeTestTAREntries(t, tarWriter, entries)
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar Close() error = %v, want nil", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func writeTestTAREntries(t *testing.T, writer *tar.Writer, entries []archiveTAREntry) {
	t.Helper()

	for _, entry := range entries {
		body := []byte(entry.body)
		header := &tar.Header{
			Name:     entry.name,
			Mode:     int64(entry.mode.Perm()),
			Size:     int64(len(body)),
			Typeflag: entry.typeflag,
			Linkname: entry.linkname,
		}
		if entry.typeflag != tar.TypeReg && entry.typeflag != 0 {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v, want nil", entry.name, err)
		}
		if header.Size > 0 {
			if _, err := writer.Write(body); err != nil {
				t.Fatalf("Write(%q) error = %v, want nil", entry.name, err)
			}
		}
	}
}

func assertTarMemberDocument(t *testing.T, documents []facts.Envelope, repoPath string, archivePath string, memberPath string) {
	t.Helper()

	docID := "doc:git:repository:r_12345678:" + archivePath + "!/" + memberPath
	doc := documentByID(documents, docID)
	if doc == nil {
		t.Fatalf("missing contained document %q in %#v", docID, documents)
	}
	if got, want := doc.SourceRef.SourceURI, filepath.Join(repoPath, filepath.FromSlash(archivePath)); got != want {
		t.Fatalf("contained document SourceURI = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(doc.Payload, "archive_path"), archivePath; got != want {
		t.Fatalf("archive_path = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(doc.Payload, "archive_member_path"), memberPath; got != want {
		t.Fatalf("archive_member_path = %q, want %q", got, want)
	}
	if got := payloadSourceMetadataValue(doc.Payload, "archive_member_hash"); got == "" {
		t.Fatal("archive_member_hash = empty, want contained content hash")
	}
	assertDocumentationFactLinkedRepository(t, *doc, "repository:r_12345678")
}
