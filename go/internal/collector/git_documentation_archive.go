// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/zip"
	"bytes"
	"context"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/archivepreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	archiveMaxEntries          = 1000
	archiveMaxExpandedBytes    = int64(256 << 20)
	archiveMaxCompressionRatio = 100
)

type archiveDocumentationResult struct {
	outerDocument facts.DocumentationDocumentPayload
	documents     []facts.DocumentationDocumentPayload
	sections      []facts.DocumentationSectionPayload
	links         []facts.DocumentationLinkPayload
}

func gitDocumentationArchiveEnvelopes(
	ctx context.Context,
	repoPath string,
	repo repositoryidentity.Metadata,
	scopeID string,
	generationID string,
	observedAt time.Time,
	sourceURI string,
	digest string,
	commitSHA string,
	body []byte,
	emitSource bool,
) []facts.Envelope {
	result := extractArchiveDocumentation(ctx, repo, sourceURI, digest, commitSHA, body)
	out := make([]facts.Envelope, 0, 1+1+len(result.documents)+len(result.sections)+len(result.links))
	if emitSource {
		out = append(out, gitDocumentationSourceEnvelope(repoPath, repo, scopeID, generationID, observedAt))
	}
	sourceFile := filepath.Join(repoPath, filepath.FromSlash(sourceURI))
	out = append(out, gitDocumentationEnvelope(
		repoPath,
		repo.ID,
		scopeID,
		generationID,
		observedAt,
		facts.DocumentationDocumentFactKind,
		facts.DocumentationDocumentStableID(result.outerDocument),
		result.outerDocument,
		sourceFile,
	))
	for _, document := range result.documents {
		out = append(out, gitDocumentationEnvelope(
			repoPath,
			repo.ID,
			scopeID,
			generationID,
			observedAt,
			facts.DocumentationDocumentFactKind,
			facts.DocumentationDocumentStableID(document),
			document,
			sourceFile,
		))
	}
	for _, section := range result.sections {
		out = append(out, gitDocumentationEnvelope(
			repoPath,
			repo.ID,
			scopeID,
			generationID,
			observedAt,
			facts.DocumentationSectionFactKind,
			facts.DocumentationSectionStableID(section),
			section,
			sourceFile,
		))
	}
	for _, link := range result.links {
		out = append(out, gitDocumentationEnvelope(
			repoPath,
			repo.ID,
			scopeID,
			generationID,
			observedAt,
			facts.DocumentationLinkFactKind,
			facts.DocumentationLinkStableID(link),
			link,
			sourceFile,
		))
	}
	return out
}

func extractArchiveDocumentation(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
) archiveDocumentationResult {
	if ctx == nil {
		ctx = context.Background()
	}
	archiveFormat := "zip"
	if format, ok := gitDocumentationFormatForPath(relativePath); ok && gitDocumentationFormatIsArchive(format) {
		archiveFormat = format.format
	}
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	outerID := gitDocumentationDocumentID(repo.ID, relativePath)
	result := archiveDocumentationResult{
		outerDocument: archiveDocumentPayload(repo, outerID, relativePath, revisionID, digest, commitSHA, body, archiveFormat),
	}
	preflight, err := archivepreflight.Preflight(
		ctx,
		path.Base(relativePath),
		bytes.NewReader(body),
		int64(len(body)),
		archivepreflight.Options{
			MaxSourceBytes:      int64(documentationReadLimitBytes(gitDocumentationFormat{format: archiveFormat})),
			MaxExpandedBytes:    archiveMaxExpandedBytes,
			MaxEntries:          archiveMaxEntries,
			MaxCompressionRatio: archiveMaxCompressionRatio,
		},
	)
	recordArchivePreflightMetadata(result.outerDocument.SourceMetadata, preflight)
	addDocumentationWarnings(result.outerDocument.SourceMetadata, archivePreflightWarningStrings(preflight)...)
	if err != nil || archivePreflightHasFatalWarning(preflight) {
		return result
	}
	switch preflight.Format {
	case archivepreflight.FormatZIP:
		reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			addDocumentationWarnings(result.outerDocument.SourceMetadata, string(archivepreflight.WarningMalformedContainer))
			return result
		}
		result.extractZIPMembers(ctx, repo, outerID, relativePath, revisionID, commitSHA, reader)
	case archivepreflight.FormatTAR:
		result.extractTARMembers(ctx, repo, outerID, relativePath, revisionID, commitSHA, bytes.NewReader(body))
	case archivepreflight.FormatTARGZ:
		if err := result.extractTARGZMembers(ctx, repo, outerID, relativePath, revisionID, commitSHA, body); err != nil {
			addDocumentationWarnings(result.outerDocument.SourceMetadata, string(archivepreflight.WarningMalformedContainer))
		}
	default:
		addDocumentationWarnings(result.outerDocument.SourceMetadata, string(archivepreflight.WarningUnsupportedFormat))
	}
	return result
}

