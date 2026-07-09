// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// mutateCassetteOptions holds the parsed command-line inputs for one
// "ifa mutate-cassette" run.
type mutateCassetteOptions struct {
	in          string
	out         string
	factKind    string
	kind        string
	field       string
	schemaMajor string
	count       int
}

func parseMutateCassetteFlags(args []string, stderr io.Writer) (mutateCassetteOptions, error) {
	fs := flag.NewFlagSet("ifa mutate-cassette", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o mutateCassetteOptions
	fs.StringVar(&o.in, "cassette", "", "path to the source replay/cassette JSON file (required)")
	fs.StringVar(&o.out, "out", "", "path to write the mutated cassette JSON file (required)")
	fs.StringVar(&o.factKind, "fact-kind", "", "fact kind eligible for mutation, e.g. gcp_cloud_resource (required)")
	fs.StringVar(&o.kind, "kind", "", "mutation kind: missing-field or schema-major (required)")
	fs.StringVar(&o.field, "field", "", "required payload field to delete (required for -kind=missing-field)")
	fs.StringVar(&o.schemaMajor, "schema-major", "", "replacement schema_version, e.g. 99.0.0 (required for -kind=schema-major)")
	fs.IntVar(&o.count, "count", 1, "number of facts to mutate, selected deterministically by ascending stable_fact_key")
	if err := fs.Parse(args); err != nil {
		return mutateCassetteOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if o.in == "" {
		return mutateCassetteOptions{}, errors.New("ifa mutate-cassette: -cassette is required")
	}
	if o.out == "" {
		return mutateCassetteOptions{}, errors.New("ifa mutate-cassette: -out is required")
	}
	if o.factKind == "" {
		return mutateCassetteOptions{}, errors.New("ifa mutate-cassette: -fact-kind is required")
	}
	if o.kind == "" {
		return mutateCassetteOptions{}, errors.New("ifa mutate-cassette: -kind is required (missing-field or schema-major)")
	}
	return o, nil
}

// runMutateCassetteCommand implements `ifa mutate-cassette`: the Ifá P3
// failure-path-determinism fixture generator (issue #4396, ADR step 3a). It
// loads the cassette at -cassette through the production cassette.LoadFile
// seam, corrupts -count facts of -fact-kind through internal/ifa.MutateCassette,
// and writes the result to -out — a NEW file, never overwriting -cassette, so
// a committed testdata cassette is never touched by this verb.
//
// This verb performs no I/O beyond reading -cassette and writing -out: it
// opens no database or graph backend, mirroring "ifa drive"'s cassette-first,
// database-free failure mode for a bad flag or a missing input file.
func runMutateCassetteCommand(args []string, stdout, stderr io.Writer) error {
	o, err := parseMutateCassetteFlags(args, stderr)
	if err != nil {
		return err
	}

	src, err := cassette.LoadFile(o.in)
	if err != nil {
		return fmt.Errorf("ifa mutate-cassette: load cassette %s: %w", o.in, err)
	}

	mutated, targets, err := ifa.MutateCassette(src, ifa.MutateOptions{
		FactKind:    o.factKind,
		Kind:        ifa.MutationKind(o.kind),
		Field:       o.field,
		SchemaMajor: o.schemaMajor,
		Count:       o.count,
	})
	if err != nil {
		return fmt.Errorf("ifa mutate-cassette: %w", err)
	}

	data, err := json.MarshalIndent(mutated, "", "  ")
	if err != nil {
		return fmt.Errorf("ifa mutate-cassette: marshal mutated cassette: %w", err)
	}
	// #nosec G306 -- a scratch cassette fixture, not a secret; world-readable
	// is fine and matches the repo's other generated-fixture permissions.
	if err := os.WriteFile(o.out, data, 0o644); err != nil {
		return fmt.Errorf("ifa mutate-cassette: write %s: %w", o.out, err)
	}

	return printMutateCassetteReport(stdout, o, targets)
}

// printMutateCassetteReport renders exactly which facts were corrupted, so a
// caller (or a proof script) can log which stable_fact_key(s) to expect in the
// resulting dead-letter set or quarantine log without re-deriving the
// selection itself.
func printMutateCassetteReport(w io.Writer, o mutateCassetteOptions, targets []ifa.MutatedFact) error {
	if _, err := fmt.Fprintf(w, "ifa mutate-cassette: cassette=%s out=%s fact_kind=%s kind=%s count=%d\n",
		o.in, o.out, o.factKind, o.kind, len(targets)); err != nil {
		return fmt.Errorf("ifa mutate-cassette: write report: %w", err)
	}
	for _, target := range targets {
		if _, err := fmt.Fprintf(w, "  mutated stable_fact_key=%s scope_id=%s generation_id=%s field=%q\n",
			target.StableFactKey, target.ScopeID, target.GenerationID, target.Field); err != nil {
			return fmt.Errorf("ifa mutate-cassette: write report: %w", err)
		}
	}
	return nil
}
