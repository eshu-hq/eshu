// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run dispatches to Ifá's subcommands. "coverage" and "expectations" are
// full subcommands with their own flag sets (coverage.go, expectations.go);
// everything else falls through to the top-level -version flag the P0
// skeleton shipped, preserving that contract unchanged.
func run(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "coverage":
			return runCoverageCommand(args[1:], stdout, stderr)
		case "expectations":
			return runExpectationsCommand(args[1:], stdout, stderr)
		}
	}

	flags := flag.NewFlagSet("ifa", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.Bool("version", false, "print Ifa command version")
	if err := flags.Parse(args); err != nil {
		return err //nolint:wrapcheck // flag errors are self-describing.
	}
	if *version {
		_, _ = fmt.Fprintln(stdout, "ifa: contract-layer skeleton")
		return nil
	}
	if flags.NArg() > 0 {
		flags.Usage()
		return fmt.Errorf("ifa: unknown subcommand %q (want coverage, expectations, or -version)", flags.Arg(0))
	}
	flags.Usage()
	return nil
}
