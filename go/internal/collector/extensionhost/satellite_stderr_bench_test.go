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
// stderr record, a full default stderr buffer of non-matching lines (the
// prefilter skips them, so the cheapest full scan), and the real worst cases:
// lines that carry "inflated_bytes" but fail Unmarshal, which pay a JSON decode
// per line.
func BenchmarkAttachStderrFigures(b *testing.B) {
	typical := []byte(figuresLine + "\n")
	noMatch := []byte(strings.Repeat(`{"component":"image","state":"terminal","page":0,"facts":0}`+"\n", maxScannedStderrBytes/62))
	// Short lines that pass the prefilter and fail Unmarshal: the most
	// decodes the 64 KiB scan bound allows.
	badShort := []byte(strings.Repeat(`{"inflated_bytes":"x"}`+"\n", maxScannedStderrBytes/23))
	// Lines just under the per-line cap that pass the prefilter and fail
	// Unmarshal after a long decode.
	badLong := []byte(strings.Repeat(
		`{"inflated_bytes":"x","pad":"`+strings.Repeat("p", maxStderrRecordBytes-64)+`"}`+"\n",
		maxScannedStderrBytes/maxStderrRecordBytes))
	cases := []struct {
		name   string
		state  sdkcollector.ResultState
		stderr []byte
	}{
		{"complete", sdkcollector.ResultComplete, typical},
		{"terminal_typical", sdkcollector.ResultTerminal, typical},
		{"terminal_full_buffer_no_match", sdkcollector.ResultTerminal, noMatch},
		{"terminal_full_buffer_bad_json_short_lines", sdkcollector.ResultTerminal, badShort},
		{"terminal_full_buffer_bad_json_long_lines", sdkcollector.ResultTerminal, badLong},
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
