// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/spf13/cobra"
)

// assistantGuidanceRoot is the project root resolved from --path or cwd by the
// command's pre-run; install/status/uninstall operate relative to it.
var assistantGuidanceRoot string

// assistantPlatformFilter restricts install/status/uninstall to a single
// platform id when non-empty.
var assistantPlatformFilter string

// assistantStatusVerify enables first-run ritual diagnostics in `assistant
// status` without changing the default status table.
var assistantStatusVerify bool

// assistantInstallVerify enables the same safe ritual diagnostics after
// `assistant install` successfully writes or refreshes guidance.
var assistantInstallVerify bool

// assistantCmd groups the project-scoped assistant guidance subcommands.
var assistantCmd = &cobra.Command{
	Use:   "assistant",
	Short: "Manage project-scoped Eshu guidance for AI assistants",
	Long: `Write, inspect, and remove project-scoped instructions that tell AI
assistants (Claude Code, Codex/AGENTS.md, Cursor) to prefer Eshu's bounded
MCP/API tools for graph-backed questions and to respect Eshu truth labels.

Guidance lives inside a clearly delimited managed block, so install, reinstall,
and uninstall never disturb other content in your instruction files.`,
}

func init() {
	rootCmd.AddCommand(assistantCmd)

	assistantCmd.PersistentFlags().StringVar(&assistantGuidanceRoot, "path", "",
		"Project root to operate on (defaults to the current directory)")
	assistantCmd.PersistentFlags().StringVar(&assistantPlatformFilter, "platform", "",
		"Restrict to one assistant: claude, codex, or cursor")

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install or update Eshu guidance for supported assistants",
		RunE:  runAssistantInstall,
	}
	installCmd.Flags().BoolVar(&assistantInstallVerify, "verify", false,
		"Run safe assistant ritual diagnostics after install")
	assistantCmd.AddCommand(installCmd)
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Report whether Eshu guidance is installed and current",
		RunE:  runAssistantStatus,
	}
	statusCmd.Flags().BoolVar(&assistantStatusVerify, "verify", false,
		"Include first-run assistant ritual diagnostics")
	assistantCmd.AddCommand(statusCmd)
	assistantCmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove the Eshu managed guidance block from instruction files",
		RunE:  runAssistantUninstall,
	})
}

// fileSystem abstracts the file operations the guidance flows need so tests can
// inject a temp dir and exercise preservation without touching a real repo.
type fileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	Stat(path string) (os.FileInfo, error)
}

// osFileSystem is the production fileSystem backed by the os package.
type osFileSystem struct{}

func (osFileSystem) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (osFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (osFileSystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (osFileSystem) Remove(path string) error                     { return os.Remove(path) }
func (osFileSystem) Stat(path string) (os.FileInfo, error)        { return os.Stat(path) }

// guidanceEngine performs install/status/uninstall against an injectable
// fileSystem rooted at a project directory.
type guidanceEngine struct {
	fs   fileSystem
	root string
}

// platformResult records the outcome of an install/status/uninstall action for
// one platform, used to render output and assert in tests.
type platformResult struct {
	platform assistantPlatform
	path     string
	status   blockStatus
	// changed reports whether the file content was modified by the action.
	changed bool
	// created reports whether the action created a new file.
	created bool
	// removed reports whether uninstall deleted a now-empty Eshu-created file.
	removed bool
}

// selectPlatforms returns the platforms to operate on, honoring the --platform
// filter. An unknown filter is an error so unsupported platforms are explicit.
func selectPlatforms(filter string) ([]assistantPlatform, error) {
	if filter == "" {
		return supportedPlatforms(), nil
	}
	p, ok := lookupPlatform(strings.ToLower(strings.TrimSpace(filter)))
	if !ok {
		return nil, fmt.Errorf("unsupported assistant platform %q (supported: claude, codex, cursor)", filter)
	}
	return []assistantPlatform{p}, nil
}

// resolveRoot returns the absolute project root from the --path flag or cwd.
func resolveRoot(path string) (string, error) {
	if path == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve current directory: %w", err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve project path %q: %w", path, err)
	}
	return abs, nil
}

// readFileOrEmpty returns the file content, or empty string when the file does
// not exist. Any other read error is returned.
func (e *guidanceEngine) readFileOrEmpty(path string) (string, bool, error) {
	data, err := e.fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), true, nil
}

