// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// configArtifactReadsContent reports whether BuildConfigPaths
// reads this file's own content. Every selector it consults is the one the
// matching extractor branches on, so the set of files the load fetches
// cannot drift from the set the extractors read (#7126):
//
//   - kustomization files, which the kustomize walk decodes for references;
//   - Docker Compose artifacts, which the compose extractor decodes;
//   - terragrunt.hcl, .tf and .hcl files, which the HCL extractor scans;
//   - a conventional Ansible playbook name that the path rules leave to the
//     content heuristic.
//
// A file that returns false still contributes path-only rows (a var file, an
// Ansible inventory, vars or role path) when its content is not blank. A
// resource file a kustomization references is read too; that set depends on
// other files' content and is planned by planKustomizeResourceReads.
func configArtifactReadsContent(file File) bool {
	if isKustomizationIndexPath(CleanRelativePath(file.RelativePath)) {
		return true
	}
	if IsDockerComposeArtifact(file) {
		return true
	}
	if classifyHCLConfigFile(file.RelativePath) == hclConfigSourceFile {
		return true
	}
	if lowerPath, lowerBase, ok := ansiblePathKeys(file); ok && ansiblePlaybookReadsContent(lowerPath, lowerBase) {
		return true
	}
	return false
}

// planKustomizeResourceReads returns the relative paths of the resource files
// the kustomize walk will decode for these files. It runs the walk's own
// collectKustomizeResourceFiles from every kustomization in the same index
// the extractor builds, so it names exactly the files the extractor reads.
// The kustomization files must carry their content; resource files need not.
func planKustomizeResourceReads(files []File) map[string]struct{} {
	index := buildKustomizeIndex(files)
	kustomizationPaths := make([]string, 0)
	for relativePath := range index {
		if isKustomizationIndexPath(relativePath) {
			kustomizationPaths = append(kustomizationPaths, relativePath)
		}
	}
	sort.Strings(kustomizationPaths)

	reads := make(map[string]struct{})
	for _, kustomizationPath := range kustomizationPaths {
		for _, resource := range collectKustomizeResourceFiles(index, kustomizationPath) {
			reads[resource.RelativePath] = struct{}{}
		}
	}
	return reads
}

// hydrateConfigFiles returns the files BuildConfigPaths
// should see for one repository: every config artifact candidate with
// non-blank content, in list order. It produces the same extractor output as
// reading every candidate in full, while fetching content only for files an
// extractor reads (#7126):
//
//  1. Select the hydration set exactly as the full pass does: the first
//     allowance candidates in list order that carry no content yet, or every
//     candidate for AllowanceWholeWindow.
//  2. Fetch the content of the files configArtifactReadsContent accepts.
//  3. For the other selected files, ask the store which are non-blank without
//     moving content, in chunks of BlankCheckChunkSize paths. The load drops a blank file before building, and a
//     path-only row exists only for a non-blank file, so non-blankness is the
//     whole of what the extractors need from them.
//  4. Fetch the resource files the kustomizations reference, which phase 2
//     can name only once the kustomization content is known.
//
// A file whose content was not fetched keeps an empty Content; no extractor
// reads it. Any read error fails the repository, as in the full pass.
func hydrateConfigFiles(
	ctx context.Context,
	reader Reader,
	repoID string,
	files []File,
	allowance int,
	stats *ReadStats,
) ([]File, error) {
	return productionConfigHydration.hydrate(ctx, reader, repoID, files, allowance, stats)
}

// configContentFetcher reads the stored rows of hydrated at indexes and writes
// them back into hydrated.
// A path with no stored row keeps its list row. A failure fails the pass and
// the caller discards hydrated.
type configContentFetcher func(
	ctx context.Context,
	reader Reader,
	repoID string,
	hydrated []File,
	indexes []int,
	stats *ReadStats,
) error

// configHydrationPolicy names the three decisions the needed-content pass
// makes about content: which files an extractor reads by itself, which
// resource files the kustomizations pull in, and how the chosen files are
// read. Production uses productionConfigHydration. The field seam exists so
// the equality tests can run the same pass with one decision removed, or with
// the per-file reader of the earlier change, and compare the output.
type configHydrationPolicy struct {
	readsContent  func(File) bool
	planResources func([]File) map[string]struct{}
	fetch         configContentFetcher
}

var productionConfigHydration = configHydrationPolicy{
	readsContent:  configArtifactReadsContent,
	planResources: planKustomizeResourceReads,
	fetch:         productionChunkedRead.fetch,
}

