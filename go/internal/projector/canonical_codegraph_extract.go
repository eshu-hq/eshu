// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// extractRepository builds a RepositoryRow from the first RepositoryObserved
// fact envelope.
func extractRepository(envelopes []facts.Envelope) *RepositoryRow {
	row, _ := extractRepositoryWithQuarantine(envelopes)
	return row
}

// extractRepositoryWithQuarantine builds a RepositoryRow from the first
// decodable RepositoryObserved fact envelope. A fact missing a required typed
// identity field is quarantined and skipped, so a valid sibling repository fact
// can still set the materialization's repo identity and path.
func extractRepositoryWithQuarantine(envelopes []facts.Envelope) (*RepositoryRow, []quarantinedFact) {
	repoFacts := FilterRepositoryFacts(envelopes)
	if len(repoFacts) == 0 {
		return nil, nil
	}

	var quarantined []quarantinedFact
	for i := range repoFacts {
		repository, err := decodeCodegraphRepository(repoFacts[i])
		if err != nil {
			q, isQuarantine, fatal := partitionProjectorDecodeFailures(repoFacts[i], err)
			if fatal != nil {
				continue
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}

		p := repoFacts[i].Payload
		repoID := repository.RepoID
		name := codegraphDerefString(repository.Name)
		repoPath, _ := payloadString(p, "path")
		localPath := codegraphDerefString(repository.LocalPath)
		remoteURL := codegraphDerefString(repository.RemoteURL)
		repoSlug := codegraphDerefString(repository.RepoSlug)

		// The collector does not emit "path" — fall back to local_path which is
		// unique per repository and satisfies the Repository.path constraint.
		// Legacy tests/fixtures may still carry "path", so preserve it when
		// present without adding it to the public codegraph contract.
		if repoPath == "" {
			repoPath = localPath
		}

		// The collector does not emit "has_remote" — derive from remote_url
		// presence which the collector sets when the repository has an origin.
		hasRemote := false
		if ptr := payloadBoolPtr(p, "has_remote"); ptr != nil {
			hasRemote = *ptr
		} else {
			hasRemote = remoteURL != ""
		}

		return &RepositoryRow{
			RepoID:    repoID,
			Name:      name,
			Path:      repoPath,
			LocalPath: localPath,
			RemoteURL: remoteURL,
			RepoSlug:  repoSlug,
			HasRemote: hasRemote,
		}, quarantined
	}
	return nil, quarantined
}

// parsedFileRef is one decoded, materializable file fact, handed to the
// extractors that read the parser's per-file buckets out of parsed_file_data.
//
// It exists so those extractors do not decode the same file fact a second time.
// decodeCodegraphFile runs a reflect-based typed decode per envelope; on a
// 2,000-file generation a second pass costs about 1ms and 7,900 allocations for
// data extractFilesWithQuarantine already holds. Path and Language are carried
// alongside so a consumer does not recompute the repo-qualified path either.
type parsedFileRef struct {
	Path           string
	Language       string
	ParsedFileData map[string]any
}

// extractFilesWithQuarantine builds FileRow entries from typed file fact
// envelopes. A fact missing a required typed identity field is quarantined and
// skipped; present-but-empty identity fields still decode and are dropped by the
// row builder's materialization gate, matching the pre-typing behavior.
//
// It also returns one parsedFileRef per materialized file, in the same order,
// so the parsed_file_data extractors (currently the import extractor, issue
// #5691) can read the parser buckets off an already-decoded file.
func extractFilesWithQuarantine(envelopes []facts.Envelope, repoID, repoPath string) ([]FileRow, []parsedFileRef, []quarantinedFact) {
	fileFacts := FilterFileFacts(envelopes)
	var rows []FileRow
	var parsed []parsedFileRef
	var quarantined []quarantinedFact

	for i := range fileFacts {
		if fileFacts[i].IsTombstone {
			continue
		}

		file, err := decodeCodegraphFile(fileFacts[i])
		if err != nil {
			q, isQuarantine, fatal := partitionProjectorDecodeFailures(fileFacts[i], err)
			if fatal != nil {
				continue
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}

		relativePath := strings.TrimSpace(file.RelativePath)
		if relativePath == "" {
			continue
		}

		fullPath := qualifyPath(repoPath, relativePath)
		name := path.Base(relativePath)
		language := codegraphDerefString(file.Language)
		dirPath := path.Dir(fullPath)

		rows = append(rows, FileRow{
			Path:         fullPath,
			RelativePath: relativePath,
			Name:         name,
			Language:     language,
			RepoID:       repoID,
			DirPath:      dirPath,
		})
		parsed = append(parsed, parsedFileRef{
			Path:           fullPath,
			Language:       strings.TrimSpace(language),
			ParsedFileData: file.ParsedFileData,
		})
	}

	return rows, parsed, quarantined
}
