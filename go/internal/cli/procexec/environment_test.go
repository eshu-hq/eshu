// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package procexec_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
)

func TestMergeEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		base      []string
		overrides map[string]string
		want      []string
	}{
		{
			name:      "override replaces a base value",
			base:      []string{"PATH=/bin", "ESHU_QUERY_PROFILE=local-lightweight"},
			overrides: map[string]string{"ESHU_QUERY_PROFILE": "local-authoritative"},
			want:      []string{"ESHU_QUERY_PROFILE=local-authoritative", "PATH=/bin"},
		},
		{
			name:      "override adds a name the base does not carry",
			base:      []string{"PATH=/bin"},
			overrides: map[string]string{"ESHU_GRAPH_BACKEND": "nornicdb"},
			want:      []string{"ESHU_GRAPH_BACKEND=nornicdb", "PATH=/bin"},
		},
		{
			name:      "only the first equals sign splits name from value",
			base:      []string{"ESHU_MCP_AUTH=user=pass=word"},
			overrides: nil,
			want:      []string{"ESHU_MCP_AUTH=user=pass=word"},
		},
		{
			name:      "an entry with no equals sign is dropped",
			base:      []string{"PATH=/bin", "MALFORMED"},
			overrides: nil,
			want:      []string{"PATH=/bin"},
		},
		{
			name:      "an empty name survives as an empty name",
			base:      []string{"=orphan"},
			overrides: nil,
			want:      []string{"=orphan"},
		},
		{
			name:      "a duplicated base name keeps the last occurrence",
			base:      []string{"PATH=/first", "PATH=/second"},
			overrides: nil,
			want:      []string{"PATH=/second"},
		},
		{
			name:      "an override to the empty string is kept, not dropped",
			base:      []string{"ESHU_DISABLE_NEO4J=true"},
			overrides: map[string]string{"ESHU_DISABLE_NEO4J": ""},
			want:      []string{"ESHU_DISABLE_NEO4J="},
		},
		{
			name:      "a nil base yields the overrides alone",
			base:      nil,
			overrides: map[string]string{"ESHU_HOME": "/tmp/eshu"},
			want:      []string{"ESHU_HOME=/tmp/eshu"},
		},
		{
			name:      "an empty base and no overrides yield no entries",
			base:      nil,
			overrides: nil,
			want:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := procexec.MergeEnvironment(tt.base, tt.overrides)
			// MergeEnvironment builds its result from a map, so the entry
			// order is not defined. Sort before comparing rather than
			// asserting an order the function never promised.
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("MergeEnvironment(%q, %v) = %q, want %q", tt.base, tt.overrides, got, want)
			}
		})
	}
}

func TestMergeEnvironmentDoesNotMutateBase(t *testing.T) {
	t.Parallel()

	base := []string{"PATH=/bin", "ESHU_HOME=/original"}
	_ = procexec.MergeEnvironment(base, map[string]string{"ESHU_HOME": "/replaced"})

	if base[1] != "ESHU_HOME=/original" {
		t.Fatalf("MergeEnvironment mutated its base slice: got %q", base[1])
	}
}
