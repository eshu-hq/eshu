// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// runLayers prints the "Gates by layer" section of the generated CI gates
// reference (#7337). scripts/generate-ci-gates-doc.sh calls it so the layer
// questions live in one place, internal/cigates, instead of being copied
// into the shell generator.
func runLayers(args []string) error {
	fs := flag.NewFlagSet("layers", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
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
	_, err = fmt.Fprint(os.Stdout, cigates.RenderLayerIndex(reg))
	return err
}
