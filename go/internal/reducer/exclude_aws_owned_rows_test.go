// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/multicloud"
)

// TestExcludeAWSOwnedRows is a dedicated table-driven boundary test for
// excludeAWSOwnedRows (issue #5759 P2: previously exercised only indirectly
// through TestMultiCloudRuntimeDriftHandlerExcludesAWSOwnedRowsFromPublication,
// which proves the full handler path but not this function's own edge cases
// in isolation). Split into its own file to keep
// multi_cloud_runtime_drift_test.go under the repository's 500-line file cap.
func TestExcludeAWSOwnedRows(t *testing.T) {
	t.Parallel()

	gcpRow := multicloud.Row{Provider: cloudinventory.ProviderGCP, RawIdentity: gcpOrphanID}
	azureRow := multicloud.Row{Provider: cloudinventory.ProviderAzure, RawIdentity: azureUnmgdID}
	awsRow := multicloud.Row{Provider: cloudinventory.ProviderAWS, RawIdentity: awsOrphanARN}

	tests := []struct {
		name string
		rows []multicloud.Row
		want []multicloud.Row
	}{
		{
			name: "nil input",
			rows: nil,
			want: nil,
		},
		{
			name: "empty input",
			rows: []multicloud.Row{},
			want: []multicloud.Row{},
		},
		{
			name: "all AWS rows dropped",
			rows: []multicloud.Row{awsRow, {Provider: cloudinventory.ProviderAWS, RawIdentity: "second-aws"}},
			want: []multicloud.Row{},
		},
		{
			name: "all non-AWS rows pass through unchanged",
			rows: []multicloud.Row{gcpRow, azureRow},
			want: []multicloud.Row{gcpRow, azureRow},
		},
		{
			name: "mixed rows keep only non-AWS, preserving order",
			rows: []multicloud.Row{gcpRow, awsRow, azureRow},
			want: []multicloud.Row{gcpRow, azureRow},
		},
		{
			name: "provider match is case-insensitive",
			rows: []multicloud.Row{{Provider: "AWS", RawIdentity: awsOrphanARN}, gcpRow},
			want: []multicloud.Row{gcpRow},
		},
		{
			name: "provider match trims surrounding whitespace",
			rows: []multicloud.Row{{Provider: "  aws  ", RawIdentity: awsOrphanARN}, gcpRow},
			want: []multicloud.Row{gcpRow},
		},
		{
			name: "a row with an empty provider is NOT filtered",
			rows: []multicloud.Row{{Provider: "", RawIdentity: "unresolved"}, gcpRow},
			want: []multicloud.Row{{Provider: "", RawIdentity: "unresolved"}, gcpRow},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := excludeAWSOwnedRows(tt.rows)
			if len(got) != len(tt.want) {
				t.Fatalf("excludeAWSOwnedRows(%q) = %#v (len %d), want %#v (len %d)",
					tt.name, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i].Provider != tt.want[i].Provider || got[i].RawIdentity != tt.want[i].RawIdentity {
					t.Fatalf("excludeAWSOwnedRows(%q)[%d] = %#v, want %#v", tt.name, i, got[i], tt.want[i])
				}
			}
		})
	}
}
