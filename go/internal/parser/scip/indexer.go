// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scip

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type languageConfig struct {
	Language    string
	Binary      string
	InstallHint string
}

// LanguageFileGroup captures selected files for one SCIP-capable language.
type LanguageFileGroup struct {
	Language string
	Files    []string
}

var extensionConfigs = map[string]languageConfig{
	".c":     {Language: "c", Binary: "scip-clang", InstallHint: "brew install llvm"},
	".cpp":   {Language: "cpp", Binary: "scip-clang", InstallHint: "brew install llvm"},
	".go":    {Language: "go", Binary: "scip-go", InstallHint: "go install github.com/sourcegraph/scip-go/...@latest"},
	".h":     {Language: "cpp", Binary: "scip-clang", InstallHint: "brew install llvm"},
	".hpp":   {Language: "cpp", Binary: "scip-clang", InstallHint: "brew install llvm"},
	".ipynb": {Language: "python", Binary: "scip-python", InstallHint: "pip install scip-python"},
	".java":  {Language: "java", Binary: "scip-java", InstallHint: "see https://github.com/sourcegraph/scip-java"},
	".js":    {Language: "javascript", Binary: "scip-typescript", InstallHint: "npm install -g @sourcegraph/scip-typescript"},
	".jsx":   {Language: "javascript", Binary: "scip-typescript", InstallHint: "npm install -g @sourcegraph/scip-typescript"},
	".py":    {Language: "python", Binary: "scip-python", InstallHint: "pip install scip-python"},
	".rs":    {Language: "rust", Binary: "scip-rust", InstallHint: "cargo install scip-rust"},
	".ts":    {Language: "typescript", Binary: "scip-typescript", InstallHint: "npm install -g @sourcegraph/scip-typescript"},
	".tsx":   {Language: "typescript", Binary: "scip-typescript", InstallHint: "npm install -g @sourcegraph/scip-typescript"},
}

var languagePriority = []string{
	"python",
	"typescript",
	"javascript",
	"go",
	"rust",
	"java",
	"cpp",
	"c",
}

// Indexer runs an external scip-* CLI and returns the generated index path.
type Indexer struct {
	LookPath   func(string) (string, error)
	RunCommand func(context.Context, []string, string) error
	Timeout    time.Duration
}

// DetectProjectLanguage returns the dominant SCIP-capable language across
// the provided file paths, restricted to the allowed set.
func DetectProjectLanguage(paths []string, allowed []string) string {
	allowedSet := allowedLanguageSet(allowed)
	grouped := filesByLanguage(paths, allowedSet)
	bestLanguage := ""
	bestCount := 0
	for _, language := range languagePriority {
		count := len(grouped[language])
		if count > bestCount {
			bestLanguage = language
			bestCount = count
		}
	}
	return bestLanguage
}

// DetectProjectLanguageGroups returns SCIP-capable selected files grouped
// by language in deterministic priority order.
func DetectProjectLanguageGroups(paths []string, allowed []string) []LanguageFileGroup {
	allowedSet := allowedLanguageSet(allowed)
	if len(allowedSet) == 0 {
		return nil
	}
	grouped := filesByLanguage(paths, allowedSet)

	groups := make([]LanguageFileGroup, 0, len(grouped))
	for _, language := range languagePriority {
		if _, ok := allowedSet[language]; !ok {
			continue
		}
		files := grouped[language]
		if len(files) == 0 {
			continue
		}
		groups = append(groups, LanguageFileGroup{
			Language: language,
			Files:    append([]string(nil), files...),
		})
	}
	return groups
}

func allowedLanguageSet(allowed []string) map[string]struct{} {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, language := range allowed {
		normalized := strings.TrimSpace(strings.ToLower(language))
		if normalized != "" {
			allowedSet[normalized] = struct{}{}
		}
	}
	return allowedSet
}

func filesByLanguage(paths []string, allowedSet map[string]struct{}) map[string][]string {
	byLanguage := make(map[string][]string)
	for _, path := range paths {
		config, ok := extensionConfigs[strings.ToLower(filepath.Ext(path))]
		if !ok {
			continue
		}
		if _, ok := allowedSet[config.Language]; !ok {
			continue
		}
		byLanguage[config.Language] = append(byLanguage[config.Language], path)
	}
	return byLanguage
}

// IsAvailable reports whether the external scip-* binary is installed for the language.
func (i Indexer) IsAvailable(language string) bool {
	binary, _, ok := binaryForLanguage(language)
	if !ok {
		return false
	}
	_, err := i.lookPath()(binary)
	return err == nil
}

// Run executes the language-appropriate scip-* binary and returns the resulting
// index.scip path.
func (i Indexer) Run(
	ctx context.Context,
	projectPath string,
	language string,
	outputDir string,
) (string, error) {
	binary, installHint, ok := binaryForLanguage(language)
	if !ok {
		return "", fmt.Errorf("unsupported SCIP language %q", language)
	}
	binaryPath, err := i.lookPath()(binary)
	if err != nil {
		return "", fmt.Errorf("resolve SCIP binary %q: %w (install hint: %s)", binary, err, installHint)
	}

	outputPath := filepath.Join(outputDir, "index.scip")
	command, err := buildCommand(language, binaryPath, outputPath)
	if err != nil {
		return "", err
	}

	timeout := i.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := i.runCommand()(runCtx, command, projectPath); err != nil {
		return "", fmt.Errorf("run SCIP indexer for %q: %w", language, err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		return "", fmt.Errorf("SCIP indexer did not produce %q: %w", outputPath, err)
	}
	return outputPath, nil
}

func binaryForLanguage(language string) (string, string, bool) {
	normalized := strings.TrimSpace(strings.ToLower(language))
	for _, config := range extensionConfigs {
		if config.Language == normalized {
			return config.Binary, config.InstallHint, true
		}
	}
	return "", "", false
}

func buildCommand(language string, binary string, outputPath string) ([]string, error) {
	switch strings.TrimSpace(strings.ToLower(language)) {
	case "python":
		return []string{binary, "index", ".", "--output", outputPath}, nil
	case "typescript", "javascript":
		return []string{binary, "index", "--output", outputPath}, nil
	case "go":
		return []string{binary, "--output", outputPath}, nil
	case "rust", "java":
		return []string{binary, "index", "--output", outputPath}, nil
	case "cpp", "c":
		return []string{binary, "--index-output-path=" + outputPath}, nil
	default:
		return nil, fmt.Errorf("unsupported SCIP language %q", language)
	}
}

func (i Indexer) lookPath() func(string) (string, error) {
	if i.LookPath != nil {
		return i.LookPath
	}
	return exec.LookPath
}

func (i Indexer) runCommand() func(context.Context, []string, string) error {
	if i.RunCommand != nil {
		return i.RunCommand
	}
	return func(ctx context.Context, command []string, cwd string) error {
		cmd := exec.CommandContext(ctx, command[0], command[1:]...) // #nosec G204 -- runs a fixed indexer binary with internally-constructed arguments
		cmd.Dir = cwd
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
}
