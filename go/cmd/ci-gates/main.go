// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command ci-gates selects, runs, and validates CI gate entries from the
// specs/ci-gates.v1.yaml registry.
//
// Usage:
//
//	ci-gates select  --registry <path> --tier <tier> [--base <ref>] [--paths-from <file|->] [--explain] [--json]
//	ci-gates run     --registry <path> --tier <tier> [--base <ref>] [--paths-from <file|->] [--json]
//	ci-gates validate --registry <path> --repo-root <path>
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]
	var err error
	switch sub {
	case "select":
		err = runSelect(args)
	case "run":
		err = runRun(args)
	case "validate":
		err = runValidate(args)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "ci-gates: unknown subcommand %q\n", sub)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "ci-gates %s: %v\n", sub, err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "usage: ci-gates <select|run|validate> [flags]")
	_, _ = fmt.Fprintln(w, "  select   --registry <path> --tier <tier> [--base <ref>] [--paths-from <file|->] [--explain] [--json]")
	_, _ = fmt.Fprintln(w, "  run      --registry <path> --tier <tier> [--base <ref>] [--paths-from <file|->] [--json]")
	_, _ = fmt.Fprintln(w, "  validate --registry <path> --repo-root <path>")
}

// --- select subcommand ---

func runSelect(args []string) error {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	tier := fs.String("tier", "pre-pr", "tier ceiling (pre-commit|pre-push|pre-pr|ci-heavy|manual)")
	base := fs.String("base", "origin/main", "git base ref for changed-path detection")
	pathsFrom := fs.String("paths-from", "", "file of changed paths, one per line ('-' for stdin)")
	explain := fs.Bool("explain", false, "print human-readable explanation for each gate")
	asJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" {
		return fmt.Errorf("--registry is required")
	}

	reg, err := cigates.Load(*registry)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	changed, err := resolveChangedPaths(*pathsFrom, *base)
	if err != nil {
		return fmt.Errorf("resolve changed paths: %w", err)
	}

	t := cigates.Tier(*tier)
	sels := reg.Select(changed, t)

	if *asJSON {
		return printSelectJSON(os.Stdout, sels, t, *base)
	}
	printSelectText(os.Stdout, sels, *explain)
	return nil
}

// --- run subcommand ---

func runRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	tier := fs.String("tier", "pre-pr", "tier ceiling (pre-commit|pre-push|pre-pr|ci-heavy|manual)")
	base := fs.String("base", "origin/main", "git base ref for changed-path detection")
	pathsFrom := fs.String("paths-from", "", "file of changed paths, one per line ('-' for stdin)")
	_ = fs.Bool("json", false, "emit JSON summary (reserved for future use)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" {
		return fmt.Errorf("--registry is required")
	}

	reg, err := cigates.Load(*registry)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	changed, err := resolveChangedPaths(*pathsFrom, *base)
	if err != nil {
		return fmt.Errorf("resolve changed paths: %w", err)
	}

	sels := reg.Select(changed, cigates.Tier(*tier))
	return executeGates(os.Stdout, sels)
}

// --- validate subcommand ---

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	repoRoot := fs.String("repo-root", "", "repository root directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" {
		return fmt.Errorf("--registry is required")
	}
	if *repoRoot == "" {
		// Default to git rev-parse when not provided.
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return fmt.Errorf("--repo-root not provided and git rev-parse failed: %w", err)
		}
		*repoRoot = strings.TrimSpace(string(out))
	}

	reg, err := cigates.Load(*registry)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	errs := reg.Validate(*repoRoot)
	if len(errs) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "PASS: ci-gates registry integrity check")
		return nil
	}
	for _, e := range errs {
		_, _ = fmt.Fprintf(os.Stderr, "  ERROR: %v\n", e)
	}
	return fmt.Errorf("%d integrity error(s) found", len(errs))
}

// --- helpers ---

// resolveChangedPaths returns the list of changed file paths. When pathsFrom is
// set it reads from that file (or stdin when "-"). Otherwise it queries git for
// paths changed relative to base.
func resolveChangedPaths(pathsFrom, base string) ([]string, error) {
	if pathsFrom != "" {
		return readPathsFrom(pathsFrom)
	}
	return gitChangedPaths(base)
}

// readPathsFrom reads one path per line from path (or stdin when path=="-").
func readPathsFrom(path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path) // #nosec G304 -- operator-provided paths file
		if err != nil {
			return nil, fmt.Errorf("open paths file %s: %w", path, err)
		}
		defer f.Close() //nolint:errcheck
		r = f
	}
	var paths []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, scanner.Err()
}

