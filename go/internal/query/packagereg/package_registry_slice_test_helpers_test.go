// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

// equalStringSlices, equalPacketStringSlices, and stringSliceContains are this
// family's own copies of three trivial, widely-shared root test helpers
// (code_dead_code_contract_test.go, documentation_packet_authz_test.go,
// code_dead_code_scan_test.go). Go never compiles one package's _test.go
// files into anything another package can import, and these three are used
// across dozens of root test files (not package-registry-only), so they stay
// in root and this family carries its own copies rather than forking a move.

// equalStringSlices reports whether got and want hold the same strings in the
// same order.
func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// equalPacketStringSlices reports whether a and b hold the same strings in
// the same order.
func equalPacketStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stringSliceContains reports whether want appears anywhere in values.
func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
