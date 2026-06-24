// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/collector/archivepreflight"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func (r *archiveDocumentationResult) extractTARGZMembers(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	outerID string,
	archivePath string,
	revisionID string,
	commitSHA string,
	body []byte,
) error {
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	r.extractTARMembers(ctx, repo, outerID, archivePath, revisionID, commitSHA, reader)
	return nil
}

func (r *archiveDocumentationResult) extractTARMembers(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	outerID string,
	archivePath string,
	revisionID string,
	commitSHA string,
	reader io.Reader,
) {
	tarReader := tar.NewReader(reader)
	skipped := 0
	supported := 0
	ordinal := 0
	for {
		if err := ctx.Err(); err != nil {
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningTimeout))
			break
		}
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningMalformedContainer))
			break
		}
		ordinal++
		memberPath, ok := normalizeArchiveMemberPath(header.Name)
		if !ok {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningArchivePathEscape))
			continue
		}
		if header.FileInfo().IsDir() {
			continue
		}
		if archiveTARHeaderIsUnsafe(header) {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, archiveTARHeaderWarning(header))
			continue
		}
		if archiveMemberIsNested(memberPath) {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningArchiveNestedSkipped))
			continue
		}
		if archiveMemberLooksCredential(memberPath) {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningCredentialFileSkipped))
			continue
		}
		format, ok := gitDocumentationFormatForPath(memberPath)
		if !ok || gitDocumentationFormatIsArchive(format) || format.format == "xls" {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, archiveUnsupportedWarning(memberPath))
			continue
		}
		memberBody, ok := readArchiveTARMember(tarReader, header, documentationReadLimitBytes(format))
		if !ok {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningResourceLimitExceeded))
			continue
		}
		supported++
		memberHash := documentationHashText(string(memberBody))
		sourceURI := archiveMemberSourceURI(archivePath, memberPath)
		document, sections, links := extractGitDocumentation(ctx, repo, sourceURI, memberHash, commitSHA, memberBody, format)
		archiveMetadata := archiveMemberMetadata(archivePath, memberPath, ordinal, memberHash)
		decorateArchiveDocument(&document, outerID, gitDocumentationCanonicalURI(repo, archivePath, commitSHA), archiveMetadata)
		for i := range sections {
			decorateArchiveSection(&sections[i], archiveMetadata)
		}
		for i := range links {
			decorateArchiveLink(&links[i], archiveMetadata)
		}
		r.documents = append(r.documents, document)
		r.sections = append(r.sections, sections...)
		r.links = append(r.links, links...)
	}
	r.outerDocument.SourceMetadata["supported_member_count"] = strconv.Itoa(supported)
	r.outerDocument.SourceMetadata["skipped_member_count"] = strconv.Itoa(skipped)
	r.outerDocument.SourceMetadata["contained_document_count"] = strconv.Itoa(len(r.documents))
}

func archiveTARHeaderIsUnsafe(header *tar.Header) bool {
	switch header.Typeflag {
	case tar.TypeReg, 0:
		return false
	default:
		return true
	}
}

func archiveTARHeaderWarning(header *tar.Header) string {
	switch header.Typeflag {
	case tar.TypeSymlink, tar.TypeLink:
		return string(archivepreflight.WarningArchiveSymlinkSkipped)
	default:
		return string(archivepreflight.WarningArchiveSpecialFileSkipped)
	}
}

func readArchiveTARMember(reader *tar.Reader, header *tar.Header, maxBytes int) ([]byte, bool) {
	if header.Size < 0 || header.Size > int64(maxBytes) {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes+1)))
	if err != nil {
		return nil, false
	}
	if len(body) > maxBytes || int64(len(body)) != header.Size {
		return nil, false
	}
	return body, true
}
