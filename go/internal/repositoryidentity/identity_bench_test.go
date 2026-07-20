// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryidentity

import "testing"

// BenchmarkNormalizedRemoteKey measures the unified key function on a
// representative input set covering all divergence classes unified in #5421.
func BenchmarkNormalizedRemoteKey(b *testing.B) {
	inputs := []string{
		"https://github.com/eshu-hq/eshu",
		"https://github.com/eshu-hq/eshu.git",
		"git@github.com:eshu-hq/eshu.git",
		"ssh://git@github.com/eshu-hq/eshu.git",
		"git+https://github.com/eshu-hq/eshu.git",
		"git+ssh://git@github.com/eshu-hq/eshu.git",
		"git+git@github.com:eshu-hq/eshu.git",
		"https://github.com:8443/eshu-hq/eshu.git",
		"https://user@github.com/eshu-hq/eshu.git",
		"https://GitHub.Com/eshu-hq/eshu.git",
		"user@host.xz:org/repo.git",
		"",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			_ = NormalizedRemoteKey(input)
		}
	}
}