func (policy configHydrationPolicy) hydrate(
	ctx context.Context,
	reader Reader,
	repoID string,
	files []File,
	allowance int,
	stats *ReadStats,
) ([]File, error) {
	hydrated := append([]File(nil), files...)
	var (
		deferred    = map[int]bool{}
		selected    int
		fetchedFull int
	)
	if reader != nil && repoID != "" && len(files) > 0 {
		indexes, candidates := SelectHydrationIndexes(hydrated, isConfigArtifactCandidate, fileContent, allowance)
		selected = len(indexes)
		stats.CandidateCount += candidates
		stats.CandidatesHydrated += selected
		needed := make([]int, 0, len(indexes))
		for _, index := range indexes {
			if policy.readsContent(hydrated[index]) {
				needed = append(needed, index)
				continue
			}
			deferred[index] = false
		}

		if err := policy.fetch(ctx, reader, repoID, hydrated, needed, stats); err != nil {
			return nil, err
		}
		fetchedFull += len(needed)

		if len(deferred) > 0 {
			if err := markNonBlankDeferredFiles(ctx, reader, repoID, hydrated, deferred, stats); err != nil {
				return nil, err
			}
		}
	}

	// working holds the files the extractors may see, with the position of
	// each in hydrated so a phase 2 read can write its row back.
	type workingFile struct {
		position int
		file     File
	}
	working := make([]workingFile, 0, len(hydrated))
	for position, file := range hydrated {
		if !isConfigArtifactCandidate(file) {
			continue
		}
		if isDeferredNonBlank(deferred, position) || strings.TrimSpace(file.Content) != "" {
			working = append(working, workingFile{position: position, file: file})
		}
	}

	if reader != nil && repoID != "" && len(working) > 0 {
		visible := make([]File, len(working))
		for i, item := range working {
			visible[i] = item.file
		}
		reads := policy.planResources(visible)
		positions := make([]int, 0, len(reads))
		for _, item := range working {
			if item.file.Content != "" {
				continue
			}
			if _, ok := reads[item.file.RelativePath]; ok {
				positions = append(positions, item.position)
			}
		}
		if err := policy.fetch(ctx, reader, repoID, hydrated, positions, stats); err != nil {
			return nil, err
		}
		fetchedFull += len(positions)
		for _, position := range positions {
			// The row is read now; the usual blank rule decides on its content,
			// so a row that vanished or went blank after the store's answer is
			// dropped as it is in the full pass.
			delete(deferred, position)
		}
	}

	contentFiles := make([]File, 0, len(working))
	for _, item := range working {
		file := hydrated[item.position]
		if !isConfigArtifactCandidate(file) {
			continue
		}
		if isDeferredNonBlank(deferred, item.position) && file.Content == "" {
			// Not fetched, and the store reported it non-blank.
			contentFiles = append(contentFiles, file)
			continue
		}
		if strings.TrimSpace(file.Content) == "" {
			continue
		}
		contentFiles = append(contentFiles, file)
	}

	stats.FilesHydrated += fetchedFull
	stats.FilesSkipped += selected - fetchedFull
	return contentFiles, nil
}

// isDeferredNonBlank reports whether the position was selected for hydration,
// not fetched, and reported non-blank by the store.
func isDeferredNonBlank(deferred map[int]bool, position int) bool {
	nonBlank, ok := deferred[position]
	return ok && nonBlank
}

// markNonBlankDeferredFiles asks the store which of the deferred files hold
// non-blank content, and records the answer in deferred. The distinct paths go
// in sorted order, BlankCheckChunkSize per NonBlankPaths statement, one
// statement after another; each statement issued adds one to
// stats.BlankCheckStatements, also when it fails. The first failure fails the
// pass.
func markNonBlankDeferredFiles(
	ctx context.Context,
	reader Reader,
	repoID string,
	hydrated []File,
	deferred map[int]bool,
	stats *ReadStats,
) error {
	paths := make([]string, 0, len(deferred))
	seen := make(map[string]struct{}, len(deferred))
	for position := range deferred {
		relativePath := hydrated[position].RelativePath
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}
	sort.Strings(paths)

	nonBlank := make(map[string]struct{}, len(paths))
	for start := 0; start < len(paths); start += BlankCheckChunkSize {
		chunk := paths[start:min(start+BlankCheckChunkSize, len(paths))]
		stats.BlankCheckStatements++
		nonBlankPaths, err := reader.NonBlankPaths(ctx, repoID, chunk)
		if err != nil {
			return fmt.Errorf("check artifact file content: %w", err)
		}
		for _, relativePath := range nonBlankPaths {
			nonBlank[relativePath] = struct{}{}
		}
	}
	for position := range deferred {
		_, ok := nonBlank[hydrated[position].RelativePath]
		deferred[position] = ok
	}
	return nil
}
