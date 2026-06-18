package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func (s NativeRepositorySnapshotter) collectSCIPLanguageGroupFiles(
	ctx context.Context,
	repoPath string,
	groups []parser.SCIPLanguageFileGroup,
	indexer scipProjectIndexer,
	resultParser scipResultParser,
) (map[string]map[string]any, bool, error) {
	scipFiles := make(map[string]map[string]any)
	usedAny := false
	for _, group := range groups {
		language := group.Language
		if !indexer.IsAvailable(language) {
			s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultBinaryMissing)
			s.logSCIPSnapshotFallback(ctx, language, scipSnapshotResultBinaryMissing)
			continue
		}

		outputDir, err := os.MkdirTemp("", "eshu-scip-*")
		if err != nil {
			return nil, false, err
		}
		indexPath, runErr := indexer.Run(ctx, scipProjectRoot(repoPath, group.Files), language, outputDir)
		if runErr != nil {
			_ = os.RemoveAll(outputDir)
			s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultIndexerFailed)
			s.logSCIPSnapshotFallback(ctx, language, scipSnapshotResultIndexerFailed)
			continue
		}
		result, parseErr := resultParser.Parse(indexPath, scipProjectRoot(repoPath, group.Files))
		_ = os.RemoveAll(outputDir)
		if parseErr != nil {
			s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultParseFailed)
			s.logSCIPSnapshotFallback(ctx, language, scipSnapshotResultParseFailed)
			continue
		}
		if len(result.Files) == 0 {
			s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultEmpty)
			continue
		}
		for path, payload := range result.Files {
			scipFiles[path] = payload
		}
		usedAny = true
		s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultUsed)
	}
	return scipFiles, usedAny, nil
}

func scipProjectRoot(repoPath string, files []string) string {
	repoRoot := filepath.Clean(repoPath)
	if len(files) == 0 {
		return repoRoot
	}
	common := filepath.Dir(filepath.Clean(files[0]))
	for _, file := range files[1:] {
		common = commonPath(common, filepath.Dir(filepath.Clean(file)))
	}
	if common == "" {
		return repoRoot
	}
	rel, err := filepath.Rel(repoRoot, common)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return repoRoot
	}
	return common
}

func commonPath(left string, right string) string {
	leftParts := splitCleanPath(left)
	rightParts := splitCleanPath(right)
	limit := min(len(leftParts), len(rightParts))
	commonParts := make([]string, 0, limit)
	for index := 0; index < limit; index++ {
		if leftParts[index] != rightParts[index] {
			break
		}
		commonParts = append(commonParts, leftParts[index])
	}
	if len(commonParts) == 0 {
		return string(filepath.Separator)
	}
	return filepath.Join(commonParts...)
}

func splitCleanPath(path string) []string {
	volume := filepath.VolumeName(path)
	trimmed := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	parts := strings.Split(trimmed, string(filepath.Separator))
	if volume != "" {
		parts[0] = volume + string(filepath.Separator) + parts[0]
	} else if filepath.IsAbs(path) && len(parts) > 0 {
		parts[0] = string(filepath.Separator) + parts[0]
	}
	return parts
}
