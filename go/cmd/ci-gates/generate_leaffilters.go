// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// --- generate-leaf-filters subcommand ---

// runGenerateLeafFilters regenerates the shared dorny leaf filters
// (.github/leaf-code-filter.yml and .github/leaf-lockstep-filter.yml) from
// specs/ci-docs-set-patterns.txt and specs/ci-docs-lockstep-paths.txt. The
// drift check proves the committed files match; this command is how a specs
// edit flows back into them.
func runGenerateLeafFilters(args []string) error {
	fs := flag.NewFlagSet("generate-leaf-filters", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", "", "repository root directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		return err
	}
	wrote, err := cigates.WriteLeafFilters(root)
	if err != nil {
		return err
	}
	for _, file := range wrote {
		_, _ = fmt.Fprintln(os.Stdout, file)
	}
	return nil
}
