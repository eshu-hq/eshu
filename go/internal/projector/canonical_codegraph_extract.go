// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
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

	// FactID and FactKind identify the file fact this reference came from, so
	// an extractor that cannot read one of its parser buckets can quarantine
	// the fact by name instead of silently reading it as "this file has
	// nothing". Without them a malformed bucket is invisible: the delta
	// refresh still deletes the file's edges and then writes none.
	FactID   string
	FactKind string
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
		if !isRepositoryLocalRelativePath(relativePath) {
			// A fact that would have produced graph rows is being discarded,
			// so it takes the package's visible dead-letter path rather than a
			// bare skip: recordProjectorQuarantinedFacts turns this into the
			// eshu_dp_projector_input_invalid_facts_total increment plus a
			// structured error log naming the fact and relative_path. Without
			// it, a file absent from the graph is indistinguishable from one
			// the collector never emitted.
			quarantined = append(quarantined, quarantinedFact{
				factID:         fileFacts[i].FactID,
				factKind:       fileFacts[i].FactKind,
				field:          "relative_path",
				classification: factschema.ClassificationInputInvalid,
			})
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
			FactID:         fileFacts[i].FactID,
			FactKind:       fileFacts[i].FactKind,
		})
	}

	return rows, parsed, quarantined
}

// isRepositoryLocalRelativePath reports whether a file fact's relative_path
// stays inside the repository it was collected from.
//
// It exists because the canonical graph derives a file's directory from this
// string and nothing downstream re-checks it. qualifyPath only concatenates,
// while path.Dir cleans, so "../beta/src/leak.go" under /repos/alpha yields the
// directory /repos/beta/src -- a SIBLING repository's path. buildDirectoryChain
// then walks that chain out of the repository and stamps every directory it
// creates, including the sibling's, with THIS repository's repo_id, and the
// canonical file phase links this repository's file to it.
//
// Both halves break a grant. The #6541 directory language query admits rows by
// `d.repo_id` and counts the files CONTAINS-linked to the directory without
// re-checking any file (buildDirectoryCypher, go/internal/query/language/cypher.go),
// so a re-pointed directory disappears for the caller granted the sibling and
// is counted for the caller granted this repository. Rejecting the path here
// keeps that query's premise -- one directory, one repository -- true by
// construction rather than by convention.
//
// Production discovery cannot emit such a path: every relative_path it writes
// comes from filepath.Rel against the repository root
// (go/internal/collector/repo/discovery/filesystem_walk.go). This is a guard on a
// malformed or hostile fact, so a rejected row is QUARANTINED rather than
// skipped: it would otherwise have produced Directory and File rows, and an
// operator needs to tell a fact this guard dropped from one that was never
// emitted. The caller routes it through recordProjectorQuarantinedFacts on the
// input_invalid counter and log, the same visible dead-letter a decode failure
// takes. The empty-relative_path skip beside it stays silent: it materializes
// nothing either way, so there is no missing row to explain.
func isRepositoryLocalRelativePath(relativePath string) bool {
	if path.IsAbs(relativePath) {
		return false
	}
	cleaned := path.Clean(relativePath)
	return cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}
