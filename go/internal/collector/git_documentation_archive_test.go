// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsZIPArchiveDocumentationPacket(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/support-packet.zip"
	body := buildTestArchiveZIP(t, []archiveZIPEntry{
		{name: "runbook.md", body: "# Restore Service\n\nFollow the recovery checklist.\n", mode: 0o644},
		{name: "tables/service-inventory.csv", body: "service,dependency\npayments-api,postgres\n", mode: 0o644},
		{name: "notes/debug.bin", body: "not documentation", mode: 0o644},
	})
	writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(archivePath)), body)

	envelopes := streamArchiveFacts(t, repoPath, archivePath, "sha256:archive")

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 3; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	outer := documentByID(documents, "doc:git:repository:r_12345678:"+archivePath)
	if outer == nil {
		t.Fatalf("missing outer archive document in %#v", documents)
	}
	if got, want := payloadString(outer.Payload, "format"), "zip"; got != want {
		t.Fatalf("outer format = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(outer.Payload, "archive_format"), "zip"; got != want {
		t.Fatalf("archive_format = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(outer.Payload, "supported_member_count"), "2"; got != want {
		t.Fatalf("supported_member_count = %q, want %q", got, want)
	}
	assertPayloadWarning(t, outer.Payload, "unsupported_format")

	for _, memberPath := range []string{"runbook.md", "tables/service-inventory.csv"} {
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

	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	if sectionByHeading(sections, "Restore Service") == nil {
		t.Fatalf("missing Restore Service section in %#v", sections)
	}
	assertSectionContentContains(t, sections, "payments-api")
	for _, section := range sections {
		if got, want := section.SourceRef.SourceURI, filepath.Join(repoPath, filepath.FromSlash(archivePath)); got != want {
			t.Fatalf("section SourceURI = %q, want %q", got, want)
		}
		if got := payloadSourceMetadataValue(section.Payload, "archive_member_path"); got == "" {
			t.Fatalf("section missing archive member metadata: %#v", section.Payload)
		}
		assertDocumentationFactLinkedRepository(t, section, "repository:r_12345678")
	}
}

func TestStreamFactsRejectsUnsafeZIPArchiveMembers(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/unsafe-packet.zip"
	body := buildTestArchiveZIP(t, []archiveZIPEntry{
		{name: "../escape.md", body: "# Escape\n\nmust not emit\n", mode: 0o644},
		{name: "docs/good.md", body: "# Good\n\nalso blocked by unsafe package\n", mode: 0o644},
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

func TestStreamFactsSkipsNestedAndCredentialZIPMembers(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/mixed-packet.zip"
	body := buildTestArchiveZIP(t, []archiveZIPEntry{
		{name: "docs/readme.md", body: "# Public Notes\n\nSafe summary.\n", mode: 0o644},
		{name: "nested.zip", body: "nested bytes", mode: 0o644},
		{name: ".env", body: "SECRET_TOKEN=do-not-emit\n", mode: 0o644},
		{name: "secret.md", body: "# Secret Packet\n\nmust-not-emit\n", mode: 0o644},
	})
	writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(archivePath)), body)

	envelopes := streamArchiveFacts(t, repoPath, archivePath, "sha256:mixed")

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "archive_nested_skipped")
	assertPayloadWarning(t, documents[0].Payload, "credential_file_skipped")
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

func TestStreamFactsSkipsOversizeZIPMembers(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/oversize-packet.zip"
	oversizeBody := oversizeArchiveMemberBody()
	body := buildTestArchiveZIP(t, []archiveZIPEntry{
		{name: "docs/large.md", body: oversizeBody, mode: 0o644},
		{name: "docs/safe.md", body: "# Safe Member\n\nSmall enough.\n", mode: 0o644},
	})
	writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(archivePath)), body)

	envelopes := streamArchiveFacts(t, repoPath, archivePath, "sha256:oversize")

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "resource_limit_exceeded")
	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	if sectionByHeading(sections, "Safe Member") == nil {
		t.Fatalf("missing safe member section in %#v", sections)
	}
	oversizePrefix := strings.SplitN(oversizeBody, "\n", 2)[0]
	for _, envelope := range envelopes {
		if strings.Contains(fmt.Sprint(envelope.Payload), oversizePrefix) {
			t.Fatalf("oversize member content leaked into payload: %#v", envelope.Payload)
		}
	}
}

