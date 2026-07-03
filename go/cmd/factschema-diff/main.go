// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// options holds the parsed CLI flags for one factschema-diff invocation.
type options struct {
	repoRoot   string
	schemaDir  string
	baseRef    string
	explicitOK bool // true when -base-ref was set explicitly by the caller
}

const helpText = `factschema-diff diffs the checked-in JSON Schemas under
sdk/go/factschema/schema/ against a baseline git ref and fails when a schema
changed in a way that breaks compatibility without a corresponding major
version bump (Contract System v1 section 5: remove/rename a required field,
narrow a type, or widen the required set).

Baseline resolution:
  There is no contracts release tag yet (only product v0.0.x tags), so this
  gate cannot diff against "the last contracts tag" the way the design doc's
  steady-state description implies. Instead -base-ref names any git ref
  (default: the merge-base of HEAD against origin/main) and every schema file
  is compared against that ref's version of the same path. A schema file with
  NO counterpart at the baseline ref (a brand new fact kind) is NOT a
  break — it passes unconditionally, since there is nothing to have broken.
  This keeps the gate correct today (no tag exists) and forward-compatible
  once a factschema release tag exists: pass -base-ref <tag> at that point.

Exit status:
  0  no breaking changes detected.
  1  one or more breaking changes detected, or a usage/git error occurred.
     Every breaking-change failure names the specific field and violation
     type (removed_required_field, narrowed_type, widened_required) so an
     external collector author can act on it without asking a human.

Flags:
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}

	baseRef := opts.baseRef
	if !opts.explicitOK {
		resolved, err := mergeBaseRef(opts.repoRoot, "origin/main")
		if err != nil {
			return fmt.Errorf("factschema-diff: resolve default -base-ref (merge-base against origin/main): %w", err)
		}
		baseRef = resolved
	}

	relSchemaDir, err := filepath.Rel(opts.repoRoot, opts.schemaDir)
	if err != nil {
		return fmt.Errorf("factschema-diff: compute schema dir relative to repo root: %w", err)
	}
	relSchemaDir = filepath.ToSlash(relSchemaDir)

	currentFiles, err := listSchemaFiles(opts.schemaDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("factschema-diff: list current schema files in %s: %w", opts.schemaDir, err)
	}
	currentSet := toStringSet(currentFiles)

	baselineFiles, err := listBaselineSchemaFiles(opts.repoRoot, baseRef, relSchemaDir)
	if err != nil {
		return fmt.Errorf("factschema-diff: list baseline schema files at %s: %w", baseRef, err)
	}

	// Compare the UNION of baseline and current schema files. A deletion (a
	// baseline path with no current counterpart) is caught here; a
	// current-only path (a new schema) is a pass. Enumerating only the
	// current tree would let a whole-contract deletion slip through silently.
	names := unionSorted(currentFiles, baselineFiles)
	if len(names) == 0 {
		_, _ = fmt.Fprintf(stdout, "factschema-diff: no schema files at %s or under %s, nothing to check\n", baseRef, opts.schemaDir)
		return nil
	}

	var failed bool
	for _, name := range names {
		relPath := relSchemaDir + "/" + name

		baseline, hasBaseline, err := showAtRef(opts.repoRoot, baseRef, relPath)
		if err != nil {
			return fmt.Errorf("factschema-diff: read baseline schema %s at %s: %w", name, baseRef, err)
		}

		if !currentSet[name] {
			// Absent from the current tree. If it existed at baseline, the
			// contract was deleted — a break. If it never existed either,
			// there is nothing to check.
			if hasBaseline {
				failed = true
				removed := Violation{
					Kind:    ViolationRemovedSchema,
					Field:   name,
					Message: "schema file was present at the baseline and has been removed; deleting a fact-kind payload contract is a breaking change",
				}
				_, _ = fmt.Fprintf(stderr, "factschema-diff: %s: %s\n", name, removed.String())
			}
			continue
		}

		current, err := os.ReadFile(filepath.Join(opts.schemaDir, name)) // #nosec G304 -- name comes from listSchemaFiles(opts.schemaDir).
		if err != nil {
			return fmt.Errorf("factschema-diff: read current schema %s: %w", name, err)
		}

		if !hasBaseline {
			_, _ = fmt.Fprintf(stdout, "factschema-diff: %s has no baseline counterpart at %s (new schema) — pass\n", name, baseRef)
			continue
		}

		violations, err := compareSchemas(name, baseline, current)
		if err != nil {
			return fmt.Errorf("factschema-diff: compare %s against baseline: %w", name, err)
		}
		if len(violations) == 0 {
			_, _ = fmt.Fprintf(stdout, "factschema-diff: %s — no breaking changes\n", name)
			continue
		}

		failed = true
		for _, v := range violations {
			_, _ = fmt.Fprintf(stderr, "factschema-diff: %s: %s\n", name, v.String())
		}
	}

	if failed {
		return fmt.Errorf("factschema-diff: one or more schemas broke compatibility without a major version bump (see violations above)")
	}
	return nil
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("factschema-diff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprint(stderr, helpText)
		flags.PrintDefaults()
	}

	opts := options{}
	flags.StringVar(&opts.repoRoot, "repo-root", ".", "repository root (used to resolve -schema-dir and to run git)")
	flags.StringVar(&opts.schemaDir, "schema-dir", "", "directory containing generated JSON Schemas (default: <repo-root>/sdk/go/factschema/schema)")
	flags.StringVar(&opts.baseRef, "base-ref", "", "git ref to diff against (default: merge-base of HEAD against origin/main)")
	if err := flags.Parse(args); err != nil {
		return options{}, err //nolint:wrapcheck // flag errors (including flag.ErrHelp) are self-describing.
	}

	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	if opts.repoRoot == "" {
		opts.repoRoot = "."
	}
	if strings.TrimSpace(opts.schemaDir) == "" {
		opts.schemaDir = filepath.Join(opts.repoRoot, "sdk", "go", "factschema", "schema")
	}
	if strings.TrimSpace(opts.baseRef) != "" {
		opts.explicitOK = true
	}
	return opts, nil
}

// listSchemaFiles returns the sorted base names of every *.schema.json file
// directly under dir.
func listSchemaFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller adds context.
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".schema.json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// listBaselineSchemaFiles returns the sorted base names of every
// *.schema.json file tracked under relSchemaDir at baseRef, via
// `git ls-tree -r --name-only <ref> -- <relSchemaDir>`. It reuses gitOutput,
// which carries the #nosec G204 annotation for the internal git invocation.
// An empty relSchemaDir at the ref (the directory did not exist yet) returns
// no names and no error.
func listBaselineSchemaFiles(repoRoot, baseRef, relSchemaDir string) ([]string, error) {
	out, err := gitOutput(repoRoot, "ls-tree", "-r", "--name-only", baseRef, "--", relSchemaDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".schema.json") {
			continue
		}
		// git returns repo-relative paths; reduce to the base name so both
		// sides key on the same identifier.
		names = append(names, path.Base(line))
	}
	sort.Strings(names)
	return names, nil
}

// toStringSet builds a set from a slice of names.
func toStringSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// unionSorted returns the sorted, de-duplicated union of two name slices.
func unionSorted(a, b []string) []string {
	set := make(map[string]struct{}, len(a)+len(b))
	for _, n := range a {
		set[n] = struct{}{}
	}
	for _, n := range b {
		set[n] = struct{}{}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// mergeBaseRef resolves `git merge-base HEAD <ref>` from repoRoot, returning
// the merge-base commit SHA to diff against.
func mergeBaseRef(repoRoot, ref string) (string, error) {
	out, err := gitOutput(repoRoot, "merge-base", "HEAD", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// showAtRef reads path as it existed at ref via `git show <ref>:<path>`. It
// returns ok=false (with no error) when the path did not exist at ref — the
// "new schema, no baseline counterpart" case this gate treats as a pass.
func showAtRef(repoRoot, ref, path string) ([]byte, bool, error) {
	// #nosec G204 -- ref is a git SHA (merge-base) or an operator-set -base-ref flag, and path is a schema filename from listSchemaFiles(schemaDir); both are internal CI-gate inputs, not untrusted external data.
	cmd := exec.Command("git", "show", ref+":"+path)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "does not exist") || strings.Contains(stderr, "exists on disk, but not in") {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("git show %s:%s: %w: %s", ref, path, err, stderr)
		}
		return nil, false, fmt.Errorf("git show %s:%s: %w", ref, path, err)
	}
	return out, true, nil
}

func gitOutput(repoRoot string, args ...string) (string, error) {
	// #nosec G204 -- args are internal git subcommands (e.g. "merge-base HEAD <ref>") constructed by this tool, not untrusted input.
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
