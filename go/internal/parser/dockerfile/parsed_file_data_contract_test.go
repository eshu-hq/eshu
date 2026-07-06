// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestDockerfileStagesRoundTripThroughTypedContract proves the factschema
// codegraphv1.DockerfileStage struct + DecodeParsedFileDataDockerfileStages
// accessor faithfully model the dockerfile parser's real dockerfile_stages
// output: decoding the producer's own Map() output through the typed accessor
// recovers every named identity field and preserves every optional runtime
// field in the open Attributes pass-through. This binds the typed contract to
// the producer as the authoritative shape (issue #4750 S1, "parser owns the
// struct") without changing the emitted bytes — the producer Map() output is
// untouched, so byte-identity of the fact payload holds.
func TestDockerfileStagesRoundTripThroughTypedContract(t *testing.T) {
	t.Parallel()

	// A two-stage Dockerfile exercising the always-present identity block on
	// every stage plus the optional runtime fields (workdir, entrypoint) that
	// stageMap emits only when non-empty.
	payload := RuntimeMetadata(`FROM golang:1.24 AS builder
WORKDIR /src

FROM alpine:3.20
ENTRYPOINT ["/app"]
`).Map()

	stages, err := factschema.DecodeParsedFileDataDockerfileStages(payload)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataDockerfileStages() error = %v, want nil", err)
	}
	rawStages, _ := payload["dockerfile_stages"].([]map[string]any)
	if len(stages) != len(rawStages) {
		t.Fatalf("typed stage count = %d, raw = %d", len(stages), len(rawStages))
	}

	for i, raw := range rawStages {
		stage := stages[i]
		// Named identity fields must match the producer's map exactly.
		if stage.Name != raw["name"] {
			t.Fatalf("stage[%d].Name = %q, raw name = %v", i, stage.Name, raw["name"])
		}
		if stage.BaseImage != raw["base_image"] || stage.BaseTag != raw["base_tag"] {
			t.Fatalf("stage[%d] image/tag = %q/%q, raw = %v/%v", i, stage.BaseImage, stage.BaseTag, raw["base_image"], raw["base_tag"])
		}
		if stage.StageIndex != raw["stage_index"] || stage.LineNumber != raw["line_number"] {
			t.Fatalf("stage[%d] index/line = %d/%d, raw = %v/%v", i, stage.StageIndex, stage.LineNumber, raw["stage_index"], raw["line_number"])
		}
		// Every producer key with no named struct field survives verbatim in
		// the open Attributes pass-through, preserving its JSON-native value.
		for key, want := range raw {
			switch key {
			case "name", "line_number", "stage_index", "base_image", "base_tag", "alias", "path":
				continue // named fields, asserted above
			}
			got, ok := stage.Attributes[key]
			if !ok {
				t.Fatalf("stage[%d] Attributes missing producer key %q", i, key)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("stage[%d] Attributes[%q] = %#v, want %#v", i, key, got, want)
			}
		}
	}
}
