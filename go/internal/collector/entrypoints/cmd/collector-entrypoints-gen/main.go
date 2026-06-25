// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/entrypoints"
)

type options struct {
	repoRoot     string
	manifestPath string
	check        bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	manifests, err := entrypoints.LoadManifestFile(opts.manifestPath)
	if err != nil {
		return err
	}
	for _, manifest := range manifests {
		files, err := entrypoints.Generate(manifest)
		if err != nil {
			return fmt.Errorf("%s: %w", manifest.RuntimeName, err)
		}
		for _, file := range files {
			path := filepath.Join(opts.repoRoot, filepath.FromSlash(manifest.CommandDir), file.Name)
			if opts.check {
				if err := verifyFile(path, file.Contents); err != nil {
					return err
				}
				continue
			}
			if err := writeFile(path, file.Contents); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(stdout, "collector-entrypoints-gen: wrote %s\n", path)
		}
	}
	if opts.check {
		_, _ = fmt.Fprintln(stdout, "collector-entrypoints-gen: generated collector entrypoints are current")
	}
	return nil
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("collector-entrypoints-gen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	opts := options{}
	flags.StringVar(&opts.repoRoot, "repo-root", "..", "repository root that owns generated collector files")
	flags.StringVar(&opts.manifestPath, "manifest", "", "collector entrypoint manifest path")
	flags.BoolVar(&opts.check, "check", false, "verify generated files without writing")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	opts.manifestPath = strings.TrimSpace(opts.manifestPath)
	if opts.repoRoot == "" {
		return options{}, fmt.Errorf("-repo-root is required")
	}
	if opts.manifestPath == "" {
		opts.manifestPath = filepath.Join(opts.repoRoot, "go", "internal", "collector", "entrypoints", "collector_entrypoints.yaml")
	}
	return opts, nil
}

func verifyFile(path string, want []byte) error {
	got, err := os.ReadFile(path) // #nosec G304 -- reads internally-constructed generated file path to verify staleness, not user-supplied input
	if err != nil {
		return fmt.Errorf("read generated file %s: %w", path, err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("generated file %s is stale; run scripts/generate-collector-entrypoints.sh", path)
	}
	return nil
}

func writeFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { // #nosec G301 -- internal generated-file output directory
		return fmt.Errorf("create generated file directory %s: %w", filepath.Dir(path), err)
	}
	current, err := os.ReadFile(path) // #nosec G304 -- reads internally-constructed generated file path to skip identical writes, not user-supplied input
	if err == nil && bytes.Equal(current, contents) {
		return nil
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil { // #nosec G306 -- generated Go source file must be world-readable for the Go toolchain
		return fmt.Errorf("write generated file %s: %w", path, err)
	}
	return nil
}
