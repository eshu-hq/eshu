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

func run(args []string, stdout io.Writer, stderr io.Writer) error {
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
	flags.Usage()
	return nil
}
