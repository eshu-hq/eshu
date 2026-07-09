// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run dispatches to Ifá's subcommands. "coverage", "expectations", "drive",
// "graph-dump", "mutate-cassette", and "dead-letters" are full subcommands
// with their own flag sets (coverage.go, expectations.go, drive.go,
// graph_dump.go, mutate_cassette.go, dead_letters.go); everything else falls
// through to the top-level -version flag the P0 skeleton shipped, preserving
// that contract unchanged. ctx is threaded through to "drive", "graph-dump",
// and "dead-letters" — the subcommands that perform live I/O (Postgres,
// respectively the graph backend) a caller may need to cancel;
// "mutate-cassette" and the other subcommands are pure disk-and-memory
// operations.
func run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "coverage":
			return runCoverageCommand(args[1:], stdout, stderr)
		case "expectations":
			return runExpectationsCommand(args[1:], stdout, stderr)
		case "drive":
			return runDriveCommand(ctx, args[1:], stdout, stderr)
		case "graph-dump":
			return runGraphDumpCommand(ctx, args[1:], stdout, stderr)
		case "mutate-cassette":
			return runMutateCassetteCommand(args[1:], stdout, stderr)
		case "dead-letters":
			return runDeadLettersCommand(ctx, args[1:], stdout, stderr)
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
		return fmt.Errorf("ifa: unknown subcommand %q (want coverage, expectations, drive, graph-dump, mutate-cassette, dead-letters, or -version)", flags.Arg(0))
	}
	flags.Usage()
	return nil
}
