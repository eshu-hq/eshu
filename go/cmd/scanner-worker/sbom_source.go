package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/metrics"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
)

const repositorySBOMSourceTool = "eshu_repository_manifest_sbom 0.1.0"

var errNoSBOMTargets = errors.New("no SBOM generation targets configured")

type sbomTargetConfig struct {
	ScopeID       string `json:"scope_id"`
	RootPath      string `json:"root_path"`
	SubjectDigest string `json:"subject_digest"`
	SourceTool    string `json:"source_tool"`
}

type repositorySBOMSource struct {
	targets map[string]sbomTargetConfig
}

func newRepositorySBOMSource(targets []sbomTargetConfig) (*repositorySBOMSource, error) {
	if len(targets) == 0 {
		return nil, errNoSBOMTargets
	}
	byScope := make(map[string]sbomTargetConfig, len(targets))
	for i, target := range targets {
		validated, err := target.validate()
		if err != nil {
			return nil, fmt.Errorf("sbom target %d: %w", i, err)
		}
		if _, exists := byScope[validated.ScopeID]; exists {
			return nil, fmt.Errorf("duplicate SBOM target scope_id")
		}
		byScope[validated.ScopeID] = validated
	}
	return &repositorySBOMSource{targets: byScope}, nil
}

func (s *repositorySBOMSource) Collect(
	ctx context.Context,
	input scannerworker.ClaimInput,
) (sbomgenerator.Inventory, error) {
	target, ok := s.targets[strings.TrimSpace(input.Target.ScopeID)]
	if !ok {
		return sbomgenerator.Inventory{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassUnsupportedTarget,
			scannerworker.ResourceUsage{},
			sbomgenerator.ErrUnsupportedTarget,
		)
	}
	reader := repositoryManifestReader{
		root:            target.RootPath,
		remainingBytes:  input.Limits.MaxInputBytes,
		maxFiles:        input.Limits.MaxFiles,
		startCPUSeconds: currentScannerCPUSeconds(),
	}
	components, err := reader.collect(ctx)
	usage := reader.usage()
	if err != nil {
		return sbomgenerator.Inventory{}, err
	}
	tool := strings.TrimSpace(target.SourceTool)
	if tool == "" {
		tool = repositorySBOMSourceTool
	}
	return sbomgenerator.Inventory{
		SubjectDigest: target.SubjectDigest,
		SourceTool:    tool,
		FileCount:     reader.filesSeen,
		InputBytes:    reader.inputBytes,
		Components:    components,
		ResourceUsage: usage,
	}, nil
}

