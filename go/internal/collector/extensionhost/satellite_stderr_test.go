// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"strings"
	"testing"
)

const figuresLine = `{"component":"image","analyzer":"image","state":"terminal","failure_class":"input_limit_exceeded","inflated_bytes":34359738369,"budget_bytes":34359738368,"compressed_bytes":1077449478}`

func TestParseInputLimitFigures(t *testing.T) {
	t.Parallel()

	want := InputLimitFigures{InflatedBytes: 34359738369, BudgetBytes: 34359738368, CompressedBytes: 1077449478}
	tests := []struct {
		name   string
		stderr string
		want   *InputLimitFigures
	}{
		{name: "empty", stderr: ""},
		{name: "plain log line without figures", stderr: `{"component":"image","state":"complete","facts":3}` + "\n"},
		{name: "figures line", stderr: figuresLine + "\n", want: &want},
		{name: "figures line without trailing newline", stderr: figuresLine, want: &want},
		{
			name:   "unknown keys are ignored, never relayed",
			stderr: `{"path":"/private/rootfs","image":"registry.example/app@sha256:dead","inflated_bytes":5,"budget_bytes":4,"compressed_bytes":1}` + "\n",
			want:   &InputLimitFigures{InflatedBytes: 5, BudgetBytes: 4, CompressedBytes: 1},
		},
		{name: "non-json lines are skipped", stderr: "panic: /private/rootfs\n" + figuresLine + "\ngoroutine 1\n", want: &want},
		{
			name:   "last figures line wins",
			stderr: `{"inflated_bytes":1,"budget_bytes":2,"compressed_bytes":3}` + "\n" + figuresLine + "\n",
			want:   &want,
		},
		{name: "string figure is rejected", stderr: `{"inflated_bytes":"5","budget_bytes":4,"compressed_bytes":1}` + "\n"},
		{name: "fractional figure is rejected", stderr: `{"inflated_bytes":5.5,"budget_bytes":4,"compressed_bytes":1}` + "\n"},
		{name: "negative figure is rejected", stderr: `{"inflated_bytes":-5,"budget_bytes":4,"compressed_bytes":1}` + "\n"},
		{name: "overflowing figure is rejected", stderr: `{"inflated_bytes":9223372036854775808,"budget_bytes":4,"compressed_bytes":1}` + "\n"},
		{name: "partial triple is rejected", stderr: `{"inflated_bytes":5,"budget_bytes":4}` + "\n"},
		{name: "null figure is rejected", stderr: `{"inflated_bytes":null,"budget_bytes":4,"compressed_bytes":1}` + "\n"},
		{
			name:   "a later bad line does not erase an earlier good one",
			stderr: figuresLine + "\n" + `{"inflated_bytes":"x","budget_bytes":1,"compressed_bytes":1}` + "\n",
			want:   &want,
		},
		{name: "oversize line is skipped", stderr: `{"pad":"` + strings.Repeat("x", maxStderrRecordBytes) + `","inflated_bytes":5,"budget_bytes":4,"compressed_bytes":1}` + "\n"},
		{name: "scan window stops at the bound", stderr: strings.Repeat("x\n", maxScannedStderrBytes/2) + figuresLine + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseInputLimitFigures([]byte(tt.stderr))
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("parseInputLimitFigures() = %+v, want nil", *got)
			case tt.want != nil && got == nil:
				t.Fatalf("parseInputLimitFigures() = nil, want %+v", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Fatalf("parseInputLimitFigures() = %+v, want %+v", *got, *tt.want)
			}
		})
	}
}
