// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// runFragments prints the production loader's validated references in order.
func runFragments(args []string) error {
	fs := flag.NewFlagSet("fragments", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" || fs.NArg() != 0 {
		return fmt.Errorf("--registry is required and positional arguments are not accepted")
	}
	loaded, err := cigates.Load(*registry)
	if err != nil {
		return err
	}
	for _, path := range loaded.FragmentPaths() {
		if _, err := fmt.Fprintln(os.Stdout, path); err != nil {
			return fmt.Errorf("write fragment path: %w", err)
		}
	}
	return nil
}
