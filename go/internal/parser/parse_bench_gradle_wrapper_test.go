// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"bytes"
	"testing"

	gradlewrapper "github.com/eshu-hq/eshu/go/internal/parser/gradle/wrapper"
)

// buildGradleWrapper returns a gradle-wrapper.properties of >= minLOC lines.
// A generated wrapper file is under 400 bytes and the parser refuses a file
// over 64 KiB, so the filler is short comment and property lines that keep
// 10K lines under the bound; a larger file would measure the oversize
// shortcut, not the scan. The distributionUrl carries userinfo and a query
// the parser must drop.
func buildGradleWrapper(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("distributionBase=GRADLE_USER_HOME\n")
	b.WriteString("distributionPath=wrapper/dists\n")
	b.WriteString("distributionUrl=https\\://user:pass@repo.example.com/gradle/gradle-8.5-bin.zip?x=1\n")
	for line := 3; line < minLOC; line++ {
		if line%2 == 0 {
			b.WriteString("#c\n")
		} else {
			b.WriteString("a=b\n")
		}
	}
	return b.Bytes()
}

// TestBuildGradleWrapperStaysInsideTheScanBound keeps the benchmark honest: the
// synthesized file must be scanned and yield its row, not hit the oversize
// shortcut.
func TestBuildGradleWrapperStaysInsideTheScanBound(t *testing.T) {
	t.Parallel()

	source := buildGradleWrapper(minBenchmarkLOC)
	if len(source) > gradlewrapper.MaxSourceBytes {
		t.Fatalf("benchmark input is %d bytes, over the %d byte scan bound", len(source), gradlewrapper.MaxSourceBytes)
	}
	result := gradlewrapper.Scan(source)
	if result.State != gradlewrapper.StateParsed || len(result.Rows) != 1 {
		t.Fatalf("Scan() = state %q rows %v, want one parsed row", result.State, result.Rows)
	}
}
