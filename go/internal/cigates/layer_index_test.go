// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestRenderLayerIndex(t *testing.T) {
	t.Parallel()
	reg := &cigates.Registry{Gates: []cigates.Gate{
		{ID: "openapi-surface", Layer: cigates.LayerContract, Purpose: "Checks every route | handler pair is in the spec.", Blocking: true},
		{ID: "go-fmt", Layer: cigates.LayerHygiene, Purpose: "Fails when Go files are not gofumpt-formatted.", Blocking: true},
		{ID: "macos-build", Layer: cigates.LayerSecondary, Purpose: "Builds the binaries on macOS.", Blocking: false},
		{ID: "unlabeled-gate", Purpose: "Has no layer yet.", Blocking: true},
	}}

	got := cigates.RenderLayerIndex(reg)

	for _, want := range []string{
		"## Gates by layer",
		"**Ifá** is Eshu's conformance platform",
		"An **Odù** is one of its scenarios",
		"### Hygiene: Is the change well-formed? (1 gate)",
		"- `go-fmt` (blocking): Fails when Go files are not gofumpt-formatted.",
		"### Contract: Do declared or generated artifacts match the code? (1 gate)",
		"- `openapi-surface` (blocking): Checks every route | handler pair is in the spec.",
		"### Secondary: What do we watch without blocking a merge? (1 gate)",
		"- `macos-build` (advisory): Builds the binaries on macOS.",
		"### Unlabeled (1 gate)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderLayerIndex output is missing %q\n--- output ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n| `") {
		t.Errorf("the layer index must not render table rows starting with \"| `\": the gate-table self-test counts those as registry rows\n%s", got)
	}
	if strings.Contains(got, "### Unit") {
		t.Errorf("empty layers must be omitted, got a Unit section:\n%s", got)
	}
	hygiene := strings.Index(got, "### Hygiene")
	contract := strings.Index(got, "### Contract")
	secondary := strings.Index(got, "### Secondary")
	unlabeled := strings.Index(got, "### Unlabeled")
	if hygiene >= contract || contract >= secondary || secondary >= unlabeled {
		t.Errorf("sections out of layer order: hygiene=%d contract=%d secondary=%d unlabeled=%d", hygiene, contract, secondary, unlabeled)
	}
}
