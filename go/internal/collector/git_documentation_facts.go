// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const gitDocumentationSourceType = "repository_documentation"

func gitDocumentationFactCount(
	ctx context.Context,
	repoPath string,
	repo repositoryidentity.Metadata,
	scopeID string,
	generationID string,
	observedAt time.Time,
	snapshot RepositorySnapshot,
) int {
	total := 0
	sourceEmitted := false
	documentationPaths := documentationMetaRelativePaths(snapshot.DocumentationFileMetas)
	if len(snapshot.ContentFileMetas) > 0 {
		for _, meta := range snapshot.ContentFileMetas {
			if documentationPaths[meta.RelativePath] {
				continue
			}
			if !isDocumentationPathOrStructuredAPIContractCandidate(meta.RelativePath) {
				continue
			}
			body, ok := readDocumentationCandidateBody(repoPath, meta.RelativePath, nil)
			if !ok {
				continue
			}
			envelopes, emitted := gitDocumentationEnvelopesForContentFile(
				ctx,
				repoPath,
				repo,
				scopeID,
				generationID,
				observedAt,
				meta.RelativePath,
				meta.Digest,
				meta.CommitSHA,
				body,
				!sourceEmitted,
			)
			total += len(envelopes)
			sourceEmitted = sourceEmitted || emitted
		}
	} else {
		for _, fileSnapshot := range snapshot.ContentFiles {
			if documentationPaths[fileSnapshot.RelativePath] {
				continue
			}
			if !isDocumentationPathOrStructuredAPIContractCandidate(fileSnapshot.RelativePath) {
				continue
			}
			envelopes, emitted := gitDocumentationEnvelopesForContentFile(
				ctx,
				repoPath,
				repo,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.Digest,
				fileSnapshot.CommitSHA,
				[]byte(fileSnapshot.Body),
				!sourceEmitted,
			)
			total += len(envelopes)
			sourceEmitted = sourceEmitted || emitted
		}
	}
	for _, meta := range snapshot.DocumentationFileMetas {
		if _, _, ok := gitDocumentationSourceURIAndFormat(meta.RelativePath); !ok {
			continue
		}
		body, ok := readDocumentationBody(repoPath, meta.RelativePath, nil)
		if !ok {
			continue
		}
		envelopes, emitted := gitDocumentationEnvelopesForContentFile(
			ctx,
			repoPath,
			repo,
			scopeID,
			generationID,
			observedAt,
			meta.RelativePath,
			meta.Digest,
			meta.CommitSHA,
			body,
			!sourceEmitted,
		)
		total += len(envelopes)
		sourceEmitted = sourceEmitted || emitted
	}
	return total
}

func emitGitDocumentationFactsForContentFile(
	ctx context.Context,
	ch chan<- facts.Envelope,
	repoPath string,
	repo repositoryidentity.Metadata,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	emitSource bool,
) bool {
	envelopes, sourceEmitted := gitDocumentationEnvelopesForContentFile(
		ctx,
		repoPath,
		repo,
		scopeID,
		generationID,
		observedAt,
		relativePath,
		digest,
		commitSHA,
		body,
		emitSource,
	)
	for _, envelope := range envelopes {
		ch <- envelope
	}
	return sourceEmitted
}

func gitDocumentationEnvelopesForContentFile(
	ctx context.Context,
	repoPath string,
	repo repositoryidentity.Metadata,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	emitSource bool,
) ([]facts.Envelope, bool) {
	sourceURI, format, ok := gitDocumentationSourceURIAndFormatForBody(relativePath, body)
	if !ok {
		return nil, false
	}
	if gitDocumentationFormatIsArchive(format) {
		return gitDocumentationArchiveEnvelopes(
			ctx,
			repoPath,
			repo,
			scopeID,
			generationID,
			observedAt,
			sourceURI,
			digest,
			commitSHA,
			body,
			emitSource,
		), emitSource
	}
	document, sections, links := extractGitDocumentation(ctx, repo, sourceURI, digest, commitSHA, body, format)
	out := make([]facts.Envelope, 0, 1+1+len(sections)+len(links))
	if emitSource {
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
		out = append(out, gitDocumentationEnvelope(
			repoPath,
			repo.ID,
			scopeID,
			generationID,
			observedAt,
			facts.DocumentationSourceFactKind,
			facts.DocumentationSourceStableID(sourcePayload),
			sourcePayload,
			repoPath,
		))
	}
	sourceFile := filepath.Join(repoPath, filepath.FromSlash(sourceURI))
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
	for _, section := range sections {
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
	for _, link := range links {
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
	if gitDocumentationFormatEmitsTruth(format) {
		out = append(out, gitDocumentationTruthEnvelopes(
			repo,
			scopeID,
			generationID,
			observedAt,
			document,
			sections,
			links,
		)...)
	}
	return out, emitSource
}

func readDocumentationBody(repoPath string, relativePath string, body []byte) ([]byte, bool) {
	if body != nil {
		return body, true
	}
	sourceURI, format, ok := gitDocumentationSourceURIAndFormat(relativePath)
	if !ok {
		return nil, false
	}
	if format.format == "xls" {
		return nil, true
	}
	file, err := os.Open(filepath.Join(repoPath, filepath.FromSlash(sourceURI)))
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, int64(documentationReadLimitBytes(format)+1)))
	if err != nil {
		return nil, false
	}
	return raw, true
}

