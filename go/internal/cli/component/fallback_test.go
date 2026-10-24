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

func TestUnionListKeepsEveryEnvironmentEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		flagValue []string
		envValue  string
		want      []string
	}{
		{name: "flag entries come first, then the environment", flagValue: []string{"flag.id"}, envValue: "env.id", want: []string{"flag.id", "env.id"}},
		{name: "an empty flag list keeps the environment", envValue: "env.one, env.two,,", want: []string{"env.one", "env.two"}},
		{name: "an unset environment keeps the flag list", flagValue: []string{"flag.id"}, want: []string{"flag.id"}},
		{name: "a shared entry is not repeated", flagValue: []string{"shared", "flag.id"}, envValue: "shared,env.id", want: []string{"shared", "flag.id", "env.id"}},
		{name: "a blank environment adds nothing", flagValue: []string{"flag.id"}, envValue: " , ", want: []string{"flag.id"}},
		{name: "a padded flag entry is trimmed so exact matching can see it", flagValue: []string{" flag.id "}, want: []string{"flag.id"}},
		{name: "a padded flag entry dedupes against the environment", flagValue: []string{"shared "}, envValue: "shared,env.id", want: []string{"shared", "env.id"}},
		{name: "a blank flag entry is dropped", flagValue: []string{" ", "flag.id"}, want: []string{"flag.id"}},
		{name: "a repeated flag entry is not repeated", flagValue: []string{"flag.id", " flag.id "}, want: []string{"flag.id"}},
		{name: "nothing configured stays empty", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := UnionList(tt.flagValue, tt.envValue); !slices.Equal(got, tt.want) {
				t.Fatalf("UnionList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnionListDoesNotAliasTheFlagSlice(t *testing.T) {
	t.Parallel()

	flagValue := make([]string, 1, 4)
	flagValue[0] = "flag.id"
	_ = UnionList(flagValue, "env.id")
	if flagValue[:2][1] != "" {
		t.Fatalf("UnionList() wrote into the caller's backing array: %v", flagValue[:2])
	}
}