// install writes or refreshes the managed block for each selected platform,
// preserving any pre-existing file content outside the managed block.
func (e *guidanceEngine) install(platforms []assistantPlatform) ([]platformResult, error) {
	results := make([]platformResult, 0, len(platforms))
	for _, p := range platforms {
		path := filepath.Join(e.root, p.relPath)
		existing, existed, err := e.readFileOrEmpty(path)
		if err != nil {
			return nil, err
		}
		body := guidanceBody(p)
		updated := upsertManagedBlock(existing, body)
		res := platformResult{platform: p, path: path, status: classifyBlock(updated, body)}
		if updated == existing {
			results = append(results, res)
			continue
		}
		// Ensure the parent directory exists (Cursor rules live under
		// .cursor/rules/). MkdirAll is a no-op when the directory already exists.
		if err := e.fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", path, err)
		}
		if err := e.fs.WriteFile(path, []byte(updated), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		res.changed = true
		res.created = !existed
		results = append(results, res)
	}
	return results, nil
}

// status reports the managed-block state for each selected platform without
// modifying any file.
func (e *guidanceEngine) status(platforms []assistantPlatform) ([]platformResult, error) {
	results := make([]platformResult, 0, len(platforms))
	for _, p := range platforms {
		path := filepath.Join(e.root, p.relPath)
		existing, _, err := e.readFileOrEmpty(path)
		if err != nil {
			return nil, err
		}
		results = append(results, platformResult{
			platform: p,
			path:     path,
			status:   classifyBlock(existing, guidanceBody(p)),
		})
	}
	return results, nil
}

