// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

func TestContainerImagesTruncatedWarning(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		cloud *cloudruntime.ResourceRow
		state *cloudruntime.ResourceRow
		want  []string
	}{
		{
			name:  "neither_truncated",
			cloud: &cloudruntime.ResourceRow{},
			state: &cloudruntime.ResourceRow{},
			want:  nil,
		},
		{
			name:  "cloud_truncated",
			cloud: &cloudruntime.ResourceRow{ContainerImagesTruncated: true},
			state: &cloudruntime.ResourceRow{},
			want:  []string{"container_images_truncated"},
		},
		{
			name:  "state_truncated",
			cloud: &cloudruntime.ResourceRow{},
			state: &cloudruntime.ResourceRow{ContainerImagesTruncated: true},
			want:  []string{"container_images_truncated"},
		},
		{
			name:  "nil_rows",
			cloud: nil,
			state: nil,
			want:  nil,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := containerImagesTruncatedWarning(tc.cloud, tc.state)
			if len(got) != len(tc.want) {
				t.Fatalf("containerImagesTruncatedWarning() = %#v, want %#v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("containerImagesTruncatedWarning() = %#v, want %#v", got, tc.want)
				}
			}
		})
	}
}
