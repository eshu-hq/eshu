package cloudruntime

import "testing"

func TestClassifyOrphanedCloudResource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cloud  *ResourceRow
		state  *ResourceRow
		config *ResourceRow
		want   FindingKind
	}{
		{
			name:  "positive_cloud_without_state",
			cloud: &ResourceRow{ARN: "arn:aws:lambda:us-east-1:123456789012:function:worker"},
			want:  FindingKindOrphanedCloudResource,
		},
		{
			name:   "negative_cloud_with_state",
			cloud:  &ResourceRow{ARN: "arn:aws:lambda:us-east-1:123456789012:function:worker"},
			state:  &ResourceRow{ARN: "arn:aws:lambda:us-east-1:123456789012:function:worker"},
			config: &ResourceRow{ARN: "arn:aws:lambda:us-east-1:123456789012:function:worker"},
			want:   "",
		},
		{
			name:  "ambiguous_state_without_cloud_is_not_runtime_drift",
			state: &ResourceRow{ARN: "arn:aws:lambda:us-east-1:123456789012:function:worker"},
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

func TestClassifyUnmanagedCloudResource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cloud  *ResourceRow
		state  *ResourceRow
		config *ResourceRow
		want   FindingKind
	}{
		{
			name:  "positive_cloud_and_state_without_config",
			cloud: &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
			state: &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
			want:  FindingKindUnmanagedCloudResource,
		},
		{
			name:   "negative_cloud_state_and_config_converge",
			cloud:  &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
			state:  &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
			config: &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
			want:   "",
		},
		{
			name:  "ambiguous_cloud_without_state_prefers_orphan",
			cloud: &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
			want:  FindingKindOrphanedCloudResource,
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
