// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"slices"
	"testing"
)

func TestFallbackStringOrdersFlagThenEnvironmentThenDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		flagValue  string
		flagPassed bool
		envValue   string
		want       string
	}{
		{name: "passed flag wins over the environment", flagValue: "disabled", flagPassed: true, envValue: "allowlist", want: "disabled"},
		{name: "passed empty flag still wins", flagValue: "", flagPassed: true, envValue: "allowlist", want: ""},
		{name: "environment beats the flag default", flagValue: "disabled", envValue: " allowlist ", want: "allowlist"},
		{name: "blank environment keeps the flag default", flagValue: "disabled", envValue: "  ", want: "disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := FallbackString(tt.flagValue, tt.flagPassed, tt.envValue); got != tt.want {
				t.Fatalf("FallbackString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFallbackListSplitsEnvironmentAndDropsBlanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		flagValue  []string
		flagPassed bool
		envValue   string
		want       []string
	}{
		{name: "passed flag wins", flagValue: []string{"flag.id"}, flagPassed: true, envValue: "env.id", want: []string{"flag.id"}},
		{name: "environment splits and trims", envValue: "one, two,,", want: []string{"one", "two"}},
		{name: "only blank entries keep the flag value", flagValue: []string{"default"}, envValue: " , ,", want: []string{"default"}},
		{name: "unset environment keeps the flag value", flagValue: []string{"default"}, want: []string{"default"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := FallbackList(tt.flagValue, tt.flagPassed, tt.envValue); !slices.Equal(got, tt.want) {
				t.Fatalf("FallbackList() = %v, want %v", got, tt.want)
			}
		})
	}
}
