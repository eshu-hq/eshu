// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

// TestDockerfileRuntimeBaseImageRef pins the runtime-lineage semantics the
// #5460 DERIVED_FROM projection depends on: only the FINAL stage's base is the
// runtime image's real ancestor, alias references resolve transitively to the
// stage they name, and anything the parser could not resolve to a concrete
// image reference (an ARG-parameterized FROM, an empty stage list) stays
// unresolved rather than becoming a guessed edge.
func TestDockerfileRuntimeBaseImageRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stages []map[string]any
		want   string
		wantOK bool
	}{
		{
			name: "single stage digest FROM is exact",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "docker.io/library/alpine@sha256:aaaa",
					"base_tag":    "",
				},
			},
			want:   "docker.io/library/alpine@sha256:aaaa",
			wantOK: true,
		},
		{
			name: "single stage tag FROM rejoins image and tag",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "docker.io/library/alpine",
					"base_tag":    "3.20",
				},
			},
			want:   "docker.io/library/alpine:3.20",
			wantOK: true,
		},
		{
			name: "multi-stage returns final stage base, not the builder base",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "docker.io/library/golang",
					"base_tag":    "1.24",
					"alias":       "builder",
				},
				{
					"stage_index": 1,
					"base_image":  "docker.io/library/alpine",
					"base_tag":    "3.20",
					"copies_from": "builder",
				},
			},
			want:   "docker.io/library/alpine:3.20",
			wantOK: true,
		},
		{
			name: "final stage aliasing a prior stage resolves transitively",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "docker.io/library/golang",
					"base_tag":    "1.24",
					"alias":       "builder",
				},
				{
					"stage_index": 1,
					"base_image":  "builder",
					"base_tag":    "",
				},
			},
			want:   "docker.io/library/golang:1.24",
			wantOK: true,
		},
		{
			name: "ARG-parameterized base stays unresolved",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "${BASE_IMAGE}",
					"base_tag":    "",
				},
			},
			wantOK: false,
		},
		{
			name: "final stage aliasing an ARG-parameterized stage stays unresolved",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "${BASE_IMAGE}",
					"base_tag":    "",
					"alias":       "builder",
				},
				{
					"stage_index": 1,
					"base_image":  "builder",
					"base_tag":    "",
				},
			},
			wantOK: false,
		},
		{
			name:   "no stages is unresolved",
			stages: nil,
			wantOK: false,
		},
		{
			name: "scratch base is unresolved",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "scratch",
					"base_tag":    "",
				},
			},
			wantOK: false,
		},
		{
			// Docker resolves a bare FROM name against earlier stage aliases
			// first and falls back to an implicit docker.io/library image. A
			// bare name that matches no alias is therefore a real image
			// reference, not a dangling stage reference. It carries no tag and
			// no digest, so the exact-only tiering downstream drops it before
			// any edge is written -- this function's job is only to report the
			// effective base, not to apply that filter.
			name: "bare name matching no alias is an implicit library image",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "docker.io/library/golang",
					"base_tag":    "1.24",
					"alias":       "builder",
				},
				{
					"stage_index": 1,
					"base_image":  "notastage",
					"base_tag":    "",
				},
			},
			want:   "notastage",
			wantOK: true,
		},
		{
			name: "tagged reference is never treated as a stage alias",
			stages: []map[string]any{
				{
					"stage_index": 0,
					"base_image":  "docker.io/library/golang",
					"base_tag":    "1.24",
					"alias":       "builder",
				},
				{
					"stage_index": 1,
					"base_image":  "builder",
					"base_tag":    "3.20",
				},
			},
			want:   "builder:3.20",
			wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := dockerfileRuntimeBaseImageRef(tc.stages)
			if ok != tc.wantOK {
				t.Fatalf("dockerfileRuntimeBaseImageRef ok = %v, want %v (ref %q)", ok, tc.wantOK, got)
			}
			if got != tc.want {
				t.Fatalf("dockerfileRuntimeBaseImageRef = %q, want %q", got, tc.want)
			}
		})
	}
}
