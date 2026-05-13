package main

import "testing"

func TestClaimAwareModeEnabledTrimsCollectorInstancesEnv(t *testing.T) {
	t.Parallel()

	if claimAwareModeEnabled(func(string) string { return " \n\t " }) {
		t.Fatal("claimAwareModeEnabled() = true for whitespace-only env, want false")
	}
	if !claimAwareModeEnabled(func(string) string { return "[]" }) {
		t.Fatal("claimAwareModeEnabled() = false for nonblank env, want true")
	}
}
