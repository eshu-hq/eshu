// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eol

import (
	"testing"
	"time"
)

// BenchmarkResolve measures one join: the cost a future read path pays per
// declared version. The snapshot loads once and is reused across answers.
func BenchmarkResolve(b *testing.B) {
	sn, err := LoadSnapshot()
	if err != nil {
		b.Fatalf("LoadSnapshot: %v", err)
	}
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Resolve(sn, "go", 1, 24, true, now)
	}
}

// BenchmarkLoadSnapshot measures parsing the embedded snapshot once at
// startup. The read path calls it once and reuses the Snapshot.
func BenchmarkLoadSnapshot(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadSnapshot(); err != nil {
			b.Fatalf("LoadSnapshot: %v", err)
		}
	}
}
