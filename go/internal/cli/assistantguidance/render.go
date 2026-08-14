// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package assistantguidance

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// writef writes formatted output and drops the write error, matching the
// fmt.Printf call sites this rendering was extracted from: a failed write to
// the operator's terminal is not a command failure. Routing every write through
// one helper keeps that decision in a single reviewable place.
func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// writeSuccess renders a success line in the CLI's shared "OK <msg>" shape.
// It is a deliberate copy of go/cmd/eshu's printSuccess: cmd/eshu is
// `package main`, so this package cannot import that helper. The literal must
// stay byte-identical to it.
func writeSuccess(w io.Writer, msg string) {
	writef(w, "OK %s\n", msg)
}

// writeTable renders the shared CLI table. Like writeSuccess it mirrors
// go/cmd/eshu's printTable exactly -- same padding, same 40-dash rule -- because
// that helper is unimportable from here.
func writeTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(headers, "\t"))
	_, _ = fmt.Fprintln(tw, strings.Repeat("-", 40))
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

// RelOrPath returns path relative to root for display, or the absolute path if
// it cannot be made relative.
func RelOrPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

// renderInstallResults prints per-platform install outcomes followed by
// `git add` hints for the commit-worthy files that changed.
func renderInstallResults(w io.Writer, root string, results []Result) {
	var addHints []string
	for _, r := range results {
		rel := RelOrPath(root, r.Path)
		switch {
		case r.Created:
			writeSuccess(w, fmt.Sprintf("%s: created %s with Eshu guidance", r.Platform.Label, rel))
		case r.Changed:
			writeSuccess(w, fmt.Sprintf("%s: updated Eshu guidance in %s", r.Platform.Label, rel))
		default:
			writef(w, "- %s: %s already current (%s)\n", r.Platform.Label, rel, BlockSummary(r.Status))
		}
		if r.Changed && r.Platform.Commit {
			addHints = append(addHints, rel)
		}
	}
	if len(addHints) == 0 {
		return
	}
	sort.Strings(addHints)
	writef(w, "\nCommit the guidance so teammates and CI agents share it:\n")
	for _, h := range addHints {
		writef(w, "  git add %s\n", h)
	}
}

// RenderInstall prints install outcomes and, when verify is set, appends the
// same local ritual diagnostics used by status --verify. It returns an error
// when verification ran and did not pass, so the caller can map that to the
// command's exit code.
func RenderInstall(w io.Writer, root string, results []Result, verify bool) error {
	renderInstallResults(w, root, results)
	return renderVerification(w, results, verify)
}

// RenderStatus prints the normal status table and, when verify is set, appends
// first-run diagnostics that prove the ritual guidance and local MCP tool
// surface are visible without making a broad graph read.
func RenderStatus(w io.Writer, root string, results []Result, verify bool) error {
	headers := []string{"Platform", "File", "Guidance"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{r.Platform.Label, RelOrPath(root, r.Path), BlockSummary(r.Status)})
	}
	writeTable(w, headers, rows)
	return renderVerification(w, results, verify)
}

// RenderUninstall prints per-platform uninstall outcomes. Uninstall has no
// verification pass, so it cannot fail on rendering and returns nothing.
func RenderUninstall(w io.Writer, root string, results []Result) {
	for _, r := range results {
		rel := RelOrPath(root, r.Path)
		switch {
		case r.Removed:
			writeSuccess(w, fmt.Sprintf("%s: removed Eshu-created %s", r.Platform.Label, rel))
		case r.Changed:
			writeSuccess(w, fmt.Sprintf("%s: removed Eshu guidance block from %s", r.Platform.Label, rel))
		default:
			writef(w, "- %s: no Eshu guidance block in %s\n", r.Platform.Label, rel)
		}
	}
}

// renderVerification is the shared --verify tail for install and status: run
// the ritual checks, print the report, and fail when any stage did not pass.
func renderVerification(w io.Writer, results []Result, verify bool) error {
	if !verify {
		return nil
	}
	report, err := RitualVerification(results)
	if err != nil {
		return err
	}
	writef(w, "%s", RenderVerifyReport(report))
	if !report.AllOK() {
		return fmt.Errorf("assistant ritual verification failed")
	}
	return nil
}

// RitualVerification builds the verification report for `assistant status
// --verify` and `assistant install --verify`. It checks committed guidance
// state first, then reuses the local stdio MCP setup verification seam for safe
// tool visibility. It probes no endpoint and makes no graph read.
func RitualVerification(results []Result) (mcpsetup.VerifyReport, error) {
	report := mcpsetup.VerifyReport{
		Stages: []mcpsetup.StageResult{guidanceStage(results)},
	}
	p, err := mcpsetup.ResolvePlatform("generic")
	if err != nil {
		return mcpsetup.VerifyReport{}, fmt.Errorf("resolve generic mcp setup platform: %w", err)
	}
	snippet, err := mcpsetup.RenderSetupSnippet(p, mcpsetup.SetupRequest{Mode: mcpsetup.ModeLocalStdio})
	if err != nil {
		return mcpsetup.VerifyReport{}, fmt.Errorf("render local stdio mcp setup snippet: %w", err)
	}
	mcpReport := mcpsetup.RunVerification(snippet, mcp.ReadOnlyTools, nil, nil, "")
	report.Stages = append(report.Stages, mcpReport.Stages...)
	return report, nil
}

// guidanceStage summarizes how many selected platforms carry a current managed
// block. It reports OK only when every selected platform is current, so a
// partially-installed project fails --verify.
func guidanceStage(results []Result) mcpsetup.StageResult {
	current := 0
	for _, r := range results {
		if r.Status == BlockCurrent {
			current++
		}
	}
	ok := len(results) > 0 && current == len(results)
	return mcpsetup.StageResult{
		Stage:  mcpsetup.VerifyStage("guidance installed"),
		OK:     ok,
		Detail: fmt.Sprintf("%d/%d platform guidance blocks current", current, len(results)),
	}
}

// RenderVerifyReport formats a verification report as the operator-facing block
// appended to install and status output.
func RenderVerifyReport(report mcpsetup.VerifyReport) string {
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