// uninstall removes the managed block for each selected platform. It deletes a
// file only when that file becomes empty AND Eshu created it (the file is
// nothing but the managed block). Files with other content are preserved with
// just the block stripped; files Eshu did not create are never deleted.
func (e *guidanceEngine) uninstall(platforms []assistantPlatform) ([]platformResult, error) {
	results := make([]platformResult, 0, len(platforms))
	for _, p := range platforms {
		path := filepath.Join(e.root, p.relPath)
		existing, existed, err := e.readFileOrEmpty(path)
		if err != nil {
			return nil, err
		}
		res := platformResult{platform: p, path: path, status: blockAbsent}
		if !existed {
			results = append(results, res)
			continue
		}
		updated, removed := removeManagedBlock(existing)
		if !removed {
			results = append(results, res)
			continue
		}
		res.changed = true
		// Delete only a file that is now empty: that means it held nothing but
		// the Eshu block, so Eshu effectively owned it. Never delete a file that
		// still has user content.
		if strings.TrimSpace(updated) == "" {
			if err := e.fs.Remove(path); err != nil {
				return nil, fmt.Errorf("remove %s: %w", path, err)
			}
			res.removed = true
			results = append(results, res)
			continue
		}
		if err := e.fs.WriteFile(path, []byte(updated), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		results = append(results, res)
	}
	return results, nil
}

// newEngine builds a guidanceEngine from the resolved root and the production
// filesystem.
func newEngine(root string) *guidanceEngine {
	return &guidanceEngine{fs: osFileSystem{}, root: root}
}

func runAssistantInstall(cmd *cobra.Command, _ []string) error {
	root, err := resolveRoot(assistantGuidanceRoot)
	if err != nil {
		return err
	}
	platforms, err := selectPlatforms(assistantPlatformFilter)
	if err != nil {
		return err
	}
	results, err := newEngine(root).install(platforms)
	if err != nil {
		return err
	}
	return renderAssistantInstall(root, results, assistantInstallVerify)
}

func runAssistantStatus(cmd *cobra.Command, _ []string) error {
	root, err := resolveRoot(assistantGuidanceRoot)
	if err != nil {
		return err
	}
	platforms, err := selectPlatforms(assistantPlatformFilter)
	if err != nil {
		return err
	}
	results, err := newEngine(root).status(platforms)
	if err != nil {
		return err
	}
	return renderAssistantStatus(root, results, assistantStatusVerify)
}

func runAssistantUninstall(cmd *cobra.Command, _ []string) error {
	root, err := resolveRoot(assistantGuidanceRoot)
	if err != nil {
		return err
	}
	platforms, err := selectPlatforms(assistantPlatformFilter)
	if err != nil {
		return err
	}
	results, err := newEngine(root).uninstall(platforms)
	if err != nil {
		return err
	}
	for _, r := range results {
		rel := relOrPath(root, r.path)
		switch {
		case r.removed:
			printSuccess(fmt.Sprintf("%s: removed Eshu-created %s", r.platform.label, rel))
		case r.changed:
			printSuccess(fmt.Sprintf("%s: removed Eshu guidance block from %s", r.platform.label, rel))
		default:
			fmt.Printf("- %s: no Eshu guidance block in %s\n", r.platform.label, rel)
		}
	}
	return nil
}

// renderInstall prints per-platform install outcomes followed by `git add`
// hints for the commit-worthy files that changed.
func renderInstall(_ *cobra.Command, root string, results []platformResult) {
	var addHints []string
	for _, r := range results {
		rel := relOrPath(root, r.path)
		switch {
		case r.created:
			printSuccess(fmt.Sprintf("%s: created %s with Eshu guidance", r.platform.label, rel))
		case r.changed:
			printSuccess(fmt.Sprintf("%s: updated Eshu guidance in %s", r.platform.label, rel))
		default:
			fmt.Printf("- %s: %s already current (%s)\n", r.platform.label, rel, managedBlockSummary(r.status))
		}
		if r.changed && r.platform.commit {
			addHints = append(addHints, rel)
		}
	}
	if len(addHints) == 0 {
		return
	}
	sort.Strings(addHints)
	fmt.Println("\nCommit the guidance so teammates and CI agents share it:")
	for _, h := range addHints {
		fmt.Printf("  git add %s\n", h)
	}
}

// renderAssistantInstall prints install outcomes and, when verify is set,
// appends the same local ritual diagnostics used by status --verify.
func renderAssistantInstall(root string, results []platformResult, verify bool) error {
	renderInstall(nil, root, results)
	if !verify {
		return nil
	}
	report, err := assistantRitualVerification(results)
	if err != nil {
		return err
	}
	fmt.Print(renderAssistantVerifyReport(report))
	if !report.allOK() {
		return fmt.Errorf("assistant ritual verification failed")
	}
	return nil
}

// renderAssistantStatus prints the normal status table and, when verify is set,
// appends first-run diagnostics that prove the ritual guidance and local MCP
// tool surface are visible without making a broad graph read.
func renderAssistantStatus(root string, results []platformResult, verify bool) error {
	headers := []string{"Platform", "File", "Guidance"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rel := relOrPath(root, r.path)
		rows = append(rows, []string{r.platform.label, rel, managedBlockSummary(r.status)})
	}
	printTable(headers, rows)
	if !verify {
		return nil
	}
	report, err := assistantRitualVerification(results)
	if err != nil {
		return err
	}
	fmt.Print(renderAssistantVerifyReport(report))
	if !report.allOK() {
		return fmt.Errorf("assistant ritual verification failed")
	}
	return nil
}

// assistantRitualVerification builds the verification report for
// `assistant status --verify`. It checks committed guidance state first, then
// reuses the local stdio MCP setup verification seam for safe tool visibility.
func assistantRitualVerification(results []platformResult) (verifyReport, error) {
	report := verifyReport{
		Stages: []stageResult{assistantGuidanceStage(results)},
	}
	p, err := resolvePlatform("generic")
	if err != nil {
		return verifyReport{}, err
	}
	snippet, err := renderSetupSnippet(p, mcpSetupRequest{Mode: modeLocalStdio})
	if err != nil {
		return verifyReport{}, err
	}
	mcpReport := runVerification(snippet, mcp.ReadOnlyTools, nil, nil)
	report.Stages = append(report.Stages, mcpReport.Stages...)
	return report, nil
}

func assistantGuidanceStage(results []platformResult) stageResult {
	current := 0
	for _, r := range results {
		if r.status == blockCurrent {
			current++
		}
	}
	ok := len(results) > 0 && current == len(results)
	return stageResult{
		Stage:  verifyStage("guidance installed"),
		OK:     ok,
		Detail: fmt.Sprintf("%d/%d platform guidance blocks current", current, len(results)),
	}
}

func renderAssistantVerifyReport(report verifyReport) string {
	var b strings.Builder
	b.WriteString("\nAssistant ritual verification\n")
	for _, s := range report.Stages {
		marker := "[ok]"
		switch {
		case s.Skipped:
			marker = "[--]"
		case !s.OK:
			marker = "[!!]"
		}
		fmt.Fprintf(&b, "  %s %s: %s\n", marker, s.Stage, s.Detail)
	}
	return b.String()
}

// relOrPath returns path relative to root for display, or the absolute path if
// it cannot be made relative.
func relOrPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
