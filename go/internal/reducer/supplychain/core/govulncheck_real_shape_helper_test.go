// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// This file documents, as executable test code, how the scanner component
// planned in eshu-hq/eshu#10002 turns a real `govulncheck -json` stream into
// Eshu vulnerability.go_call_reachability facts. It is NOT the production
// emitter: the emitter lives in eshu-hq/eshu-scanner-worker. It exists so the
// reducer tests consume facts derived from recorded govulncheck output
// instead of hand-written payloads.
//
// Rules (design ruling for #10002, section 3 stage B):
//   - one fact per (osv, Trace[0].Module);
//   - level symbol when any finding of the group has Trace[0].Function;
//   - otherwise, in a symbol-level scan, a package-level or module-level
//     finding means govulncheck found no call path: level not_called;
//   - level import (package) and module (module) apply only when the scan
//     itself ran at that coarser level;
//   - govulncheck orders Trace "from the imported vulnerable symbol until the
//     entry point"; Eshu orders entry point first, vulnerable symbol last, so
//     the trace is reversed;
//   - position is rendered filename:line:column.

// govulncheckStreamFixture is a recorded govulncheck run: config plus the
// finding messages, in stream order.
type govulncheckStreamFixture struct {
	config   govulncheckConfig
	findings []govulncheckFinding
}

type govulncheckConfig struct {
	ScannerVersion string `json:"scanner_version"`
	DBLastModified string `json:"db_last_modified"`
	GoVersion      string `json:"go_version"`
	ScanLevel      string `json:"scan_level"`
	ScanMode       string `json:"scan_mode"`
}

type govulncheckFinding struct {
	OSV          string            `json:"osv"`
	FixedVersion string            `json:"fixed_version"`
	Trace        []govulncheckPath `json:"trace"`
}

