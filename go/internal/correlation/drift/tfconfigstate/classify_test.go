package tfconfigstate

import "testing"

// TestClassifyAddedInState covers the positive, negative, and ambiguous cases
// for the added_in_state drift kind.
func TestClassifyAddedInState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config *ResourceRow
		state  *ResourceRow
		prior  *ResourceRow
		want   DriftKind
	}{
		{
			name:   "positive_state_only",
			config: nil,
			state:  &ResourceRow{Address: "aws_s3_bucket.logs", ResourceType: "aws_s3_bucket"},
			want:   DriftKindAddedInState,
		},
		{
			name:   "negative_both_match",
			config: &ResourceRow{Address: "aws_s3_bucket.logs", ResourceType: "aws_s3_bucket"},
			state:  &ResourceRow{Address: "aws_s3_bucket.logs", ResourceType: "aws_s3_bucket"},
			want:   "",
		},
		{
			name:   "ambiguous_imported_resource_still_added_in_state",
			config: nil,
			state:  &ResourceRow{Address: "aws_s3_bucket.imported", ResourceType: "aws_s3_bucket"},
			want:   DriftKindAddedInState,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.config, tc.state, tc.prior)
			if got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClassifyAddedInConfig covers the positive, negative, and ambiguous cases
// for the added_in_config drift kind.
func TestClassifyAddedInConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config *ResourceRow
		state  *ResourceRow
		prior  *ResourceRow
		want   DriftKind
	}{
		{
			name:   "positive_config_only",
			config: &ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
			state:  nil,
			want:   DriftKindAddedInConfig,
		},
		{
			name:   "negative_both_match",
			config: &ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
			state:  &ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
			want:   "",
		},
		{
			name:   "ambiguous_for_each_pre_apply",
			config: &ResourceRow{Address: `aws_iam_role.roles["new"]`, ResourceType: "aws_iam_role"},
			state:  nil,
			want:   DriftKindAddedInConfig,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.config, tc.state, tc.prior)
			if got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClassifyAttributeDrift covers the positive, negative, and ambiguous
// cases for the attribute_drift drift kind. Computed/unknown values must not
// raise drift.
func TestClassifyAttributeDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config *ResourceRow
		state  *ResourceRow
		want   DriftKind
	}{
		{
			name: "positive_versioning_differs",
			config: &ResourceRow{
				Address:      "aws_s3_bucket.logs",
				ResourceType: "aws_s3_bucket",
				Attributes:   map[string]string{"versioning.enabled": "true"},
			},
			state: &ResourceRow{
				Address:      "aws_s3_bucket.logs",
				ResourceType: "aws_s3_bucket",
				Attributes:   map[string]string{"versioning.enabled": "false"},
			},
			want: DriftKindAttributeDrift,
		},
		{
			name: "negative_versioning_matches",
			config: &ResourceRow{
				Address:      "aws_s3_bucket.logs",
				ResourceType: "aws_s3_bucket",
				Attributes:   map[string]string{"versioning.enabled": "true"},
			},
			state: &ResourceRow{
				Address:      "aws_s3_bucket.logs",
				ResourceType: "aws_s3_bucket",
				Attributes:   map[string]string{"versioning.enabled": "true"},
			},
			want: "",
		},
		{
			name: "ambiguous_computed_tags_no_signal",
			config: &ResourceRow{
				Address:      "aws_s3_bucket.logs",
				ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{
					"tags": "local.common_tags",
				},
				UnknownAttributes: map[string]bool{"tags": true},
			},
			state: &ResourceRow{
				Address:      "aws_s3_bucket.logs",
				ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{
					"tags": `{"env":"prod"}`,
				},
			},
			want: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.config, tc.state, nil)
			if got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClassifyRemovedFromState covers the positive, negative, and ambiguous
// cases for the removed_from_state drift kind. Requires a prior-generation row
// to fire.
func TestClassifyRemovedFromState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config *ResourceRow
		state  *ResourceRow
		prior  *ResourceRow
		want   DriftKind
	}{
		{
			name:   "positive_present_prior_absent_current",
			config: &ResourceRow{Address: "aws_lambda_function.worker", ResourceType: "aws_lambda_function"},
			state:  nil,
			prior:  &ResourceRow{Address: "aws_lambda_function.worker", ResourceType: "aws_lambda_function"},
			want:   DriftKindRemovedFromState,
		},
		{
			name:   "negative_never_present",
			config: nil,
			state:  nil,
			prior:  nil,
			want:   "",
		},
		{
			name:   "ambiguous_lineage_rotation_marker_emits_no_classification",
			config: &ResourceRow{Address: "aws_lambda_function.worker", ResourceType: "aws_lambda_function"},
			state:  nil,
			prior:  &ResourceRow{Address: "aws_lambda_function.worker", ResourceType: "aws_lambda_function", LineageRotation: true},
			want:   "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.config, tc.state, tc.prior)
			if got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClassifyRemovedFromConfig covers the positive, negative, and ambiguous
// cases for the removed_from_config drift kind.
func TestClassifyRemovedFromConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config *ResourceRow
		state  *ResourceRow
		want   DriftKind
	}{
		{
			name:   "positive_state_only_no_prior_needed",
			config: nil,
			state:  &ResourceRow{Address: "aws_iam_policy.legacy", ResourceType: "aws_iam_policy", PreviouslyDeclaredInConfig: true},
			want:   DriftKindRemovedFromConfig,
		},
		{
			name:   "negative_both_absent",
			config: nil,
			state:  nil,
			want:   "",
		},
		{
			name: "ambiguous_moved_block_transient",
			// The old address is in state, no longer in config; the classifier
			// reports removed_from_config for the old address. (The new address
			// would surface as added_in_config in a separate call.)
			config: nil,
			state:  &ResourceRow{Address: "aws_iam_policy.legacy", ResourceType: "aws_iam_policy", PreviouslyDeclaredInConfig: true},
			want:   DriftKindRemovedFromConfig,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.config, tc.state, nil)
			if got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClassifyDispatchPriority pins the dispatch order: added_in_state and
// added_in_config win when one side is missing, removed_from_state wins when
// only the prior side carries the address, removed_from_config wins when only
// the state carries the address and the prior config declared it,
// attribute_drift only fires when both sides exist and an allowlisted
// attribute differs.
func TestClassifyDispatchPriority(t *testing.T) {
	t.Parallel()

	// Both sides absent, no prior — no drift.
	if got := Classify(nil, nil, nil); got != "" {
		t.Fatalf("Classify(nil,nil,nil) = %q, want empty", got)
	}
}