func readDocumentationCandidateBody(repoPath string, relativePath string, body []byte) ([]byte, bool) {
	if body != nil {
		return body, true
	}
	sourceURI, format, ok := gitDocumentationSourceURIAndFormat(relativePath)
	var readLimit int
	if ok {
		if format.format == "xls" {
			return nil, true
		}
		readLimit = documentationReadLimitBytes(format)
	} else {
		sourceURI, ok = documentationSourceURI(relativePath)
		if !ok || !isPotentialStructuredAPIContractPath(sourceURI) {
			return nil, false
		}
		readLimit = apiContractMaxBodyBytes
	}
	file, err := os.Open(filepath.Join(repoPath, filepath.FromSlash(sourceURI)))
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, int64(readLimit+1)))
	if err != nil {
		return nil, false
	}
	return raw, true
}

func documentationReadLimitBytes(format gitDocumentationFormat) int {
	switch format.format {
	case "openapi", "swagger", "asyncapi", "graphql_sdl":
		return apiContractMaxBodyBytes
	}
	if format.format == "notebook" {
		return notebookMaxBodyBytes
	}
	if format.format == "docx" || format.format == "xlsx" || format.format == "pptx" {
		return 50 << 20
	}
	if gitDocumentationFormatIsArchive(format) {
		return 100 << 20
	}
	return documentationMaxBodyBytes
}

func documentationMetaRelativePaths(metas []ContentFileMeta) map[string]bool {
	paths := make(map[string]bool, len(metas))
	for _, meta := range metas {
		if meta.RelativePath != "" {
			paths[meta.RelativePath] = true
		}
	}
	return paths
}

func extractMarkdownDocumentationWithFormat(
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	format string,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	bodyText, warnings := boundedDocumentationBody(body)
	lines := markdownContentLines(bodyText)
	sections := markdownSections(documentID, revisionID, relativePath, lines, format)
	title := documentationTitle(relativePath, sections)
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   documentID,
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        title,
		DocumentType: documentationDocumentType(relativePath, format),
		Format:       format,
		Language:     "en",
		ContentHash:  firstNonEmptyString(digest, documentationHashText(bodyText)),
		SourceMetadata: map[string]string{
			"path":    relativePath,
			"repo_id": repo.ID,
		},
	}
	if commitSHA != "" {
		document.SourceMetadata["source_revision"] = commitSHA
	}
	addDocumentationWarnings(document.SourceMetadata, warnings...)
	links := markdownLinks(relativePath, sections)
	return document, sections, links
}

func documentationSourceURI(relativePath string) (string, bool) {
	sourceURI := path.Clean(filepath.ToSlash(strings.TrimSpace(relativePath)))
	if sourceURI == "." || path.IsAbs(sourceURI) || sourceURI == ".." || strings.HasPrefix(sourceURI, "../") {
		return "", false
	}
	return sourceURI, true
}

func gitDocumentationSourceID(repoID string) string {
	return "doc-source:git:" + repoID
}

func gitDocumentationDocumentID(repoID string, relativePath string) string {
	return "doc:git:" + repoID + ":" + relativePath
}

func gitDocumentationCanonicalURI(repo repositoryidentity.Metadata, relativePath string, revisionID string) string {
	if repo.RemoteURL == "" {
		return "git://" + strings.TrimPrefix(repo.ID, "repository:") + "/" + relativePath
	}
	base := strings.TrimSuffix(repo.RemoteURL, "/")
	if strings.Contains(base, "github.com/") && revisionID != "" && revisionID != "unknown" {
		return base + "/blob/" + revisionID + "/" + relativePath
	}
	return base + "#" + relativePath
}

func gitDocumentationEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	factKind string,
	factKey string,
	payload any,
	sourceURI string,
) facts.Envelope {
	payloadMap, err := documentationPayloadMap(payload)
	if err != nil {
		payloadMap = map[string]any{"payload_error": err.Error()}
	}
	payloadMap["linked_entities"] = []map[string]string{{
		"entity_type": "repository",
		"entity_id":   repoID,
	}}
	envelope := factEnvelope(factKind, scopeID, generationID, observedAt, factKey, payloadMap, sourceURI)
	if factKind == facts.DocumentationSectionFactKind {
		envelope.SchemaVersion = facts.DocumentationSectionFactSchemaVersion
	} else {
		envelope.SchemaVersion = facts.DocumentationFactSchemaVersion
	}
	if sourceURI == repoPath {
		envelope.SourceRef.SourceRecordID = factKey
	} else {
		envelope.SourceRef.SourceRecordID = repositoryRelativePath(repoPath, sourceURI)
	}
	return envelope
}

func documentationPayloadMap(payload any) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func documentationHashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}
