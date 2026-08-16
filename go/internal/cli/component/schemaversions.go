// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// RunSchemaVersions reports the schema version each core reducer or query
// consumer currently supports for every core fact kind. A non-empty check of
// the form fact_kind=version classifies that single collector fact version
// instead, and returns an error -- so the command exits non-zero -- when the
// version is not supported. The command is read-only and never changes
// runtime behavior.
func RunSchemaVersions(w io.Writer, jsonOutput bool, check string) error {
	if strings.TrimSpace(check) != "" {
		return runComponentSchemaVersionCheck(w, check, jsonOutput)
	}
	return runComponentSchemaVersionList(w, jsonOutput)
}

// schemaVersionEntry is one core fact kind and the schema version core supports.
type schemaVersionEntry struct {
	FactKind      string `json:"fact_kind"`
	SchemaVersion string `json:"schema_version"`
}

// The schema-versions JSON writers keep their own encoders rather than using
// this package's writeJSON: the surface has always HTML-escaped its output
// (the encoder default), and switching writers would change those bytes.

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
		return encoder.Encode(map[string]any{"fact_schema_versions": entries}) //nolint:wrapcheck // the encode error is the operator-facing text of a failed write; a wrap would change it
	}
	if err := writef(out, "Core fact-kind schema versions (read-only)\n"); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := writef(out, "  %s\t%s\n", entry.FactKind, entry.SchemaVersion); err != nil {
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
			return err //nolint:wrapcheck // the encode error is the operator-facing text of a failed write; a wrap would change it
		}
	} else if err := writef(
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
