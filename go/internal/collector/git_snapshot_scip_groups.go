package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func (s NativeRepositorySnapshotter) collectSCIPLanguageGroupFiles(
	ctx context.Context,
	repoPath string,
	groups []parser.SCIPLanguageFileGroup,
	indexer scipProjectIndexer,
	resultParser scipResultParser,
) (map[string]map[string]any, bool, error) {
	subtrees := scipLanguageSubtrees(repoPath, groups)
	if len(subtrees) == 0 {
		return map[string]map[string]any{}, false, nil
	}
	workerCount := min(max(1, s.scipConfig().Workers), len(subtrees))
	if workerCount <= 1 {
		return s.collectSCIPLanguageGroupFilesSequential(ctx, subtrees, indexer, resultParser)
	}
	return s.collectSCIPLanguageGroupFilesConcurrent(ctx, subtrees, indexer, resultParser, workerCount)
}

func (s NativeRepositorySnapshotter) collectSCIPLanguageGroupFilesSequential(
	ctx context.Context,
	subtrees []scipLanguageSubtree,
	indexer scipProjectIndexer,
	resultParser scipResultParser,
) (map[string]map[string]any, bool, error) {
	scipFiles := make(map[string]map[string]any)
	usedAny := false
	for _, subtree := range subtrees {
		result, err := s.collectSCIPLanguageSubtreeFiles(ctx, subtree, indexer, resultParser)
		if err != nil {
			return nil, false, err
		}
		if !result.used {
			continue
		}
		for path, payload := range result.files {
			scipFiles[path] = payload
		}
		usedAny = true
	}
	return scipFiles, usedAny, nil
}

func (s NativeRepositorySnapshotter) collectSCIPLanguageGroupFilesConcurrent(
	ctx context.Context,
	subtrees []scipLanguageSubtree,
	indexer scipProjectIndexer,
	resultParser scipResultParser,
	workerCount int,
) (map[string]map[string]any, bool, error) {
	jobs := make(chan scipSubtreeFilesJob, len(subtrees))
	results := make(chan scipSubtreeFilesResult, len(subtrees))

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				result, err := s.collectSCIPLanguageSubtreeFiles(ctx, job.subtree, indexer, resultParser)
				result.index = job.index
				result.err = err
				results <- result
			}
		}()
	}
	for index, subtree := range subtrees {
		jobs <- scipSubtreeFilesJob{index: index, subtree: subtree}
	}
	close(jobs)
	go func() {
		wg.Wait()
		close(results)
	}()

	resultsByIndex := make([]scipSubtreeFilesResult, len(subtrees))
	for result := range results {
		resultsByIndex[result.index] = result
	}

	scipFiles := make(map[string]map[string]any)
	usedAny := false
	var firstErr error
	for _, result := range resultsByIndex {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
		}
		if !result.used {
			continue
		}
		for path, payload := range result.files {
			scipFiles[path] = payload
		}
		usedAny = true
	}
	if firstErr != nil {
		return nil, false, firstErr
	}
	return scipFiles, usedAny, nil
}

type scipSubtreeFilesJob struct {
	index   int
	subtree scipLanguageSubtree
}

type scipSubtreeFilesResult struct {
	index int
	files map[string]map[string]any
	used  bool
	err   error
}

func (s NativeRepositorySnapshotter) collectSCIPLanguageSubtreeFiles(
	ctx context.Context,
	subtree scipLanguageSubtree,
	indexer scipProjectIndexer,
	resultParser scipResultParser,
) (scipSubtreeFilesResult, error) {
	language := subtree.Language
	if !indexer.IsAvailable(language) {
		s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultBinaryMissing)
		s.logSCIPSnapshotFallback(ctx, language, scipSnapshotResultBinaryMissing)
		return scipSubtreeFilesResult{}, nil
	}

	outputDir, err := os.MkdirTemp("", "eshu-scip-*")
	if err != nil {
		return scipSubtreeFilesResult{}, err
	}
	release, err := s.scipConfig().acquireProcess(ctx)
	if err != nil {
		_ = os.RemoveAll(outputDir)
		return scipSubtreeFilesResult{}, err
	}
	indexPath, runErr := indexer.Run(ctx, subtree.Root, language, outputDir)
	release()
	if runErr != nil {
		_ = os.RemoveAll(outputDir)
		s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultIndexerFailed)
		s.logSCIPSnapshotFallback(ctx, language, scipSnapshotResultIndexerFailed)
		return scipSubtreeFilesResult{}, nil
	}
	result, parseErr := resultParser.Parse(indexPath, subtree.Root)
	_ = os.RemoveAll(outputDir)
	if parseErr != nil {
		s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultParseFailed)
		s.logSCIPSnapshotFallback(ctx, language, scipSnapshotResultParseFailed)
		return scipSubtreeFilesResult{}, nil
	}
	if len(result.Files) == 0 {
		s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultEmpty)
		return scipSubtreeFilesResult{}, nil
	}
	s.recordSCIPSnapshotAttempt(ctx, language, scipSnapshotResultUsed)
	return scipSubtreeFilesResult{files: result.Files, used: true}, nil
}

type scipLanguageSubtree struct {
	Language string
	Root     string
}

func scipLanguageSubtrees(repoPath string, groups []parser.SCIPLanguageFileGroup) []scipLanguageSubtree {
	subtrees := make([]scipLanguageSubtree, 0, len(groups))
	for _, group := range groups {
		groupRoot := scipProjectRoot(repoPath, group.Files)
		roots := make(map[string]struct{})
		rootOrder := make([]string, 0, len(group.Files))
		for _, file := range group.Files {
			root := scipPackageRoot(repoPath, group.Language, file)
			if root == "" {
				root = groupRoot
			}
			if _, ok := roots[root]; !ok {
				rootOrder = append(rootOrder, root)
				roots[root] = struct{}{}
			}
		}
		for _, root := range rootOrder {
			subtrees = append(subtrees, scipLanguageSubtree{
				Language: group.Language,
				Root:     root,
			})
		}
	}
	return subtrees
}

func scipPackageRoot(repoPath string, language string, file string) string {
	markers := scipPackageRootMarkers(language)
	if len(markers) == 0 {
		return ""
	}
	repoRoot := filepath.Clean(repoPath)
	dir := filepath.Dir(filepath.Clean(file))
	for {
		if !isPathWithin(repoRoot, dir) {
			return ""
		}
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		if dir == repoRoot {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func scipPackageRootMarkers(language string) []string {
	switch language {
	case "python":
		return []string{"pyproject.toml", "setup.cfg", "setup.py", "requirements.txt", "Pipfile", "poetry.lock"}
	case "typescript", "javascript":
		return []string{"package.json", "tsconfig.json", "jsconfig.json"}
	case "go":
		return []string{"go.mod", "go.work"}
	case "rust":
		return []string{"Cargo.toml"}
	case "java":
		return []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}
	case "cpp", "c":
		return []string{"compile_commands.json", "CMakeLists.txt"}
	default:
		return nil
	}
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

func isPathWithin(root string, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