func (t sbomTargetConfig) validate() (sbomTargetConfig, error) {
	t.ScopeID = strings.TrimSpace(t.ScopeID)
	t.RootPath = strings.TrimSpace(t.RootPath)
	t.SubjectDigest = strings.TrimSpace(t.SubjectDigest)
	t.SourceTool = strings.TrimSpace(t.SourceTool)
	if t.ScopeID == "" {
		return sbomTargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if t.RootPath == "" {
		return sbomTargetConfig{}, fmt.Errorf("root_path is required")
	}
	return t, nil
}

type repositoryManifestReader struct {
	root            string
	remainingBytes  int64
	maxFiles        int64
	filesSeen       int64
	inputBytes      int64
	peakBytes       int64
	startCPUSeconds float64
}

func (r *repositoryManifestReader) collect(ctx context.Context) ([]sbomgenerator.Component, error) {
	root, err := secureRepositoryRoot(r.root)
	if err != nil {
		return nil, scannerworker.NewRetryableAnalyzerFailure(
			scannerworker.FailureClassTargetUnavailable,
			r.usage(),
			err,
		)
	}
	components := make([]sbomgenerator.Component, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return scannerworker.NewRetryableAnalyzerFailure(
				scannerworker.FailureClassTargetUnavailable,
				r.usage(),
				walkErr,
			)
		}
		if err := ctx.Err(); err != nil {
			return scannerworker.NewRetryableAnalyzerFailure(
				scannerworker.FailureClassSourceUnavailable,
				r.usage(),
				err,
			)
		}
		if entry.IsDir() {
			if shouldSkipRepositoryDir(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		r.filesSeen++
		if r.filesSeen > r.maxFiles {
			return scannerworker.NewTerminalAnalyzerFailure(
				scannerworker.FailureClassFileLimitExceeded,
				r.usage(),
				nil,
			)
		}
		if !isSupportedManifestName(entry.Name()) {
			return nil
		}
		body, err := r.readManifest(path)
		if err != nil {
			return err
		}
		parsed, err := parseRepositoryManifest(entry.Name(), body)
		if err != nil {
			return scannerworker.NewTerminalAnalyzerFailure(
				scannerworker.FailureClassAnalyzerFailed,
				r.usage(),
				err,
			)
		}
		components = append(components, parsed...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return components, nil
}

func (r *repositoryManifestReader) readManifest(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, scannerworker.NewRetryableAnalyzerFailure(
			scannerworker.FailureClassTargetUnavailable,
			r.usage(),
			err,
		)
	}
	if info.Size() > r.remainingBytes {
		return nil, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassInputLimitExceeded,
			r.usage(),
			nil,
		)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, scannerworker.NewRetryableAnalyzerFailure(
			scannerworker.FailureClassTargetUnavailable,
			r.usage(),
			err,
		)
	}
	if int64(len(body)) > r.remainingBytes {
		return nil, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassInputLimitExceeded,
			r.usage(),
			nil,
		)
	}
	r.remainingBytes -= int64(len(body))
	r.inputBytes += int64(len(body))
	if int64(len(body)) > r.peakBytes {
		r.peakBytes = int64(len(body))
	}
	return body, nil
}

func (r repositoryManifestReader) usage() scannerworker.ResourceUsage {
	cpuSeconds := currentScannerCPUSeconds() - r.startCPUSeconds
	if cpuSeconds < 0 {
		cpuSeconds = 0
	}
	return scannerworker.ResourceUsage{
		CPUSeconds:      cpuSeconds,
		PeakMemoryBytes: r.peakBytes,
	}
}

func secureRepositoryRoot(root string) (string, error) {
	cleanRoot := filepath.Clean(root)
	if cleanRoot == "." || cleanRoot == "" {
		return "", fmt.Errorf("repository root path is required")
	}
	absRoot, err := filepath.Abs(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root")
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root")
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return "", fmt.Errorf("stat repository root")
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root is not a directory")
	}
	return realRoot, nil
}

func shouldSkipRepositoryDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".terraform", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func isSupportedManifestName(name string) bool {
	switch name {
	case "package-lock.json", "npm-shrinkwrap.json", "go.mod":
		return true
	default:
		return false
	}
}

func parseRepositoryManifest(name string, body []byte) ([]sbomgenerator.Component, error) {
	switch name {
	case "package-lock.json", "npm-shrinkwrap.json":
		return parseNPMLockComponents(body)
	case "go.mod":
		return parseGoModComponents(body), nil
	default:
		return nil, nil
	}
}

type npmLockFile struct {
	Packages     map[string]npmLockPackage `json:"packages"`
	Dependencies map[string]npmLockPackage `json:"dependencies"`
}

type npmLockPackage struct {
	Name         string                    `json:"name"`
	Version      string                    `json:"version"`
	Dependencies map[string]npmLockPackage `json:"dependencies"`
}

func parseNPMLockComponents(body []byte) ([]sbomgenerator.Component, error) {
	var lock npmLockFile
	if err := json.Unmarshal(body, &lock); err != nil {
		return nil, err
	}
	components := make([]sbomgenerator.Component, 0, len(lock.Packages)+len(lock.Dependencies))
	for path, pkg := range lock.Packages {
		if strings.TrimSpace(path) == "" {
			continue
		}
		name := firstNonBlank(pkg.Name, npmNameFromPackagePath(path))
		if strings.TrimSpace(name) == "" || strings.TrimSpace(pkg.Version) == "" {
			continue
		}
		components = append(components, sbomgenerator.Component{
			Name:    name,
			Version: pkg.Version,
			Type:    "library",
		})
	}
	appendNPMDependencies(&components, lock.Dependencies)
	return components, nil
}

func appendNPMDependencies(components *[]sbomgenerator.Component, deps map[string]npmLockPackage) {
	for name, dep := range deps {
		if strings.TrimSpace(name) != "" && strings.TrimSpace(dep.Version) != "" {
			*components = append(*components, sbomgenerator.Component{
				Name:    name,
				Version: dep.Version,
				Type:    "library",
			})
		}
		appendNPMDependencies(components, dep.Dependencies)
	}
}

func npmNameFromPackagePath(path string) string {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	index := strings.LastIndex(normalized, "node_modules/")
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(normalized[index+len("node_modules/"):])
}

func parseGoModComponents(body []byte) []sbomgenerator.Component {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	components := make([]sbomgenerator.Component, 0)
	inRequireBlock := false
	for scanner.Scan() {
		line := stripGoComment(scanner.Text())
		if line == "" {
			continue
		}
		if inRequireBlock {
			if line == ")" {
				inRequireBlock = false
				continue
			}
			if component, ok := goRequireComponent(line); ok {
				components = append(components, component)
			}
			continue
		}
		if strings.TrimSpace(line) == "require (" {
			inRequireBlock = true
			continue
		}
		if rest, ok := strings.CutPrefix(line, "require "); ok {
			if component, ok := goRequireComponent(rest); ok {
				components = append(components, component)
			}
		}
	}
	return components
}

func goRequireComponent(line string) (sbomgenerator.Component, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return sbomgenerator.Component{}, false
	}
	return sbomgenerator.Component{
		Name:    fields[0],
		Version: fields[1],
		Type:    "library",
	}, true
}

func stripGoComment(line string) string {
	if before, _, ok := strings.Cut(line, "//"); ok {
		line = before
	}
	return strings.TrimSpace(line)
}

func currentScannerCPUSeconds() float64 {
	samples := []metrics.Sample{{Name: "/cpu/classes/user:cpu-seconds"}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindFloat64 {
		return 0
	}
	return samples[0].Value.Float64()
}
