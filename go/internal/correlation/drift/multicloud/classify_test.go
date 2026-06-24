// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloud

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

func TestClassifyMirrorsCloudRuntimeJoin(t *testing.T) {
	t.Parallel()

	resource := func() *cloudruntime.ResourceRow {
		return &cloudruntime.ResourceRow{ARN: "//compute.googleapis.com/projects/p/zones/z/instances/i"}
	}

	cases := []struct {
		name   string
		cloud  *cloudruntime.ResourceRow
		state  *cloudruntime.ResourceRow
		config *cloudruntime.ResourceRow
		want   cloudruntime.FindingKind
	}{
		{
			name:  "gcp_cloud_only_is_orphaned",
			cloud: resource(),
			want:  cloudruntime.FindingKindOrphanedCloudResource,
		},
		{
			name:  "gcp_cloud_and_state_without_config_is_unmanaged",
			cloud: resource(),
			state: resource(),
			want:  cloudruntime.FindingKindUnmanagedCloudResource,
		},
		{
			name:   "gcp_cloud_state_config_converge_no_finding",
			cloud:  resource(),
			state:  resource(),
			config: resource(),
			want:   "",
		},
		{
			name:  "state_without_cloud_is_not_runtime_drift",
			state: resource(),
			want:  "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.cloud, tc.state, tc.config)
			if got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyAmbiguousOwnershipOverridesJoin(t *testing.T) {
	t.Parallel()

	row := Row{
		Provider:         cloudinventory.ProviderAzure,
		RawIdentity:      "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/a",
		FindingKind:      cloudruntime.FindingKindAmbiguousCloudResource,
		ManagementStatus: cloudruntime.ManagementStatusAmbiguous,
		Cloud:            &cloudruntime.ResourceRow{ARN: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/a"},
		State:            &cloudruntime.ResourceRow{ARN: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/a"},
		Config:           &cloudruntime.ResourceRow{ARN: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/a"},
	}
	if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindAmbiguousCloudResource {
		t.Fatalf("EffectiveFindingKind() = %q, want ambiguous override of converged join", got)
	}
}

func TestClassifyUnknownCoverageGap(t *testing.T) {
	t.Parallel()

	row := Row{
		Provider:         cloudinventory.ProviderGCP,
		RawIdentity:      "//compute.googleapis.com/projects/p/zones/z/instances/gap",
		FindingKind:      cloudruntime.FindingKindUnknownCloudResource,
		ManagementStatus: cloudruntime.ManagementStatusUnknown,
		Cloud:            &cloudruntime.ResourceRow{ARN: "//compute.googleapis.com/projects/p/zones/z/instances/gap"},
		State:            &cloudruntime.ResourceRow{ARN: "//compute.googleapis.com/projects/p/zones/z/instances/gap"},
		MissingEvidence:  []string{"collector_coverage"},
	}
	if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindUnknownCloudResource {
		t.Fatalf("EffectiveFindingKind() = %q, want unknown coverage-gap finding", got)
	}
}
