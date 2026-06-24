// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command skillgen generates and verifies the per-host skill-file
// roundtrip baseline that the Eshu skillgen epic ships.
//
// Subcommands:
//
//	gen     read skill-fragments/, render per host, write expected/
//	check   read skill-fragments/, render per host, byte-compare to expected/
//	         (exits non-zero on any drift)
//
// Both subcommands read the same inputs (a fragments directory and an
// expected root) so the gen output and the check baseline are always
// produced from the same source. The capability override file at
// `<fragments>/capabilities.local.yaml` is read when present; its
// absence means the default capability set (the on-disk catalog
// enumerated collectors, all enabled).
//
// Run from the go module directory:
//
//	go run ./cmd/skillgen gen
//	go run ./cmd/skillgen check
//
// Flags:
//
//	-fragments  path to the skill-fragments/ directory
//	-expected   path to the expected/ baseline directory
//	-caps       path to a capabilities.local.yaml file (optional)
//	-catalog    path to the editorial surface inventory (specs/surface-inventory.v1.yaml)
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/extensions/skillgen"
)

const (
	defaultFragmentsDir = "../skill-fragments"
	defaultExpectedDir  = "../expected"
	defaultCatalogPath  = "../specs/surface-inventory.v1.yaml"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run executes the skillgen command. The driver is a thin wrapper around
// the skillgen package: the package owns the loader, the render pipeline,
// and the drift check.
func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: skillgen <gen|check> [flags]")
	}
	command := args[0]
	flags := flag.NewFlagSet("skillgen "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	fragmentsDir := flags.String("fragments", defaultFragmentsDir, "path to the skill-fragments/ directory")
	expectedDir := flags.String("expected", defaultExpectedDir, "path to the expected/ baseline directory")
	capsPath := flags.String("caps", "", "path to a capabilities.local.yaml file (defaults to <fragments>/capabilities.local.yaml)")
	catalogPath := flags.String("catalog", defaultCatalogPath, "path to the editorial surface inventory (the S1 contract for the per-collector matrix)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	resolvedCaps := *capsPath
	if resolvedCaps == "" {
		resolvedCaps = filepath.Join(*fragmentsDir, "capabilities.local.yaml")
	}

	caps, err := skillgen.LoadCapabilities(resolvedCaps, *catalogPath)
	if err != nil {
		return err
	}
	fragments, err := skillgen.LoadFragments(*fragmentsDir)
	if err != nil {
		return err
	}
	results, err := skillgen.RenderAll(fragments, caps)
	if err != nil {
		return err
	}

	switch command {
	case "gen":
		return gen(stdout, *expectedDir, results, caps)
	case "check":
		return check(stdout, *expectedDir, results, caps)
	default:
		return fmt.Errorf("skillgen: unknown subcommand %q (supported: gen, check)", command)
	}
}

// gen writes the rendered results to <expectedRoot>/<host>/<output_path>.
// The summary line names the host, the on-disk path, and the byte count so
// a contributor can spot-check the regeneration.
func gen(stdout io.Writer, expectedRoot string, results []skillgen.RenderResult, caps skillgen.Capabilities) error {
	if err := skillgen.WriteExpected(expectedRoot, results); err != nil {
		return err
	}
	source := caps.Source
	if source == "" {
		source = "default"
	}
	_, _ = fmt.Fprintf(stdout, "wrote %d host files under %s (capabilities: %s)\n", len(results), expectedRoot, source)
	for _, r := range results {
		target := filepath.Join(expectedRoot, string(r.Host), r.OutputPath)
		_, _ = fmt.Fprintf(stdout, "  - %s -> %s (%d bytes)\n", r.Host, target, len(r.Bytes))
	}
	return nil
}

// check byte-compares the rendered results against the on-disk baseline.
// A non-empty drift list returns an error and prints one line per drifted
// host so CI logs are immediately useful.
func check(stdout io.Writer, expectedRoot string, results []skillgen.RenderResult, _ skillgen.Capabilities) error {
	drifts, err := skillgen.CheckDrift(expectedRoot, results)
	if err != nil {
		return err
	}
	if len(drifts) == 0 {
		_, _ = fmt.Fprintf(stdout, "skillgen check: %d host files in lockstep with %s\n", len(results), expectedRoot)
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "skillgen check: %d host file(s) drifted from %s\n", len(drifts), expectedRoot)
	for _, d := range drifts {
		_, _ = fmt.Fprintf(stdout, "  - %s: %s (%s)\n", d.Host, d.Path, d.Reason)
	}
	_, _ = fmt.Fprintln(stdout, "run `go run ./cmd/skillgen gen` to regenerate the baseline.")
	return fmt.Errorf("skillgen check failed: %d drifted host file(s)", len(drifts))
}
