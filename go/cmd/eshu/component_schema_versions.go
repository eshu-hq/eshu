// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const componentSchemaCheckFlag = "check"

func init() {
	cmd := &cobra.Command{
		Use:   "schema-versions",
		Short: "List core fact-kind schema versions or classify a collector fact version",
		Long: "Report the schema version each core reducer or query consumer currently " +
			"supports for every core fact kind. With --check fact_kind=version it classifies " +
			"a collector's fact schema version as supported, unsupported_major, " +
			"unsupported_minor, or unknown_kind, and exits non-zero when the version is not " +
			"supported. The command is read-only and never changes runtime behavior.",
		Args: cobra.NoArgs,
		RunE: runComponentSchemaVersions,
	}
	cmd.Flags().Bool(componentJSONFlag, false, "Emit machine-readable JSON")
	cmd.Flags().String(componentSchemaCheckFlag, "", "Classify one fact version as fact_kind=schema_version")
	componentCmd.AddCommand(cmd)
}

func runComponentSchemaVersions(cmd *cobra.Command, _ []string) error {
	asJSON, err := cmd.Flags().GetBool(componentJSONFlag)
	if err != nil {
		return err
	}
	check, err := cmd.Flags().GetString(componentSchemaCheckFlag)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if strings.TrimSpace(check) != "" {
		return runComponentSchemaVersionCheck(out, check, asJSON)
	}
	return runComponentSchemaVersionList(out, asJSON)
}

// schemaVersionEntry is one core fact kind and the schema version core supports.
type schemaVersionEntry struct {
	FactKind      string `json:"fact_kind"`
	SchemaVersion string `json:"schema_version"`
}

func runComponentSchemaVersionList(out io.Writer, asJSON bool) error {
	registry := facts.SupportedSchemaVersions()
	entries := make([]schemaVersionEntry, 0, len(registry))
	for kind, version := range registry {
		entries = append(entries, schemaVersionEntry{FactKind: kind, SchemaVersion: version})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FactKind < entries[j].FactKind })

	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]any{"fact_schema_versions": entries})
	}
	if _, err := fmt.Fprintln(out, "Core fact-kind schema versions (read-only)"); err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(out, "  %s\t%s\n", entry.FactKind, entry.SchemaVersion); err != nil {
			return err
		}
	}
	return nil
}

// schemaVersionCheckResult is the classification of one candidate fact version.
type schemaVersionCheckResult struct {
	FactKind         string              `json:"fact_kind"`
	Candidate        string              `json:"candidate"`
	Compatibility    facts.Compatibility `json:"compatibility"`
	SupportedVersion string              `json:"supported_version,omitempty"`
}

func runComponentSchemaVersionCheck(out io.Writer, check string, asJSON bool) error {
	kind, version, ok := strings.Cut(check, "=")
	kind = strings.TrimSpace(kind)
	version = strings.TrimSpace(version)
	if !ok || kind == "" || version == "" {
		return fmt.Errorf("--check must be fact_kind=schema_version")
	}

	classification := facts.ClassifySchemaVersion(kind, version)
	supported, _ := facts.SchemaVersion(kind)
	result := schemaVersionCheckResult{
		FactKind:         kind,
		Candidate:        version,
		Compatibility:    classification,
		SupportedVersion: supported,
	}

	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(
		out,
		"%s %s -> %s (core supports %q)\n",
		result.FactKind,
		result.Candidate,
		result.Compatibility,
		result.SupportedVersion,
	); err != nil {
		return err
	}

	// Exit non-zero only for an owned core kind carrying an unsupported version.
	// An unknown (out-of-tree) kind is not core's call to reject, matching
	// facts.ValidateSchemaVersion, so the gate does not falsely fail components.
	switch classification {
	case facts.CompatibilityUnsupportedMajor, facts.CompatibilityUnsupportedMinor:
		return fmt.Errorf("fact kind %q schema_version %q is %s", kind, version, classification)
	default:
		return nil
	}
}
