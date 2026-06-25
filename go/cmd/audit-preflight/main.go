// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eshu-hq/eshu/go/internal/auditpreflight"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run validates a competitive-audit issue body. It reads from -file or stdin and
// returns a non-nil error when the issue fails the preflight contract, which the
// caller maps to a non-zero exit so the check can gate issue creation in CI.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("audit-preflight", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "path to the issue body file (default: stdin)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	body, err := readBody(*file, stdin)
	if err != nil {
		return err
	}

	findings := auditpreflight.Validate(body)
	if len(findings) == 0 {
		_, err = fmt.Fprintln(stdout, "audit preflight passed: all required evidence present")
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%d audit preflight findings:\n", len(findings))
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "  [%s] %s: %s\n", finding.Kind, finding.Field, finding.Detail)
	}
	return fmt.Errorf("audit preflight failed: %d findings", len(findings))
}

func readBody(file string, stdin io.Reader) (string, error) {
	if file == "" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(raw), nil
	}
	raw, err := os.ReadFile(file) // #nosec G304 -- file is the -file CLI flag pointing to a local issue body file supplied by the operator
	if err != nil {
		return "", fmt.Errorf("read issue body %s: %w", file, err)
	}
	return string(raw), nil
}
