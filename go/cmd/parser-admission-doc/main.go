// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("parser-admission-doc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repoRoot := flags.String("repo-root", "", "repository root containing "+parser.AdmissionDocPath)
	check := flags.Bool("check", false, "report drift and exit non-zero instead of writing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *repoRoot == "" {
		return errors.New("-repo-root is required")
	}

	path := filepath.Join(*repoRoot, parser.AdmissionDocPath)
	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	next, err := parser.ReplaceAdmissionTable(string(current), parser.AdmissionTableMarkdown(parser.DefaultRegistry()))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if next == string(current) {
		_, _ = fmt.Fprintln(stdout, "parser admission table is current")
		return nil
	}
	if *check {
		return fmt.Errorf("%s admission table is stale; run scripts/generate-parser-admission-doc.sh", parser.AdmissionDocPath)
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s\n", parser.AdmissionDocPath)
	return nil
}
