// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func mixEvidenceBody(line string) string {
	return fmt.Sprintf("defmodule Demo.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :demo,\n      version: \"0.1.0\",\n%s\n      deps: deps()\n    ]\n  end\nend\n", line)
}

// TestExtractMixRuntimeEvidenceIsReadOnceTypedHandoff proves the mix.exs
// scanner owns bounded, source-positioned elixir: evidence: literal-only
// values with their real source lines, fail-closed dynamic values, an absent
// key distinct from a malformed file, and the credential guard before
// emission.
func TestExtractMixRuntimeEvidenceIsReadOnceTypedHandoff(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		want         []shared.RuntimeValue
		wantRejected int
		wantState    string
	}{
		{"exact", "      elixir: \"1.17.0\",", []shared.RuntimeValue{{SourceKind: "mix_exs_elixir", Runtime: "elixir", Value: "1.17.0", Line: 8}}, 0, "parsed_values"},
		{"range", "      elixir: \"~> 1.17\",", []shared.RuntimeValue{{SourceKind: "mix_exs_elixir", Runtime: "elixir", Value: "~> 1.17", Line: 8}}, 0, "parsed_values"},
		{"dynamic attribute", "      elixir: @elixir_requirement,", nil, 1, "parsed_empty"},
		{"dynamic call", "      elixir: System.get_env(\"ELIXIR_VERSION\"),", nil, 1, "parsed_empty"},
		{"charlist", "      elixir: '~> 1.17',", nil, 1, "parsed_empty"},
		{"comment is absent", "      # elixir: \"~> 9.9\",", nil, 0, "parsed_empty"},
		{"credential", "      elixir: \"https://user:password@example.com\",", nil, 1, "parsed_empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := mixEvidenceBody(tc.line)
			evidence := ExtractMixRuntimeEvidence([]byte(body))
			if evidence.State != tc.wantState || evidence.Rejected != tc.wantRejected || len(evidence.Values) != len(tc.want) {
				t.Fatalf("evidence = %+v, want state=%q rejected=%d values=%d", evidence, tc.wantState, tc.wantRejected, len(tc.want))
			}
			for i, want := range tc.want {
				if evidence.Values[i] != want {
					t.Fatalf("value = %+v, want %+v", evidence.Values[i], want)
				}
			}
			if evidence.Digest != shared.RuntimeDigest([]byte(body)) || evidence.ByteSize != len(body) {
				t.Fatalf("evidence is not bound to its exact source bytes: %+v", evidence)
			}
		})
	}
}

// TestScanMixRuntimeP2Shapes pins the three reviewer-held shapes: a bare
// elixir: key joins the next line's literal and reports the value's line, a
// single-quoted mention stays absent without a rejection, and a trailing #
// comment never becomes a declaration.
func TestScanMixRuntimeP2Shapes(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		want         []shared.RuntimeValue
		wantRejected int
	}{
		{
			"wrapped literal joins value line",
			"defmodule Demo.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :demo,\n      version: \"0.1.0\",\n      elixir:\n        \"~> 1.17\",\n      deps: deps()\n    ]\n  end\nend\n",
			[]shared.RuntimeValue{{SourceKind: "mix_exs_elixir", Runtime: "elixir", Value: "~> 1.17", Line: 9}},
			0,
		},
		{
			"single-quoted mention is absent",
			mixEvidenceBody("      description: 'requires elixir: \"~> 9.9\",',"),
			nil,
			0,
		},
		{
			"trailing comment key is absent",
			mixEvidenceBody("      version: \"0.1.0\", # elixir: \"9.9.9\""),
			nil,
			0,
		},
		{
			"glued trailing comment key is absent",
			mixEvidenceBody("      version: \"0.1.0\",#elixir: \"9.9.9\""),
			nil,
			0,
		},
		{
			"heredoc block key is absent",
			"defmodule Demo.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :demo,\n      version: \"0.1.0\",\n      description: \"\"\"\n      docs mention elixir: \"~> 9.9\",\n      more text\n      \"\"\",\n      deps: deps()\n    ]\n  end\nend\n",
			nil,
			0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evidence := ExtractMixRuntimeEvidence([]byte(tc.body))
			if evidence.Rejected != tc.wantRejected || len(evidence.Values) != len(tc.want) {
				t.Fatalf("evidence = %+v, want rejected=%d values=%d", evidence, tc.wantRejected, len(tc.want))
			}
			for i, want := range tc.want {
				if evidence.Values[i] != want {
					t.Fatalf("value = %+v, want %+v", evidence.Values[i], want)
				}
			}
			if evidence.Digest != shared.RuntimeDigest([]byte(tc.body)) || evidence.ByteSize != len(tc.body) {
				t.Fatalf("evidence is not bound to its exact source bytes: %+v", evidence)
			}
		})
	}
}

// TestExtractMixRuntimeEvidenceAbsentIsNotMalformed proves a mix.exs without
// an elixir: key stays parsed_empty with no skip signal, while a syntactically
// broken file with an elixir: line is unavailable: absence never implies a
// retry and malformation never implies a version.
func TestExtractMixRuntimeEvidenceAbsentIsNotMalformed(t *testing.T) {
	absent := mixEvidenceBody("      description: \"demo\",")
	if evidence := ExtractMixRuntimeEvidence([]byte(absent)); evidence.State != "parsed_empty" || len(evidence.Values) != 0 || evidence.Rejected != 0 {
		t.Fatalf("absent evidence = %+v, want parsed_empty with nothing held", evidence)
	}
	malformed := "defmodule Demo.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :demo,\n      elixir: \"~> 1.17\",\n    ]\n  end\n"
	if evidence := ExtractMixRuntimeEvidence([]byte(malformed)); evidence.State != "unavailable" || len(evidence.Values) != 0 {
		t.Fatalf("malformed evidence = %+v, want unavailable with no values", evidence)
	}
}

// TestParseMixExsCarriesPrivateRuntimeEvidence proves the native Elixir parse
// attaches the same typed evidence once through the snapshot seam for mix.exs
// only: a sibling .exs file carries no runtime transport.
func TestParseMixExsCarriesPrivateRuntimeEvidence(t *testing.T) {
	dir := t.TempDir()
	mixPath := filepath.Join(dir, "mix.exs")
	if err := os.WriteFile(mixPath, []byte(mixEvidenceBody("      elixir: \"~> 1.17\",")), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(mixPath, false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := payload["elixir_runtime_evidence"].(shared.RuntimeEvidence)
	if !ok || len(evidence.Values) != 1 || evidence.Values[0] != (shared.RuntimeValue{SourceKind: "mix_exs_elixir", Runtime: "elixir", Value: "~> 1.17", Line: 8}) {
		t.Fatalf("native payload evidence = %+v (%v)", payload["elixir_runtime_evidence"], ok)
	}
	otherPath := filepath.Join(dir, "lib.exs")
	if err := os.WriteFile(otherPath, []byte("IO.puts(\"hi\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other, err := Parse(otherPath, false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := other["elixir_runtime_evidence"]; ok {
		t.Fatalf("non-mix .exs file carries runtime evidence: %v", other["elixir_runtime_evidence"])
	}
}
