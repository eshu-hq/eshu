// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicereport

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// errReader fails every Read with a non-EOF error, standing in for a stdin
// stream that dies part-way through instead of ending cleanly.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

const sampleServiceStoryEnvelope = `{
  "data": {
    "service_identity": {
      "service_id": "svc:checkout",
      "service_name": "checkout",
      "kind": "service",
      "repo_id": "repo:checkout",
      "limitations": ["materialization pending for one lane"]
    },
    "entrypoints": [{"x": 1}],
    "network_paths": [{"y": 1}],
    "deployment_lanes": [{"lane_type": "kubernetes"}],
    "result_limits": {"truncated": false}
  },
  "truth": {
    "level": "exact",
    "basis": "authoritative_graph",
    "freshness": {"state": "fresh"}
  }
}`

const sampleSupplyChainEnvelope = `{
  "data": {"count": 3, "truncated": false},
  "truth": {
    "level": "exact",
    "basis": "authoritative_graph",
    "freshness": {"state": "fresh"}
  }
}`

func TestReadInputFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "story.json")
	writeFile(t, path, sampleServiceStoryEnvelope)

	got, err := ReadInput(strings.NewReader(""), path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != sampleServiceStoryEnvelope {
		t.Fatalf("ReadInput did not return the file contents verbatim")
	}
}

func TestReadInputFromStdin(t *testing.T) {
	got, err := ReadInput(strings.NewReader(sampleServiceStoryEnvelope), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != sampleServiceStoryEnvelope {
		t.Fatalf("ReadInput did not return stdin contents verbatim")
	}
}

func TestReadInputEmptyStdinErrors(t *testing.T) {
	if _, err := ReadInput(strings.NewReader("   "), ""); err == nil {
		t.Fatalf("expected an error for empty stdin with no --from path")
	}
}

// TestReadInputWhitespacePathReadsStdin pins the branch condition in
// ReadInput: strings.TrimSpace decides whether a file is opened, so a
// whitespace-only --from path never touches the filesystem and stdin is read
// instead. This is the trap the package docs call out.
func TestReadInputWhitespacePathReadsStdin(t *testing.T) {
	got, err := ReadInput(strings.NewReader(sampleServiceStoryEnvelope), "   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != sampleServiceStoryEnvelope {
		t.Fatalf("a whitespace-only path must fall through to stdin, got %q", got)
	}
}

// TestReadInputStdinReadErrorPropagates covers ReadInput's third error return:
// io.ReadAll failing on its own, which is neither the file read failing nor
// stdin arriving empty.
func TestReadInputStdinReadErrorPropagates(t *testing.T) {
	sentinel := errors.New("stdin stream broke")
	_, err := ReadInput(errReader{err: sentinel}, "")
	if err == nil {
		t.Fatalf("expected an error when the stdin reader itself fails")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("ReadInput must wrap the underlying stdin read error, got %v", err)
	}
	if !strings.Contains(err.Error(), "read service-story response from stdin") {
		t.Fatalf("expected the stdin-read message, got %v", err)
	}
}

func TestReadInputMissingFileErrors(t *testing.T) {
	if _, err := ReadInput(strings.NewReader(""), filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatalf("expected an error for a nonexistent --from path")
	}
}

func TestParseServiceStoryResponseEnvelope(t *testing.T) {
	dossier, truth, err := ParseServiceStoryResponse([]byte(sampleServiceStoryEnvelope))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truth == nil {
		t.Fatalf("expected a non-nil truth envelope from the wrapped response")
	}
	identity, _ := dossier["service_identity"].(map[string]any)
	if identity["service_name"] != "checkout" {
		t.Fatalf("dossier missing expected service_identity.service_name: %+v", dossier)
	}
}

func TestParseServiceStoryResponseBareDossier(t *testing.T) {
	bare := `{"service_identity": {"service_id": "svc:x", "service_name": "x"}}`
	dossier, truth, err := ParseServiceStoryResponse([]byte(bare))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truth != nil {
		t.Fatalf("a bare dossier has no truth envelope, got %+v", truth)
	}
	identity, _ := dossier["service_identity"].(map[string]any)
	if identity["service_name"] != "x" {
		t.Fatalf("dossier missing expected service_identity.service_name: %+v", dossier)
	}
}

func TestParseServiceStoryResponseInvalidJSON(t *testing.T) {
	if _, _, err := ParseServiceStoryResponse([]byte("{not json")); err == nil {
		t.Fatalf("expected an error for invalid JSON")
	}
}

func TestSupplyChainSectionNoPathReturnsNil(t *testing.T) {
	section, err := SupplyChainSection("", serviceintel.ReportSubject{ServiceName: "checkout"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if section != nil {
		t.Fatalf("expected a nil section when no path is given, got %+v", section)
	}
}

func TestSupplyChainSectionReadsCapturedInventory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supply-chain.json")
	writeFile(t, path, sampleSupplyChainEnvelope)

	section, err := SupplyChainSection(path, serviceintel.ReportSubject{ServiceName: "checkout"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if section == nil {
		t.Fatalf("expected a non-nil section for a captured supply-chain inventory")
	}
	if section.Kind != serviceintel.SectionSupplyChain {
		t.Fatalf("section.Kind = %q, want %q", section.Kind, serviceintel.SectionSupplyChain)
	}
}

func TestSupplyChainSectionMissingFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := SupplyChainSection(path, serviceintel.ReportSubject{ServiceName: "checkout"}); err == nil {
		t.Fatalf("expected an error for a nonexistent --supply-chain-from path")
	}
}

func TestRenderReportRendersComposedReport(t *testing.T) {
	dossier, truth, err := ParseServiceStoryResponse([]byte(sampleServiceStoryEnvelope))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	input := serviceintel.FromServiceStory(dossier, truth)
	report := serviceintel.Compose(input)

	var out bytes.Buffer
	RenderReport(&out, report)
	rendered := out.String()

	for _, want := range []string{"checkout", "Service identity", "Supply-chain evidence", "[UNSUPPORTED]", "Suggested investigations:", "unsupported_lane"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered report missing %q:\n%s", want, rendered)
		}
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test fixture %s: %v", path, err)
	}
}
