package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type codeCallReexportIndex struct {
	exportsByRepoPath map[string]map[string][]codeCallReexportEntry
}

type codeCallReexportEntry struct {
	exportedName string
	originalName string
	source       string
}

// buildCodeCallReexportIndex records static relative JavaScript-family
// re-exports by repository and barrel path so import resolution can take one
// bounded hop without modeling a full bundler.
func buildCodeCallReexportIndex(envelopes []facts.Envelope) codeCallReexportIndex {
	index := codeCallReexportIndex{
		exportsByRepoPath: make(map[string]map[string][]codeCallReexportEntry),
	}
	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}
		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		relativePath := payloadStr(env.Payload, "relative_path")
		rawPath := anyToString(fileData["path"])
		for _, item := range mapSlice(fileData["imports"]) {
			if strings.TrimSpace(anyToString(item["import_type"])) != "reexport" {
				continue
			}
			entry := codeCallReexportEntry{
				exportedName: strings.TrimSpace(anyToString(item["name"])),
				originalName: strings.TrimSpace(anyToString(item["original_name"])),
				source:       strings.TrimSpace(anyToString(item["source"])),
			}
			if entry.originalName == "" {
				entry.originalName = entry.exportedName
			}
			if entry.exportedName == "" || entry.source == "" || !strings.HasPrefix(entry.source, ".") {
				continue
			}
			for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
				index.append(repositoryID, pathKey, entry)
			}
		}
	}
	return index
}

func (i codeCallReexportIndex) append(repositoryID string, path string, entry codeCallReexportEntry) {
	repositoryID = strings.TrimSpace(repositoryID)
	path = normalizeCodeCallPath(path)
	if repositoryID == "" || path == "" {
		return
	}
	if _, ok := i.exportsByRepoPath[repositoryID]; !ok {
		i.exportsByRepoPath[repositoryID] = make(map[string][]codeCallReexportEntry)
	}
	for _, existing := range i.exportsByRepoPath[repositoryID][path] {
		if existing == entry {
			return
		}
	}
	i.exportsByRepoPath[repositoryID][path] = append(i.exportsByRepoPath[repositoryID][path], entry)
}

func (i codeCallReexportIndex) entries(repositoryID string, path string) []codeCallReexportEntry {
	repositoryID = strings.TrimSpace(repositoryID)
	path = normalizeCodeCallPath(path)
	if repositoryID == "" || path == "" {
		return nil
	}
	return i.exportsByRepoPath[repositoryID][path]
}

// resolveReexportedCrossFileCallee resolves caller import -> barrel file ->
// original module only for parser-proven static relative re-export metadata.
func resolveReexportedCrossFileCallee(
	index codeEntityIndex,
	reexportIndex codeCallReexportIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	language string,
	target codeCallImportedTarget,
) (string, string) {
	for _, reexportPath := range codeCallImportSourceCandidates(rawPath, relativePath, target.importSource, language) {
		for _, entry := range reexportIndex.entries(repositoryID, reexportPath) {
			originalName, ok := entry.originalNameFor(target.symbolName)
			if !ok {
				continue
			}
			for _, targetPath := range codeCallImportSourceCandidates(reexportPath, "", entry.source, language) {
				if entityID := index.uniqueNameByPath[targetPath][originalName]; entityID != "" {
					return entityID, index.entityFileByID[entityID]
				}
			}
		}
	}
	return "", ""
}

func (e codeCallReexportEntry) originalNameFor(importedName string) (string, bool) {
	importedName = strings.TrimSpace(importedName)
	if importedName == "" {
		return "", false
	}
	switch {
	case e.exportedName == "*":
		return importedName, true
	case e.exportedName == importedName && e.originalName != "":
		return e.originalName, true
	default:
		return "", false
	}
}