// gitChangedPaths returns the union of committed-vs-base, staged, and unstaged
// changed paths, mirroring the changed_all_files logic in scripts/dev/pre-pr.sh.
func gitChangedPaths(base string) ([]string, error) {
	seen := make(map[string]struct{})
	var all []string
	add := func(lines []string) {
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				return
			}
			if _, ok := seen[l]; !ok {
				seen[l] = struct{}{}
				all = append(all, l)
			}
		}
	}

	run := func(gitArgs ...string) ([]string, error) {
		out, err := exec.Command("git", gitArgs...).Output()
		if err != nil {
			// git diff returns exit 1 when there are differences but output is valid.
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return strings.Split(string(out), "\n"), nil
			}
			return nil, fmt.Errorf("git %v: %w", gitArgs, err)
		}
		return strings.Split(string(out), "\n"), nil
	}

	// committed vs base
	committed, err := run("diff", "--name-only", base+"...HEAD")
	if err != nil {
		// Non-fatal: base may not exist locally (e.g. shallow clone).
		committed = nil
	}
	add(committed)

	// unstaged
	unstaged, err := run("diff", "--name-only", "HEAD")
	if err == nil {
		add(unstaged)
	}

	// staged
	staged, err := run("diff", "--name-only", "--cached")
	if err == nil {
		add(staged)
	}

	return all, nil
}

// --- output formatters ---

type selectJSONOutput struct {
	Tier     string            `json:"tier"`
	Base     string            `json:"base"`
	Selected []selectJSONEntry `json:"selected"`
	Skipped  []selectJSONEntry `json:"skipped"`
	CIOnly   []selectJSONEntry `json:"ci_only"`
}

type selectJSONEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Reason  string `json:"reason"`
	Command string `json:"command,omitempty"`
}

func printSelectJSON(w io.Writer, sels []cigates.Selection, tier cigates.Tier, base string) error {
	out := selectJSONOutput{
		Tier: string(tier),
		Base: base,
	}
	for _, s := range sels {
		entry := selectJSONEntry{
			ID:     s.Gate.ID,
			Name:   s.Gate.Name,
			Reason: s.Reason,
		}
		if s.Gate.Local != nil {
			entry.Command = s.Gate.Local.Command
		}
		switch {
		case s.Gate.CIOnlyReason != "":
			out.CIOnly = append(out.CIOnly, entry)
		case s.Selected:
			out.Selected = append(out.Selected, entry)
		default:
			out.Skipped = append(out.Skipped, entry)
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func printSelectText(w io.Writer, sels []cigates.Selection, explain bool) {
	for _, s := range sels {
		if s.Selected {
			if explain {
				_, _ = fmt.Fprintf(w, "SELECTED  %s — %s\n", s.Gate.ID, s.Reason)
			} else {
				_, _ = fmt.Fprintln(w, s.Gate.ID)
			}
		} else if explain {
			if s.Gate.CIOnlyReason != "" {
				_, _ = fmt.Fprintf(w, "CI-ONLY   %s — %s\n", s.Gate.ID, s.Reason)
			} else {
				_, _ = fmt.Fprintf(w, "SKIPPED   %s — %s\n", s.Gate.ID, s.Reason)
			}
		}
	}
}

// executeGates runs all selected gates, accumulates results, and returns an
// error if any blocking gate failed. Advisory failures are printed but do not
// affect the exit code.
func executeGates(w io.Writer, sels []cigates.Selection) error {
	anyBlockingFail := false
	for _, s := range sels {
		if s.Gate.CIOnlyReason != "" {
			_, _ = fmt.Fprintf(w, "CI-ONLY  %s: %s\n", s.Gate.ID, s.Gate.CIOnlyReason)
			continue
		}
		if !s.Selected {
			_, _ = fmt.Fprintf(w, "SKIP     %s: %s\n", s.Gate.ID, s.Reason)
			continue
		}
		_, _ = fmt.Fprintf(w, "RUN      %s: %s\n", s.Gate.ID, s.Gate.Local.Command)
		if err := runShellCommand(s.Gate.Local.Command); err != nil {
			if s.Gate.Blocking {
				_, _ = fmt.Fprintf(w, "FAIL     %s (blocking): %v\n", s.Gate.ID, err)
				anyBlockingFail = true
			} else {
				_, _ = fmt.Fprintf(w, "FAIL     %s (advisory): %v\n", s.Gate.ID, err)
			}
		} else {
			_, _ = fmt.Fprintf(w, "PASS     %s\n", s.Gate.ID)
		}
	}
	if anyBlockingFail {
		return fmt.Errorf("one or more blocking gates failed")
	}
	return nil
}

// runShellCommand executes a shell command string via /bin/sh -c and returns any
// non-zero exit as an error.
func runShellCommand(command string) error {
	cmd := exec.Command("/bin/sh", "-c", command) // #nosec G204 -- command comes from the operator-controlled gate registry
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
