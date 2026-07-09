// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

type expectationsOptions struct {
	specsDir       string
	snapshot       string
	replayManifest string
	kind           string
	format         string
}

func parseExpectationsFlags(args []string, stderr io.Writer) (expectationsOptions, error) {
	fs := flag.NewFlagSet("ifa expectations", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o expectationsOptions
	fs.StringVar(&o.specsDir, "specs-dir", "specs", "directory holding the registry specs")
	fs.StringVar(&o.snapshot, "snapshot", "testdata/golden/e2e-20repo-snapshot.json", "path to the B-12 golden snapshot")
	fs.StringVar(&o.replayManifest, "replay-manifest", "", "path to the replay coverage manifest (default: <specs-dir>/"+replaycoverage.ManifestFileName+")")
	fs.StringVar(&o.kind, "kind", "", "print only the derived expectation for one fact kind (default: print every kind)")
	fs.StringVar(&o.format, "format", "json", "output format (only json is supported)")
	if err := fs.Parse(args); err != nil {
		return expectationsOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if o.replayManifest == "" {
		o.replayManifest = filepath.Join(o.specsDir, replaycoverage.ManifestFileName)
	}
	return o, nil
}

// runExpectationsCommand implements `ifa expectations`: it prints Derive's
// output (every derived fact-kind expectation, or one when -kind is set) as
// indented JSON, so an operator or another tool can inspect the derivation
// join without writing a Go test.
func runExpectationsCommand(args []string, stdout, stderr io.Writer) error {
	o, err := parseExpectationsFlags(args, stderr)
	if err != nil {
		return err
	}
	if o.format != "json" {
		return fmt.Errorf("ifa expectations: unsupported -format %q (only json is supported)", o.format)
	}

	entries := facts.FactKindRegistry()
	snap, err := goldengate.LoadSnapshot(o.snapshot)
	if err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	replayManifest, err := replaycoverage.LoadManifest(o.replayManifest)
	if err != nil {
		return err
	}
	exp, err := ifa.Derive(entries, snap, replayManifest)
	if err != nil {
		return fmt.Errorf("derive expectations: %w", err)
	}

	if o.kind != "" {
		for _, ke := range exp.Kinds {
			if ke.Kind == o.kind {
				return printJSON(stdout, ke)
			}
		}
		return fmt.Errorf("ifa expectations: no derived expectation for fact kind %q", o.kind)
	}
	return printJSON(stdout, exp)
}

func printJSON(w io.Writer, v any) error {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal expectations: %w", err)
	}
	_, err = fmt.Fprintln(w, string(payload))
	if err != nil {
		return fmt.Errorf("write expectations: %w", err)
	}
	return nil
}
