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
// service-report`: the file at path when path holds any non-whitespace
// character, otherwise everything readable from stdin. The branch is a
// strings.TrimSpace test, so a blank or whitespace-only path counts as no
// path -- a --from of "   " reads stdin and never opens a file.
//
// It returns an error in three cases: the file read failed, the stdin read
// failed, or stdin was read and turned out to be empty or all whitespace --
// a silent empty report would otherwise print as an unsupported dossier with
// no explanation. That emptiness check covers stdin only. An empty file at a
// real path comes back as empty bytes and fails later, when
// ParseServiceStoryResponse cannot decode it.
func ReadInput(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- a caller-supplied path parameter; the CLI wrapper sources it from an operator flag (--from), not an HTTP request param
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
// when path is blank or whitespace-only -- the same strings.TrimSpace test
// ReadInput branches on -- so the section falls back to its unsupported
// placeholder. It returns an error when the file read fails and when the file
// does not decode.
func SupplyChainSection(path string, subject serviceintel.ReportSubject) (*serviceintel.SectionInput, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- a caller-supplied path parameter; the CLI wrapper sources it from an operator flag (--supply-chain-from), not an HTTP request param
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

// RenderReport prints a compact, human-readable view of the report to w. It
// returns nothing and discards w's write errors, so a failed terminal write
// does not fail the command.
//
// The text view is a lossy projection of the report: it omits Schema, the
// report-level Truth envelope, and the report-level aggregated Limitations,
// all of which the JSON output carries. The JSON output (serviceintel.Report
// marshaled directly by the caller) is therefore the machine-readable source
// of truth, and this function is the only producer of the text form.
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
