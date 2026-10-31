// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func mixExsBody(line string) string {
	return "defmodule Demo.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :demo,\n      version: \"0.1.0\",\n" + line + "\n      deps: deps()\n    ]\n  end\nend\n"
}

func TestMixExsSourceKindsAndGuard(t *testing.T) {
	for _, tc := range []struct {
		name, value, spec string
		rejected          int
	}{
		{"exact", "1.17.0", "exact", 0},
		{"range", "~> 1.17", "range", 0},
		{"alias", "latest", "alias", 0},
		{"unresolved", "${ELIXIR_VERSION}", "unresolved", 0},
		{"credential", "https://user:password@example.com", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := shared.RuntimeEvidence{State: "parsed_values", ByteSize: 100, Values: []shared.RuntimeValue{{SourceKind: "mix_exs_elixir", Runtime: "elixir", Value: tc.value, Line: 8}}}
			extraction := ExtractElixirEvidence(e)
			facts, skip := EmitExtraction(testContext("mix.exs"), "repo-1", "mix.exs", extraction)
			if len(facts) != 1-tc.rejected || skip.Rejected != tc.rejected {
				t.Fatalf("facts=%d skip=%+v", len(facts), skip)
			}
			if tc.rejected == 0 {
				if got := classifyDeclaration("mix_exs_elixir", extraction.declarations[0]).Kind; got != tc.spec {
					t.Fatalf("kind=%s want=%s", got, tc.spec)
				}
			}
		})
	}
}

func TestMixExsContentOnlyEntrypoint(t *testing.T) {
	for _, tc := range []struct {
		name, body, spec string
		want             int
	}{
		{"exact pin", mixExsBody("      elixir: \"1.17.0\","), "exact", 1},
		{"range", mixExsBody("      elixir: \"~> 1.17\","), "range", 1},
		{"dynamic value", mixExsBody("      elixir: @elixir_requirement,"), "", 0},
		{"credential", mixExsBody("      elixir: \"https://user:password@example.com\","), "", 0},
		{"malformed", "defmodule Demo.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :demo,\n      elixir: \"~> 1.17\",\n    ]\n  end\n", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := emitDecoded(t, "mix.exs", tc.body)
			if len(facts) != tc.want {
				t.Fatalf("Emit(%q) emitted %d facts, want %d: %+v", tc.body, len(facts), tc.want, facts)
			}
			if tc.want == 1 && (facts[0].SourceKind != "mix_exs_elixir" || facts[0].SpecKind != tc.spec || facts[0].Runtime != "elixir") {
				t.Fatalf("facts = %+v", facts)
			}
		})
	}
}

// TestMixExsAbsentIsNotMalformedSkip proves the skip states stay explicit: a
// mix.exs without an elixir: key is admitted with no skip, a syntactically
// broken one reports manifest_unparseable, and a credential-shaped value
// reports value_rejected while emitting nothing.
func TestMixExsAbsentIsNotMalformedSkip(t *testing.T) {
	if ClassifyPath("a/mix.exs") != FileManifest {
		t.Fatalf("ClassifyPath(mix.exs) is not FileManifest")
	}
	absent, ok := Extract("mix.exs", mixExsBody("      description: \"demo\","))
	if !ok || absent.Len() != 0 || absent.Skip().Skipped() {
		t.Fatalf("absent extraction = %+v (%v), want admitted with no skip", absent, ok)
	}
	malformed, ok := Extract("mix.exs", "defmodule Demo.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :demo,\n      elixir: \"~> 1.17\",\n    ]\n  end\n")
	if !ok || malformed.Len() != 0 || malformed.Skip().Reason != SkipReasonManifestUnparseable {
		t.Fatalf("malformed extraction = %+v (%v), want manifest_unparseable", malformed, ok)
	}
	guarded, ok := Extract("mix.exs", mixExsBody("      elixir: \"https://user:password@example.com\","))
	if !ok || guarded.Len() != 0 || guarded.Skip().Reason != SkipReasonValueRejected || guarded.Skip().Rejected != 1 {
		t.Fatalf("guarded extraction = %+v (%v), want one value_rejected", guarded, ok)
	}
}
