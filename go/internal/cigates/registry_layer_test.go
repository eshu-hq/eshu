// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// layeredYAML renders one gate with the given layer, purpose and blocking
// values so each test states only what it varies.
func layeredYAML(layer, purpose string, blocking bool) string {
	blockingText := "false"
	if blocking {
		blockingText = "true"
	}
	var extra strings.Builder
	if layer != "" {
		extra.WriteString("    layer: " + layer + "\n")
	}
	if purpose != "" {
		extra.WriteString("    purpose: " + purpose + "\n")
	}
	return `version: v1
gates:
  - id: openapi-surface
    name: Verify OpenAPI Surface
    category: exactness
    tier: pre-pr
    blocking: ` + blockingText + `
` + extra.String() + `    triggers:
      - "go/internal/query/openapi*.go"
    local:
      command: "bash scripts/verify-openapi.sh"
    ci:
      workflow: verify-openapi.yml
      job: "Verify OpenAPI gate"
    requirements:
      - go
`
}

func TestLoad_CarriesLayerAndPurpose(t *testing.T) {
	t.Parallel()
	reg, err := cigates.Load(writeYAML(t, layeredYAML("contract", `"  Checks every handler route appears in the OpenAPI spec.  "`, true)))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	gate := reg.Gates[0]
	if gate.Layer != cigates.LayerContract {
		t.Errorf("Layer = %q; want %q", gate.Layer, cigates.LayerContract)
	}
	if want := "Checks every handler route appears in the OpenAPI spec."; gate.Purpose != want {
		t.Errorf("Purpose = %q; want %q (trimmed)", gate.Purpose, want)
	}
}

func TestLoad_AllowsMissingLayerAndPurpose(t *testing.T) {
	t.Parallel()
	reg, err := cigates.Load(writeYAML(t, layeredYAML("", "", true)))
	if err != nil {
		t.Fatalf("Load must accept a gate with no layer or purpose (DescriptionCheck enforces them): %v", err)
	}
	if reg.Gates[0].Layer != "" || reg.Gates[0].Purpose != "" {
		t.Errorf("got layer %q purpose %q; want both empty", reg.Gates[0].Layer, reg.Gates[0].Purpose)
	}
}

func TestLoad_RejectsUnknownLayer(t *testing.T) {
	t.Parallel()
	_, err := cigates.Load(writeYAML(t, layeredYAML("unitt", "Runs the unit tests.", true)))
	if err == nil {
		t.Fatal("Load accepted unknown layer \"unitt\"; want an error")
	}
	if !strings.Contains(err.Error(), `invalid layer "unitt"`) || !strings.Contains(err.Error(), "contract") {
		t.Errorf("error %q should name the bad layer and list the valid ones", err)
	}
}

func TestDescriptionCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		layer    string
		purpose  string
		blocking bool
		wantErr  string
	}{
		{name: "complete", layer: "contract", purpose: "Checks every handler route appears in the OpenAPI spec.", blocking: true},
		{name: "missing layer", purpose: "Checks the spec.", blocking: true, wantErr: "has no layer"},
		{name: "missing purpose", layer: "contract", blocking: true, wantErr: "has no purpose"},
		{name: "purpose too long", layer: "contract", purpose: strings.Repeat("x", cigates.MaxPurposeLength+1), blocking: true, wantErr: "purpose is"},
		{name: "blocking secondary", layer: "secondary", purpose: "Builds on macOS.", blocking: true, wantErr: "secondary but blocking"},
		{name: "advisory secondary", layer: "secondary", purpose: "Builds on macOS.", blocking: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg, err := cigates.Load(writeYAML(t, layeredYAML(tt.layer, tt.purpose, tt.blocking)))
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			errs := cigates.DescriptionCheck(reg)
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("DescriptionCheck = %v; want no errors", errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tt.wantErr) {
				t.Fatalf("DescriptionCheck = %v; want one error containing %q", errs, tt.wantErr)
			}
		})
	}
}

func TestDescriptionCheck_CountsCharactersNotBytes(t *testing.T) {
	t.Parallel()
	// "Ifá Odù " is 8 characters but 10 bytes; a purpose of exactly
	// MaxPurposeLength characters must pass even though it is longer in bytes.
	purpose := strings.Repeat("Ifá Odù ", cigates.MaxPurposeLength/8)
	if n := len([]rune(purpose)); n != cigates.MaxPurposeLength {
		t.Fatalf("fixture has %d characters; want %d", n, cigates.MaxPurposeLength)
	}
	reg, err := cigates.Load(writeYAML(t, layeredYAML("replay", `"`+purpose+`"`, true)))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if errs := cigates.DescriptionCheck(reg); len(errs) != 0 {
		t.Fatalf("DescriptionCheck = %v; want a %d-character multibyte purpose accepted", errs, cigates.MaxPurposeLength)
	}
}

func TestDescriptionCheck_RejectsMultiLinePurpose(t *testing.T) {
	t.Parallel()
	reg, err := cigates.Load(writeYAML(t, layeredYAML("contract", `"Checks the spec.\nAnd more."`, true)))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	errs := cigates.DescriptionCheck(reg)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "one line") {
		t.Fatalf("DescriptionCheck = %v; want one error about a one-line purpose", errs)
	}
}

func TestLayersAreOrderedFromCheapestToWatchOnly(t *testing.T) {
	t.Parallel()
	want := []cigates.Layer{
		cigates.LayerHygiene,
		cigates.LayerUnit,
		cigates.LayerContract,
		cigates.LayerReplay,
		cigates.LayerTruth,
		cigates.LayerPerformance,
		cigates.LayerSecondary,
	}
	if got := cigates.Layers(); !slices.Equal(got, want) {
		t.Fatalf("Layers() = %v; want %v", got, want)
	}
	for _, layer := range want {
		if strings.TrimSpace(cigates.LayerQuestion(layer)) == "" {
			t.Errorf("LayerQuestion(%q) is empty; every layer must say what question it answers", layer)
		}
	}
}