func TestStreamFactsSkipsSymlinkZIPMembersWithoutBlockingSafeMembers(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/symlink-packet.zip"
	body := buildTestArchiveZIP(t, []archiveZIPEntry{
		{name: "docs/link.md", body: "safe.md", mode: fs.ModeSymlink | 0o777},
		{name: "docs/safe.md", body: "# Safe Member\n\nStill indexed.\n", mode: 0o644},
	})
	writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(archivePath)), body)

	envelopes := streamArchiveFacts(t, repoPath, archivePath, "sha256:symlink")

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "archive_symlink_skipped")
	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	if sectionByHeading(sections, "Safe Member") == nil {
		t.Fatalf("missing safe member section in %#v", sections)
	}
}

func TestStreamFactsBoundsZIPArchiveResources(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/too-many-members.zip"
	const testArchiveMaxEntries = 1000
	entries := make([]archiveZIPEntry, 0, testArchiveMaxEntries+1)
	for i := 0; i <= testArchiveMaxEntries; i++ {
		entries = append(entries, archiveZIPEntry{
			name: fmt.Sprintf("docs/member-%04d.md", i),
			body: "# Member\n\nbounded.\n",
			mode: 0o644,
		})
	}
	body := buildTestArchiveZIP(t, entries)
	writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(archivePath)), body)

	envelopes := streamArchiveFacts(t, repoPath, archivePath, "sha256:many")

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "resource_limit_exceeded")
	if got := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)); got != 0 {
		t.Fatalf("documentation_section count = %d, want 0", got)
	}
}

func TestStreamFactsRejectsZIPCompressionRatioHazards(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	archivePath := "docs/repeated-packet.zip"
	body := buildTestArchiveZIP(t, []archiveZIPEntry{{
		name: "docs/repeated.md",
		body: strings.Repeat("a", 256*1024),
		mode: 0o644,
	}})
	writeCollectorTestBytes(t, filepath.Join(repoPath, filepath.FromSlash(archivePath)), body)

	envelopes := streamArchiveFacts(t, repoPath, archivePath, "sha256:ratio")

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "compression_ratio_exceeded")
	if got := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)); got != 0 {
		t.Fatalf("documentation_section count = %d, want 0", got)
	}
}

type archiveZIPEntry struct {
	name string
	body string
	mode fs.FileMode
}

func buildTestArchiveZIP(t *testing.T, entries []archiveZIPEntry) []byte {
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

func streamArchiveFacts(t *testing.T, repoPath string, relativePath string, digest string) []facts.Envelope {
	t.Helper()

	collected := buildStreamingGeneration(
		repoPath,
		testCollectorRepositoryMetadata(repoPath),
		"run-1",
		time.Date(2026, time.June, 9, 8, 45, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount: 1,
			DocumentationFileMetas: []ContentFileMeta{{
				RelativePath: relativePath,
				Digest:       digest,
				Language:     "zip",
				ArtifactType: "documentation",
				CommitSHA:    "abc123",
			}},
		},
		false,
	)
	return drainFactChannel(collected.Facts)
}

func oversizeArchiveMemberBody() string {
	var builder strings.Builder
	for i := 0; builder.Len() <= documentationMaxBodyBytes+1024; i++ {
		fmt.Fprintf(&builder, "%08x-%08x-%08x\n", i, i*1664525+1013904223, ^i)
	}
	return builder.String()
}

func documentByID(documents []facts.Envelope, documentID string) *facts.Envelope {
	for i := range documents {
		if payloadString(documents[i].Payload, "document_id") == documentID {
			return &documents[i]
		}
	}
	return nil
}
