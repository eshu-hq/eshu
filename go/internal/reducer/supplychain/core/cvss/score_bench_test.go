// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import "testing"

// BenchmarkBaseScore measures the per-finding cost of the derivation (#9693):
// one parse and one score per finding that carries a vector but no score.
func BenchmarkBaseScore(b *testing.B) {
	cases := map[string]string{
		"v3.1": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"v4.0": "CVSS:4.0/AV:N/AC:L/AT:P/PR:N/UI:P/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N",
	}
	for name, vector := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := BaseScore(vector); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
