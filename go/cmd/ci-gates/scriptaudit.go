// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

type scriptAuditSummary struct {
	TrackedShellScripts int `json:"tracked_shell_scripts"`
	GateEntrypoints     int `json:"gate_entrypoints"`
	Referenced          int `json:"referenced"`
	Unreferenced        int `json:"unreferenced"`
}

type scriptAuditJSONOutput struct {
	SchemaVersion string                `json:"schema_version"`
	Summary       scriptAuditSummary    `json:"summary"`
	Scripts       []cigates.ScriptAudit `json:"scripts"`
}

func runAuditScripts(args []string) error {
	fs := flag.NewFlagSet("audit-scripts", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	repoRoot := fs.String("repo-root", "", "repository root directory")
	unreferencedOnly := fs.Bool("unreferenced-only", false, "show only scripts with no supported in-repository references")
	asJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" {
		return fmt.Errorf("--registry is required")
	}
	root, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	reg, err := cigates.Load(*registry)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	audits, err := cigates.AuditScripts(root, reg)
	if err != nil {
		return fmt.Errorf("audit tracked scripts: %w", err)
	}

	summary := summarizeScriptAudits(audits)
	visible := audits
	if *unreferencedOnly {
		visible = filterScriptAudits(audits, cigates.ScriptStatusUnreferenced)
	}
	if *asJSON {
		return printScriptAuditJSON(os.Stdout, summary, visible)
	}
	printScriptAuditText(os.Stdout, summary, visible)
	return nil
}

func summarizeScriptAudits(audits []cigates.ScriptAudit) scriptAuditSummary {
	summary := scriptAuditSummary{TrackedShellScripts: len(audits)}
	for _, audit := range audits {
		switch audit.Status {
		case cigates.ScriptStatusGateEntrypoint:
			summary.GateEntrypoints++
		case cigates.ScriptStatusReferenced:
			summary.Referenced++
		case cigates.ScriptStatusUnreferenced:
			summary.Unreferenced++
		}
	}
	return summary
}

func filterScriptAudits(audits []cigates.ScriptAudit, status cigates.ScriptStatus) []cigates.ScriptAudit {
	filtered := make([]cigates.ScriptAudit, 0)
	for _, audit := range audits {
		if audit.Status == status {
			filtered = append(filtered, audit)
		}
	}
	return filtered
}

func printScriptAuditJSON(w io.Writer, summary scriptAuditSummary, audits []cigates.ScriptAudit) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(scriptAuditJSONOutput{
		SchemaVersion: "v1",
		Summary:       summary,
		Scripts:       audits,
	}); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return nil
}

func printScriptAuditText(w io.Writer, summary scriptAuditSummary, audits []cigates.ScriptAudit) {
	_, _ = fmt.Fprintln(w, "ADVISORY: unreferenced means no supported in-repository usage reference was observed; gate triggers are coverage, and this is not a deletion verdict.")
	for _, audit := range audits {
		_, _ = fmt.Fprintf(w, "%s\t%s", audit.Status, audit.Path)
		if len(audit.GateCommands) > 0 {
			fields := make([]string, 0, len(audit.GateCommands))
			for _, command := range audit.GateCommands {
				fields = append(fields, command.GateID+":"+command.Field)
			}
			_, _ = fmt.Fprintf(w, "\tgate=%s", strings.Join(fields, ","))
		}
		_, _ = fmt.Fprintln(w)
	}
	_, _ = fmt.Fprintf(w, "TOTAL tracked=%d gate-entrypoint=%d referenced=%d unreferenced=%d\n",
		summary.TrackedShellScripts, summary.GateEntrypoints, summary.Referenced, summary.Unreferenced)
}
