// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command schemagen regenerates the checked-in JSON Schema artifacts under
// sdk/go/factschema/schema/. Run it via `go generate ./...` from the
// sdk/go/factschema module root (see the //go:generate directive in
// decode.go), or directly with:
//
//	go run ./internal/schemagen/cmd
//
// The command is deterministic: running it twice in a row against an
// unchanged struct produces byte-identical output, which is what
// schema_gen_test.go's drift test relies on.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/sdk/go/factschema/internal/schemagen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// internal/schemagen/cmd -> module root is two levels up.
	moduleRoot, err := moduleRootDir()
	if err != nil {
		return err
	}

	targets := []struct {
		name     string
		generate func() ([]byte, error)
	}{
		{name: "aws_resource.v1.schema.json", generate: schemagen.AWSResourceSchema},
	}

	for _, target := range targets {
		raw, err := target.generate()
		if err != nil {
			return fmt.Errorf("schemagen: generate %s: %w", target.name, err)
		}

		dest := filepath.Join(moduleRoot, "schema", target.name)
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			return fmt.Errorf("schemagen: write %s: %w", dest, err)
		}
	}

	return nil
}

func moduleRootDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("schemagen: getwd: %w", err)
	}
	// go run ./internal/schemagen/cmd is invoked from the module root, so
	// os.Getwd() already is the module root in the go:generate and
	// documented invocation. Guard against accidental invocation from
	// inside the cmd directory itself.
	if filepath.Base(wd) == "cmd" {
		return filepath.Dir(filepath.Dir(wd)), nil
	}
	return wd, nil
}