func archiveDocumentPayload(
	repo repositoryidentity.Metadata,
	documentID string,
	relativePath string,
	revisionID string,
	digest string,
	commitSHA string,
	body []byte,
	archiveFormat string,
) facts.DocumentationDocumentPayload {
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   documentID,
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        documentationTitle(relativePath, nil),
		DocumentType: "archive",
		Format:       archiveFormat,
		Language:     "en",
		ContentHash:  firstNonEmptyString(digest, documentationHashText(string(body))),
		SourceMetadata: map[string]string{
			"path":           relativePath,
			"repo_id":        repo.ID,
			"archive_format": archiveFormat,
		},
	}
	if commitSHA != "" {
		document.SourceMetadata["source_revision"] = commitSHA
	}
	return document
}

func (r *archiveDocumentationResult) extractZIPMembers(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	outerID string,
	archivePath string,
	revisionID string,
	commitSHA string,
	reader *zip.Reader,
) {
	skipped := 0
	supported := 0
	for ordinal, file := range reader.File {
		if err := ctx.Err(); err != nil {
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningTimeout))
			break
		}
		memberPath, ok := normalizeArchiveMemberPath(file.Name)
		if !ok {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningArchivePathEscape))
			continue
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if archiveZipModeIsUnsafe(file.FileInfo().Mode()) {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, archiveZipModeWarning(file.FileInfo().Mode()))
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
		memberBody, ok := readArchiveZIPMember(file, documentationReadLimitBytes(format))
		if !ok {
			skipped++
			addDocumentationWarnings(r.outerDocument.SourceMetadata, string(archivepreflight.WarningResourceLimitExceeded))
			continue
		}
		supported++
		memberHash := documentationHashText(string(memberBody))
		sourceURI := archiveMemberSourceURI(archivePath, memberPath)
		document, sections, links := extractGitDocumentation(ctx, repo, sourceURI, memberHash, commitSHA, memberBody, format)
		archiveMetadata := archiveMemberMetadata(archivePath, memberPath, ordinal+1, memberHash)
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

func gitDocumentationSourceEnvelope(
	repoPath string,
	repo repositoryidentity.Metadata,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	sourcePayload := facts.DocumentationSourcePayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		SourceSystem: "git",
		ExternalID:   repo.ID,
		DisplayName:  firstNonEmptyString(repo.Name, repo.RepoSlug, repo.ID),
		BaseURI:      repo.RemoteURL,
		SourceType:   gitDocumentationSourceType,
		ACLSummary: &facts.DocumentationACLSummary{
			Visibility:    "repository",
			IsPartial:     true,
			PartialReason: "repository_acl_not_collected",
		},
		SourceMetadata: map[string]string{
			"repo_id": repo.ID,
		},
	}
	if repo.RepoSlug != "" {
		sourcePayload.SourceMetadata["repo_slug"] = repo.RepoSlug
	}
	return gitDocumentationEnvelope(
		repoPath,
		repo.ID,
		scopeID,
		generationID,
		observedAt,
		facts.DocumentationSourceFactKind,
		facts.DocumentationSourceStableID(sourcePayload),
		sourcePayload,
		repoPath,
	)
}
