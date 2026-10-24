// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"fmt"
	"strings"
	"testing"
)

// paddedArgChain declares n global ARGs. Each default is padLen literal bytes
// followed by a reference to the previous ARG, so every default passes the
// per-reference length bound and the literal prefix keeps the running output
// under maxExpandedLen at the moment the previous value is appended. The
// cap is enforced before each write, so the output never passes
// maxExpandedLen however the bytes are built up.
func paddedArgChain(n, padLen int) string {
	pad := strings.Repeat("x", padLen)
	var source strings.Builder
	fmt.Fprintf(&source, "ARG A1=%s\n", pad)
	for i := 2; i <= n; i++ {
		fmt.Fprintf(&source, "ARG A%d=%s${A%d}\n", i, pad, i-1)
	}
	return source.String()
}

func TestBaseRefPaddedArgChainIsUnresolved(t *testing.T) {
	// Not parallel: allocatedBytes reads process-wide MemStats.
	source := paddedArgChain(50, 4000) + "FROM i:${A50}\n"
	var got Metadata
	allocated := allocatedBytes(func() { got = RuntimeMetadata(source) })
	// Measured 1,701,408 bytes.
	if allocated > 8<<20 {
		t.Fatalf("padded ARG chain allocated %d bytes, want under 8 MiB", allocated)
	}
	stage := stageAtLine(t, got, 51)
	if len(stage.UnresolvedArgs) == 0 {
		t.Fatalf("UnresolvedArgs is empty, want the over-cap expansion marked unresolved")
	}
	if stage.BaseTag != "${A50}" {
		t.Fatalf("BaseTag has %d bytes, want the placeholder kept as written", len(stage.BaseTag))
	}
}

func TestBaseRefPaddedArgChainDoesNotAmplifyAcrossStages(t *testing.T) {
	// Not parallel: allocatedBytes reads process-wide MemStats.
	var source strings.Builder
	source.WriteString(paddedArgChain(100, 4000))
	const stages = 1000
	for i := 0; i < stages; i++ {
		source.WriteString("FROM i:${A100}\n")
	}
	var got Metadata
	allocated := allocatedBytes(func() { got = RuntimeMetadata(source.String()) })
	// Measured 5,145,312 bytes; before the output cap the base tags alone were
	// 400,001,000 bytes.
	if allocated > 32<<20 {
		t.Fatalf("%d FROM lines allocated %d bytes, want under 32 MiB", stages, allocated)
	}
	if len(got.Stages) != stages {
		t.Fatalf("stages = %d, want %d", len(got.Stages), stages)
	}
	total := 0
	for _, stage := range got.Stages {
		if len(stage.UnresolvedArgs) == 0 {
			t.Fatalf("stage on line %d resolved, want every over-cap expansion unresolved", stage.LineNumber)
		}
		total += len(stage.BaseImage) + len(stage.BaseTag)
	}
	if total > 64*1024 {
		t.Fatalf("stage base refs hold %d bytes in total, want under 64 KB", total)
	}
}

func TestBaseRefArgChainUnderCapStillResolves(t *testing.T) {
	t.Parallel()

	source := paddedArgChain(3, 1000) + "FROM i:${A3}\n"
	stage := stageAtLine(t, RuntimeMetadata(source), 4)
	if len(stage.UnresolvedArgs) != 0 {
		t.Fatalf("UnresolvedArgs = %v, want none for a chain under the cap", stage.UnresolvedArgs)
	}
	if len(stage.BaseTag) != 3000 {
		t.Fatalf("BaseTag has %d bytes, want the full 3000-byte value", len(stage.BaseTag))
	}
}

func TestBaseRefExpansionCapBoundary(t *testing.T) {
	t.Parallel()

	atCap := strings.Repeat("x", maxExpandedLen)
	stage := stageAtLine(t, RuntimeMetadata("ARG A="+atCap+"\nFROM ${A}\n"), 2)
	if len(stage.UnresolvedArgs) != 0 || stage.BaseImage != atCap {
		t.Fatalf("a %d-byte expansion = %d unresolved, %d image bytes, want it resolved",
			maxExpandedLen, len(stage.UnresolvedArgs), len(stage.BaseImage))
	}

	overCap := atCap + "y"
	stage = stageAtLine(t, RuntimeMetadata("ARG A="+overCap+"\nFROM ${A}\n"), 2)
	if len(stage.UnresolvedArgs) == 0 || stage.BaseImage != "${A}" {
		t.Fatalf("a %d-byte expansion = %d unresolved, image %.20q, want it unresolved",
			maxExpandedLen+1, len(stage.UnresolvedArgs), stage.BaseImage)
	}
}
