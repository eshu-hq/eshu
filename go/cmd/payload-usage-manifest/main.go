// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/payloadusage"
)

// options holds the parsed CLI flags for one payload-usage-manifest
// invocation.
type options struct {
	repoRoot          string
	reducerDir        string
	decodeFile        string
	schemaDir         string
	awsStructDir      string
	iamStructDir      string
	incidentStructDir string
	mode              string
	outputPath        string
}

const helpText = `payload-usage-manifest derives, from the typed factschema.Decode* calls in
go/internal/reducer/factschema_decode.go, a manifest of which declared payload
fields each reducer-decoded fact kind's handlers actually read, and gates on a
handler reading a field that no checked-in JSON Schema
(sdk/go/factschema/schema/*.json) declares (Contract System v1 section 6,
enforcement gate 2).

Modes:
  generate   Write the manifest JSON to -out (default: stdout).
  gate       Build the manifest, compare every used field against the
             declared JSON Schema field set, and fail (exit 1) on any
             violation, naming the handler file, fact kind, and field.

Exit status:
  0  generate always succeeds if parsing succeeds; gate succeeds if no
     handler reads an undeclared field.
  1  a usage/parse error occurred, or (gate mode) one or more handlers read a
     field absent from that fact kind's declared schema.

Flags:
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	paths := payloadusage.Paths{
		RepoRoot:          opts.repoRoot,
		ReducerDir:        opts.reducerDir,
		DecodeFile:        opts.decodeFile,
		SchemaDir:         opts.schemaDir,
		AWSStructDir:      opts.awsStructDir,
		IAMStructDir:      opts.iamStructDir,
		IncidentStructDir: opts.incidentStructDir,
	}

	switch opts.mode {
	case "generate":
		manifest, err := payloadusage.Load(paths)
		if err != nil {
			return err
		}
		return writeManifest(manifest, opts.outputPath, stdout)
	case "gate":
		manifest, violations, err := payloadusage.Gate(paths)
		if err != nil {
			return err
		}
		return reportGate(manifest, violations, stdout)
	default:
		return fmt.Errorf("payload-usage-manifest: unknown -mode %q (want \"generate\" or \"gate\")", opts.mode)
	}
}

func writeManifest(manifest payloadusage.Manifest, outputPath string, stdout io.Writer) error {
	encoded, err := payloadusage.MarshalIndent(manifest)
	if err != nil {
		return err
	}

	if outputPath == "" {
		_, err := io.WriteString(stdout, encoded)
		return err //nolint:wrapcheck // stdout write errors are self-describing.
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("payload-usage-manifest: create output dir for %s: %w", outputPath, err)
	}
	if err := os.WriteFile(outputPath, []byte(encoded), 0o600); err != nil {
		return fmt.Errorf("payload-usage-manifest: write manifest %s: %w", outputPath, err)
	}
	return nil
}

func reportGate(manifest payloadusage.Manifest, violations []payloadusage.Violation, stdout io.Writer) error {
	if len(violations) == 0 {
		_, _ = fmt.Fprintf(stdout, "payload-usage-manifest: %d fact kind(s) checked, no undeclared field usage found\n", len(manifest.Kinds))
		return nil
	}
	for _, v := range violations {
		_, _ = fmt.Fprintln(stdout, v.String())
	}
	return fmt.Errorf("payload-usage-manifest: %d handler(s) read a payload field no declared schema covers (see violations above)", len(violations))
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("payload-usage-manifest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprint(stderr, helpText)
		flags.PrintDefaults()
	}

	opts := options{}
	flags.StringVar(&opts.repoRoot, "repo-root", ".", "repository root (used to resolve default paths)")
	flags.StringVar(&opts.reducerDir, "reducer-dir", "", "directory of reducer handler source (default: <repo-root>/go/internal/reducer)")
	flags.StringVar(&opts.decodeFile, "decode-file", "", "restrict seam parsing to this one file (default: glob <reducer-dir>/factschema_decode*.go across every per-family decode file)")
	flags.StringVar(&opts.schemaDir, "schema-dir", "", "directory of checked-in JSON Schemas (default: <repo-root>/sdk/go/factschema/schema)")
	flags.StringVar(&opts.awsStructDir, "aws-struct-dir", "", "directory of aws/v1 typed structs (default: <repo-root>/sdk/go/factschema/aws/v1)")
	flags.StringVar(&opts.iamStructDir, "iam-struct-dir", "", "directory of iam/v1 typed structs (default: <repo-root>/sdk/go/factschema/iam/v1)")
	flags.StringVar(&opts.incidentStructDir, "incident-struct-dir", "", "directory of incident/v1 typed structs (default: <repo-root>/sdk/go/factschema/incident/v1)")
	flags.StringVar(&opts.mode, "mode", "gate", `"generate" to emit the manifest, "gate" to check it against declared schemas (default "gate")`)
	flags.StringVar(&opts.outputPath, "out", "", "generate mode: output file path (default: stdout)")
	if err := flags.Parse(args); err != nil {
		return options{}, err //nolint:wrapcheck // flag errors (including flag.ErrHelp) are self-describing.
	}

	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	if opts.repoRoot == "" {
		opts.repoRoot = "."
	}
	if strings.TrimSpace(opts.mode) == "" {
		opts.mode = "gate"
	}
	return opts, nil
}
