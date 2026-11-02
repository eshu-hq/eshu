// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// BenchmarkAttachStderrFigures measures the per-call relay cost: a complete
// call (the success path, no parse), a terminal call with the typical one-line
// stderr record, and the worst case of a full default stderr buffer of
// non-matching lines.
func BenchmarkAttachStderrFigures(b *testing.B) {
	typical := []byte(figuresLine + "\n")
	worst := []byte(strings.Repeat(`{"component":"image","state":"terminal","page":0,"facts":0}`+"\n", maxScannedStderrBytes/62))
	cases := []struct {
		name   string
		state  sdkcollector.ResultState
		stderr []byte
	}{
		{"complete", sdkcollector.ResultComplete, typical},
		{"terminal_typical", sdkcollector.ResultTerminal, typical},
		{"terminal_full_buffer_no_match", sdkcollector.ResultTerminal, worst},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			call := CallResult{Result: sdkcollector.Result{State: tc.state}}
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.stderr)))
			for i := 0; i < b.N; i++ {
				_ = attachStderrFigures(call, tc.stderr)
			}
		})
	}
}
