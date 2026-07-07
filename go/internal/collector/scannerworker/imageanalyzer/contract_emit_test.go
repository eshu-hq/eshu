// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"os"
	"strings"
	"testing"
)

func TestScannerWorkerFactsEmitThroughTypedContracts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file string
		want string
	}{
		{file: "analysis.go", want: "factschema.EncodeScannerWorkerAnalysis"},
		{file: "warning.go", want: "factschema.EncodeScannerWorkerWarning"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatalf("os.ReadFile(%s) error = %v, want nil", tc.file, err)
			}
			if !strings.Contains(string(raw), tc.want) {
				t.Fatalf("%s must emit scanner_worker payloads through %s", tc.file, tc.want)
			}
		})
	}
}
