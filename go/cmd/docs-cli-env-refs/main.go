// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command docs-cli-env-refs verifies public documentation references against
// the code-owned environment registry and the live Eshu Cobra command tree.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/envregistry"
)

const defaultCLITimeout = 2 * time.Minute

type options struct {
	docsRoot string
	baseline string
	eshu     string
	update   bool
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "docs-cli-env-refs: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	var opts options
	flags := flag.NewFlagSet("docs-cli-env-refs", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.docsRoot, "docs-root", "../docs/public", "public Markdown root")
	flags.StringVar(&opts.baseline, "baseline", "../scripts/docs-cli-env-refs-baseline.txt", "burn-down baseline")
	flags.StringVar(&opts.eshu, "eshu", "", "built Eshu CLI binary")
	flags.BoolVar(&opts.update, "update", false, "regenerate the baseline")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(opts.eshu) == "" {
		return errors.New("-eshu is required")
	}

	refs, err := scanDocs(opts.docsRoot)
	if err != nil {
		return err
	}
	cliCtx, cancel := context.WithTimeout(ctx, defaultCLITimeout)
	defer cancel()
	knownFlags, err := collectCLIFlags(cliCtx, opts.eshu)
	if err != nil {
		return err
	}
	unresolved := unresolvedReferences(refs, knownFlags)
	if opts.update {
		if err := writeBaseline(opts.baseline, unresolved); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "docs-cli-env-refs: baseline updated: %d unresolved reference(s)\n", len(unresolved))
		return nil
	}
	baseline, err := readBaseline(opts.baseline)
	if err != nil {
		return err
	}
	newRefs := difference(unresolved, baseline)
	if len(newRefs) > 0 {
		for _, ref := range newRefs {
			fmt.Fprintf(os.Stderr, "docs-cli-env-refs: %s cites unknown %s %s (not in %s)\n", ref.Document, ref.Kind, ref.Value, opts.baseline)
		}
		return fmt.Errorf("%d documentation reference(s) are not registered or baselined", len(newRefs))
	}
	fmt.Fprintf(os.Stderr, "docs-cli-env-refs: OK: %d reference(s) checked, %d unresolved reference(s) baselined\n", len(refs), len(unresolved))
	return nil
}

func scanDocs(root string) (refs []reference, resultErr error) {
	docsRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open docs root: %w", err)
	}
	defer func() {
		if closeErr := docsRoot.Close(); closeErr != nil && resultErr == nil {
			resultErr = fmt.Errorf("close docs root: %w", closeErr)
		}
	}()
	info, err := docsRoot.Stat(".")
	if err != nil {
		return nil, fmt.Errorf("stat opened docs root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("docs root is not a directory: %s", root)
	}

	refs = []reference{}
	err = fs.WalkDir(docsRoot.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		content, err := docsRoot.ReadFile(path)
		if err != nil {
			return err
		}
		refs = append(refs, scanMarkdown(filepath.ToSlash(path), string(content))...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan docs: %w", err)
	}
	refs = uniqueReferences(refs)
	sortReferences(refs)
	return refs, nil
}

func unresolvedReferences(refs []reference, knownFlags map[string]map[string]struct{}) []reference {
	registry := envregistry.Default()
	out := []reference{}
	for _, ref := range refs {
		known := false
		switch ref.Kind {
		case referenceKindEnv:
			known = registry.Covers(ref.Value)
		case referenceKindFlag:
			ref.Command, known = commandFlagTruth(ref.Command, ref.Value, knownFlags)
		}
		if !known {
			out = append(out, ref)
		}
	}
	return uniqueReferences(out)
}

func commandFlagTruth(command string, flag string, truth map[string]map[string]struct{}) (string, bool) {
	if command == "" {
		_, ok := truth[command][flag]
		return command, ok
	}
	parts := strings.Split(command, "/")
	matched := ""
	for index := range parts {
		candidate := strings.Join(parts[:index+1], "/")
		if _, ok := truth[candidate]; !ok {
			if index == 0 || hasCommandChildren(matched, truth) {
				return command, false
			}
			break
		}
		matched = candidate
	}
	flags, ok := truth[matched]
	if !ok {
		return command, false
	}
	_, ok = flags[flag]
	return matched, ok
}

func hasCommandChildren(command string, truth map[string]map[string]struct{}) bool {
	prefix := command + "/"
	for candidate := range truth {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

func uniqueReferences(refs []reference) []reference {
	seen := map[string]reference{}
	for _, ref := range refs {
		seen[referenceKey(ref)] = ref
	}
	out := make([]reference, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	sortReferences(out)
	return out
}

func difference(refs []reference, baseline map[string]struct{}) []reference {
	out := []reference{}
	for _, ref := range refs {
		if _, ok := baseline[referenceKey(ref)]; !ok {
			out = append(out, ref)
		}
	}
	return out
}

func readBaseline(path string) (map[string]struct{}, error) {
	baseline := map[string]struct{}{}
	file, err := os.Open(path) // #nosec G304 -- explicit verifier baseline path
	if errors.Is(err, os.ErrNotExist) {
		return baseline, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open baseline: %w", err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 || (fields[0] != referenceKindEnv && fields[0] != referenceKindFlag) {
			return nil, fmt.Errorf("baseline malformed at line %d: expected <env|flag> <doc> <comma-separated-values>", line)
		}
		for _, value := range strings.Split(fields[2], ",") {
			if strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("baseline malformed at line %d: empty reference value", line)
			}
			ref := reference{Kind: fields[0], Document: fields[1]}
			if ref.Kind == referenceKindFlag {
				parts := strings.SplitN(value, "::", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return nil, fmt.Errorf("baseline malformed at line %d: flag value must be <command>::<flag>", line)
				}
				ref.Command, ref.Value = parts[0], parts[1]
			} else {
				ref.Value = value
			}
			baseline[referenceKey(ref)] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	return baseline, nil
}

func writeBaseline(path string, refs []reference) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create baseline directory: %w", err)
	}
	file, err := os.Create(path) // #nosec G304 -- explicit verifier baseline path
	if err != nil {
		return fmt.Errorf("create baseline: %w", err)
	}
	defer func() { _ = file.Close() }()
	header := []string{
		"# scripts/docs-cli-env-refs-baseline.txt",
		"#",
		"# Burn-down baseline for scripts/verify-docs-cli-env-refs.sh (#6023).",
		"# Format: <env|flag> <docs/public-relative-page> <sorted-comma-separated-references>.",
		"# Regenerate with: bash scripts/verify-docs-cli-env-refs.sh -update",
		"# Shrinking is progress; new unregistered references fail the gate.",
		"#",
	}
	for _, line := range header {
		if _, err := fmt.Fprintln(file, line); err != nil {
			return fmt.Errorf("write baseline: %w", err)
		}
	}
	type groupKey struct{ kind, document string }
	groups := map[groupKey][]string{}
	keys := []groupKey{}
	for _, ref := range refs {
		key := groupKey{kind: ref.Kind, document: ref.Document}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		value := ref.Value
		if ref.Kind == referenceKindFlag {
			value = ref.Command + "::" + ref.Value
		}
		groups[key] = append(groups[key], value)
	}
	for _, key := range keys {
		if _, err := fmt.Fprintf(file, "%s %s %s\n", key.kind, key.document, strings.Join(groups[key], ",")); err != nil {
			return fmt.Errorf("write baseline: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close baseline: %w", err)
	}
	return nil
}
