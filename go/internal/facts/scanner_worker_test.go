// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestScannerWorkerFactKindRegistry(t *testing.T) {
	t.Parallel()

	wantKinds := []string{
		ScannerWorkerAnalysisFactKind,
		ScannerWorkerWarningFactKind,
	}

	gotKinds := ScannerWorkerFactKinds()
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("ScannerWorkerFactKinds() len = %d, want %d: %#v", len(gotKinds), len(wantKinds), gotKinds)
	}
	for i, want := range wantKinds {
		if gotKinds[i] != want {
			t.Fatalf("ScannerWorkerFactKinds()[%d] = %q, want %q", i, gotKinds[i], want)
		}
		version, ok := ScannerWorkerSchemaVersion(want)
		if !ok {
			t.Fatalf("ScannerWorkerSchemaVersion(%q) ok = false, want true", want)
		}
		if version != ScannerWorkerSchemaVersionV1 {
			t.Fatalf("ScannerWorkerSchemaVersion(%q) = %q, want %q", want, version, ScannerWorkerSchemaVersionV1)
		}
	}

	gotKinds[0] = "mutated"
	if fresh := ScannerWorkerFactKinds(); fresh[0] != ScannerWorkerAnalysisFactKind {
		t.Fatalf("ScannerWorkerFactKinds() returned mutable backing slice: %#v", fresh)
	}
}
