// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicereport

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// ReadInput returns the captured service-story response bytes for `eshu
// service-report`: the file at path when path is non-blank, otherwise
// everything readable from stdin. A blank or whitespace-only path counts as
// no path and falls through to stdin. It returns an error in two cases: the
// file read failed, or stdin was read and turned out to be empty or all
// whitespace -- a silent empty report would otherwise print as an
// unsupported dossier with no explanation.
func ReadInput(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- path is an operator-supplied CLI flag pointing to a local captured response file, not an HTTP request param
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

// SupplyChainSection reads an optional captured supply-chain inventory response
// and maps it into the report's supply_chain section. It returns a nil section
// when path is blank, so the section falls back to its unsupported placeholder.
func SupplyChainSection(path string, subject serviceintel.ReportSubject) (*serviceintel.SectionInput, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is an operator-supplied CLI flag pointing to a local supply-chain inventory file, not an HTTP request param
	if err != nil {
		return nil, fmt.Errorf("read supply-chain inventory %s: %w", path, err)
	}
	inventory, truth, err := ParseServiceStoryResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("decode supply-chain inventory: %w", err)
	}
	section := serviceintel.FromSupplyChainInventory(inventory, subject, truth)
	return &section, nil
}

// ParseServiceStoryResponse extracts the dossier map and optional truth envelope
// from a captured service-story response. It accepts the standard envelope
// ({"data": ..., "truth": ...}) and falls back to treating the whole object as a
// bare dossier, with a nil truth envelope, whenever "data" decodes to nil --
// which covers a bare dossier with no wrapper and an explicit "data": null
// alike.
func ParseServiceStoryResponse(raw []byte) (map[string]any, *query.TruthEnvelope, error) {
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

// RenderReport prints a compact, human-readable view of the report to w. The
// JSON output (serviceintel.Report marshaled directly by the caller) remains
// the machine-readable source of truth.
func RenderReport(w io.Writer, report serviceintel.Report) {
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