// govulncheckPath mirrors one upstream golang/vuln Frame. Position is an
// object upstream, not a string.
type govulncheckPath struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Package  string `json:"package"`
	Function string `json:"function"`
	Receiver string `json:"receiver"`
	Position *struct {
		Filename string `json:"filename"`
		Offset   int    `json:"offset"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	} `json:"position"`
}

// loadGovulncheckStream decodes a recorded govulncheck JSON stream, keeping
// the config and finding messages and ignoring osv, SBOM, and progress.
func loadGovulncheckStream(t *testing.T, name string) govulncheckStreamFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "govulncheck", name))
	if err != nil {
		t.Fatalf("read govulncheck fixture %s: %v", name, err)
	}
	var out govulncheckStreamFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		var message struct {
			Config  *govulncheckConfig  `json:"config"`
			Finding *govulncheckFinding `json:"finding"`
		}
		if err := decoder.Decode(&message); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode govulncheck fixture %s: %v", name, err)
		}
		if message.Config != nil {
			out.config = *message.Config
		}
		if message.Finding != nil {
			out.findings = append(out.findings, *message.Finding)
		}
	}
	return out
}

// deriveGoCallReachabilityFacts applies the emitter rules to a COMPLETE run
// (exit code 0, config seen) and returns one go_call_reachability envelope per
// (osv, module), sorted by osv then module.
func deriveGoCallReachabilityFacts(t *testing.T, stream govulncheckStreamFixture, repositoryID string) []facts.Envelope {
	t.Helper()
	type group struct {
		osv, module string
		findings    []govulncheckFinding
	}
	groups := map[string]*group{}
	for _, finding := range stream.findings {
		module := finding.Trace[0].Module
		key := finding.OSV + "\x00" + module
		if groups[key] == nil {
			groups[key] = &group{osv: finding.OSV, module: module}
		}
		groups[key].findings = append(groups[key].findings, finding)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]facts.Envelope, 0, len(keys))
	for _, key := range keys {
		g := groups[key]
		out = append(out, deriveGoCallReachabilityFact(t, stream.config, g.osv, g.module, g.findings, repositoryID))
	}
	return out
}

func deriveGoCallReachabilityFact(
	t *testing.T,
	config govulncheckConfig,
	osv, module string,
	findings []govulncheckFinding,
	repositoryID string,
) facts.Envelope {
	t.Helper()
	var symbolTraces [][]govulncheckPath
	deepestPackage := ""
	for _, finding := range findings {
		first := finding.Trace[0]
		switch {
		case first.Function != "":
			symbolTraces = append(symbolTraces, finding.Trace)
		case first.Package != "" && (deepestPackage == "" || first.Package < deepestPackage):
			deepestPackage = first.Package
		}
	}
	payload := map[string]any{
		"repository_id":   repositoryID,
		"osv_id":          osv,
		"deepest_module":  module,
		"scan_level":      config.ScanLevel,
		"scan_mode":       config.ScanMode,
		"scanner_version": config.ScannerVersion,
	}
	switch {
	case len(symbolTraces) > 0:
		// Shortest trace wins; ties break on the rendered trace text.
		sort.SliceStable(symbolTraces, func(i, j int) bool {
			if len(symbolTraces[i]) != len(symbolTraces[j]) {
				return len(symbolTraces[i]) < len(symbolTraces[j])
			}
			return renderGovulncheckTrace(symbolTraces[i]) < renderGovulncheckTrace(symbolTraces[j])
		})
		best := symbolTraces[0]
		payload["reachability_level"] = "symbol"
		payload["deepest_package"] = best[0].Package
		payload["deepest_symbol"] = best[0].Function
		payload["trace"] = eshuTraceFromGovulncheck(best)
	case deepestPackage != "":
		payload["reachability_level"] = coarseLevel(t, config, "import")
		payload["deepest_package"] = deepestPackage
	default:
		payload["reachability_level"] = coarseLevel(t, config, "module")
	}
	return facts.Envelope{
		FactID:        facts.StableID(chain.VulnerabilityGoCallReachabilityFactKind, map[string]any{"repo": repositoryID, "osv": osv, "module": module}),
		FactKind:      chain.VulnerabilityGoCallReachabilityFactKind,
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload:       payload,
	}
}

// coarseLevel maps a package-only or module-only group to its level: in a
// symbol-level scan govulncheck reports that nothing calls the vulnerable
// surface (not_called); only a scan that itself ran at package or module
// level reports import or module.
func coarseLevel(t *testing.T, config govulncheckConfig, coarse string) string {
	t.Helper()
	switch config.ScanLevel {
	case "symbol":
		return "not_called"
	case "package", "module":
		return coarse
	default:
		t.Fatalf("unexpected govulncheck scan_level %q", config.ScanLevel)
		return ""
	}
}

// eshuTraceFromGovulncheck reverses govulncheck's order (vulnerable symbol
// first) into Eshu's order (entry point first, vulnerable symbol last).
func eshuTraceFromGovulncheck(trace []govulncheckPath) []any {
	out := make([]any, 0, len(trace))
	for i := len(trace) - 1; i >= 0; i-- {
		frame := map[string]any{}
		set := func(key, value string) {
			if value != "" {
				frame[key] = value
			}
		}
		set("module", trace[i].Module)
		set("version", trace[i].Version)
		set("package", trace[i].Package)
		set("function", trace[i].Function)
		set("receiver", trace[i].Receiver)
		if p := trace[i].Position; p != nil {
			frame["position"] = fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
		}
		out = append(out, frame)
	}
	return out
}

func renderGovulncheckTrace(trace []govulncheckPath) string {
	parts := make([]string, 0, len(trace))
	for _, frame := range trace {
		parts = append(parts, frame.Package+"."+frame.Function)
	}
	return strings.Join(parts, ">")
}

// deriveGoRunFacts models the emitter's handling of one module's govulncheck
// run. A run that exited non-zero is partial: it emits NO reachability fact
// (a failed analysis must not read as not_called and must not retract a prior
// run's evidence) and one warning naming the failure class. govulncheck still
// prints the config message before it fails, so a failed run's stdout is
// indistinguishable from a clean module's: only the exit code tells them apart.
func deriveGoRunFacts(
	t *testing.T,
	stream govulncheckStreamFixture,
	exitCode int,
	stderr, repositoryID string,
) ([]facts.Envelope, *facts.Envelope) {
	t.Helper()
	if exitCode == 0 {
		return deriveGoCallReachabilityFacts(t, stream, repositoryID), nil
	}
	class := "analysis_failed"
	if strings.Contains(stderr, "requires go >=") {
		class = "toolchain_unsupported"
	}
	warning := facts.Envelope{
		FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": repositoryID, "class": class}),
		FactKind:      "scanner_worker.warning",
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload: map[string]any{
			"analyzer":            "go_reachability",
			"target_kind":         "repository",
			"target_locator_hash": "sha256:" + strings.Repeat("0", 64),
			"reason":              class,
			"warning_class":       class,
			"analysis_status":     "partial",
			"coverage_status":     "incomplete",
		},
	}
	return nil, &warning
}
