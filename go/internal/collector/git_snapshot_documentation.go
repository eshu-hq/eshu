package collector

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func partitionNativeSnapshotFiles(files []string, registry parser.Registry) ([]string, []string) {
	parserFiles := make([]string, 0, len(files))
	documentationFiles := []string{}
	for _, filePath := range files {
		if isGitDocumentationPath(filePath) {
			if isParserPreferredDocumentationPath(filePath, registry) {
				parserFiles = append(parserFiles, filePath)
				continue
			}
			documentationFiles = append(documentationFiles, filePath)
			continue
		}
		parserFiles = append(parserFiles, filePath)
	}
	return parserFiles, documentationFiles
}

func isParserPreferredDocumentationPath(filePath string, registry parser.Registry) bool {
	if strings.ToLower(filepath.Ext(filePath)) != ".txt" {
		return false
	}
	_, ok := registry.LookupByPath(filePath)
	return ok
}

func documentationFileMetasForPaths(repoPath string, paths []string, commitSHA string) []ContentFileMeta {
	metas := make([]ContentFileMeta, 0, len(paths))
	for _, filePath := range paths {
		relativePath, err := filepath.Rel(repoPath, filePath)
		if err != nil {
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		format, ok := gitDocumentationFormatForPath(relativePath)
		if !ok {
			continue
		}
		digest, ok := documentationDigestForFile(filePath)
		if !ok {
			continue
		}
		metas = append(metas, ContentFileMeta{
			RelativePath: relativePath,
			Digest:       digest,
			Language:     format.language,
			ArtifactType: "documentation",
			CommitSHA:    commitSHA,
		})
	}
	return metas
}

func documentationDigestForFile(filePath string) (string, bool) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()
	hash := sha1.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", false
	}
	return hex.EncodeToString(hash.Sum(nil)), true
}
