// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

func init() {
	rootCmd.AddCommand(newServiceReportCommand())
}

// newServiceReportCommand builds the offline service intelligence report
// renderer. It composes an operator-ready report from a captured
// get_service_story response without running an LLM interpretation path.
func newServiceReportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service-report",
		Short: "Compose a service intelligence report from a captured service-story response",
		Long: `service-report composes an operator-ready service intelligence report from a
captured get_service_story response.

It reads JSON from --from or stdin. The input is the response from the service
story route (the standard {"data": ..., "truth": ...} envelope, or a bare
dossier object). The command maps the dossier into the identity,
code_to_runtime, and deployment_config sections, then composes the report. It
runs no query, store, or LLM path: each section's truth is the captured truth,
and sections with no supporting evidence stay visible as partial or unsupported
with explicit limitations and bounded next calls.

Supply-chain and incident sections are emitted unsupported unless their evidence
is wired in; this slice sources only the service story.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runServiceReport,
	}
	cmd.Flags().String("from", "", "Path to a captured service-story response JSON file (default: read stdin)")
	cmd.Flags().String("supply-chain-from", "", "Optional path to a captured supply-chain impact inventory response JSON file")
	cmd.Flags().Bool("json", false, "Emit the composed report as JSON")
	return cmd
}

func runServiceReport(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("from")
	supplyChainPath, _ := cmd.Flags().GetString("supply-chain-from")
	jsonOut, _ := cmd.Flags().GetBool("json")

	raw, err := readServiceReportInput(cmd.InOrStdin(), path)
	if err != nil {
		return err
	}
	dossier, truth, err := parseServiceStoryResponse(raw)
	if err != nil {
		return err
	}

	input := serviceintel.FromServiceStory(dossier, truth)
	if section, err := supplyChainSection(supplyChainPath, input.Subject); err != nil {
		return err
	} else if section != nil {
		input.Sections = append(input.Sections, *section)
	}

	report := serviceintel.Compose(input)

	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("write service report JSON: %w", err)
		}
		return nil
	}
	renderServiceReport(cmd.OutOrStdout(), report)
	return nil
}

func readServiceReportInput(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read service-story response %s: %w", path, err)
		}
		return data, nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("read service-story response from stdin: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("no service-story response provided; pass --from or pipe JSON on stdin")
	}
	return data, nil
}

// supplyChainSection reads an optional captured supply-chain inventory response
// and maps it into the report's supply_chain section. It returns nil when no
// path is given, so the section falls back to its unsupported placeholder.
func supplyChainSection(path string, subject serviceintel.ReportSubject) (*serviceintel.SectionInput, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read supply-chain inventory %s: %w", path, err)
	}
	inventory, truth, err := parseServiceStoryResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("decode supply-chain inventory: %w", err)
	}
	section := serviceintel.FromSupplyChainInventory(inventory, subject, truth)
	return &section, nil
}

// parseServiceStoryResponse extracts the dossier map and optional truth envelope
// from a captured service-story response. It accepts the standard envelope
// ({"data": ..., "truth": ...}) and falls back to treating the whole object as a
// bare dossier when no envelope wrapper is present.
func parseServiceStoryResponse(raw []byte) (map[string]any, *query.TruthEnvelope, error) {
	var envelope struct {
		Data  map[string]any       `json:"data"`
		Truth *query.TruthEnvelope `json:"truth"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, nil, fmt.Errorf("decode service-story response: %w", err)
	}
	if envelope.Data != nil {
		return envelope.Data, envelope.Truth, nil
	}
	var bare map[string]any
	if err := json.Unmarshal(raw, &bare); err != nil {
		return nil, nil, fmt.Errorf("decode service-story dossier: %w", err)
	}
	return bare, nil, nil
}

// renderServiceReport prints a compact, human-readable view of the report. The
// JSON output remains the machine-readable source of truth.
func renderServiceReport(w io.Writer, report serviceintel.Report) {
	_, _ = fmt.Fprintf(w, "Service intelligence report: %s\n", reportSubjectLabel(report.Subject))
	_, _ = fmt.Fprintf(w, "  supported=%t partial=%t truth_class=%s\n\n", report.Supported, report.Partial, report.TruthClass)

	for _, section := range report.Sections {
		_, _ = fmt.Fprintf(w, "[%s] %s\n", strings.ToUpper(string(section.Status)), section.Title)
		if summary := strings.TrimSpace(section.Answer.Summary); summary != "" {
			_, _ = fmt.Fprintf(w, "  %s\n", summary)
		}
		for _, reason := range section.Answer.UnsupportedReasons {
			_, _ = fmt.Fprintf(w, "  - %s\n", reason)
		}
		for _, limitation := range section.Answer.Limitations {
			_, _ = fmt.Fprintf(w, "  ! %s\n", limitation)
		}
	}

	if len(report.NextCalls) > 0 {
		_, _ = fmt.Fprintf(w, "\nRecommended next calls:\n")
		for _, call := range report.NextCalls {
			_, _ = fmt.Fprintf(w, "  - %s", nextCallLabel(call))
			if reason := strings.TrimSpace(call.Reason); reason != "" {
				_, _ = fmt.Fprintf(w, " (%s)", reason)
			}
			_, _ = fmt.Fprintln(w)
		}
	}

	if len(report.Investigations) > 0 {
		_, _ = fmt.Fprintf(w, "\nSuggested investigations:\n")
		for _, inv := range report.Investigations {
			_, _ = fmt.Fprintf(w, "  - [%s] %s -> %s (expect %s)\n",
				inv.Basis, inv.Reason, nextCallLabel(inv.NextCall), inv.ExpectedTruthClass)
		}
	}
}

func reportSubjectLabel(subject serviceintel.ReportSubject) string {
	name := strings.TrimSpace(subject.ServiceName)
	if name == "" {
		return "(unknown service)"
	}
	if id := strings.TrimSpace(subject.ServiceID); id != "" {
		return fmt.Sprintf("%s (%s)", name, id)
	}
	return name
}

func nextCallLabel(call serviceintel.NextCall) string {
	switch {
	case call.Tool != "":
		return call.Tool
	case call.Route != "":
		return call.Route
	case call.Playbook != "":
		return "playbook:" + call.Playbook
	default:
		return "(none)"
	}
}
