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

// TestClassifyDispatchPriority pins the dispatch order across classifier
// conflicts: when inputs satisfy more than one drift kind's preconditions,
// the classifier returns the highest-precedence kind. The precedence is
// fixed by classify.go: lineage rotation pre-empts removed_from_state;
// added_in_state and added_in_config win over removed_* when one side is
// fully absent; attribute_drift only fires when both sides exist; and
// both-sides-absent always returns "".
func TestClassifyDispatchPriority(t *testing.T) {
	t.Parallel()

	allowlisted := allowlistedAttributeFor(t, "aws_s3_bucket")

	cases := []struct {
		name   string
		config *ResourceRow
		state  *ResourceRow
		prior  *ResourceRow
		want   DriftKind
	}{
		{
			name:   "both_sides_absent_returns_empty",
			config: nil, state: nil, prior: nil,
			want: "",
		},
		{
			name:   "prior_only_without_config_does_not_classify_as_removed_from_state",
			config: nil,
			state:  nil,
			prior:  &ResourceRow{Address: "aws_iam_role.gone", ResourceType: "aws_iam_role"},
			want:   "",
		},
		{
			name:   "state_present_config_absent_classifies_as_added_in_state_not_attribute_drift",
			config: nil,
			state:  &ResourceRow{Address: "aws_s3_bucket.logs", ResourceType: "aws_s3_bucket"},
			want:   DriftKindAddedInState,
		},
		{
			name:   "config_present_state_absent_classifies_as_added_in_config_not_removed_from_config",
			config: &ResourceRow{Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket"},
			state:  nil,
			want:   DriftKindAddedInConfig,
		},
		{
			name: "both_sides_present_with_allowlisted_diff_classifies_as_attribute_drift",
			config: &ResourceRow{
				Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{allowlisted: "old"},
			},
			state: &ResourceRow{
				Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{allowlisted: "new"},
			},
			want: DriftKindAttributeDrift,
		},
		{
			name: "lineage_rotation_on_prior_suppresses_all_classification",
			config: &ResourceRow{
				Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
			},
			state: nil,
			prior: &ResourceRow{
				Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
				LineageRotation: true,
			},
			want: "",
		},
		{
			name: "without_lineage_rotation_same_inputs_classify_as_removed_from_state",
			config: &ResourceRow{
				Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
			},
			state: nil,
			prior: &ResourceRow{Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket"},
			want:  DriftKindRemovedFromState,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.config, tc.state, tc.prior)
			if got != tc.want {
				t.Fatalf("Classify(%v, %v, %v) = %q, want %q",
					tc.config, tc.state, tc.prior, got, tc.want)
			}
		})
	}
}

// allowlistedAttributeFor returns the first allowlisted attribute for the
// supplied resource type. Tests use this so they don't drift if the allowlist
// seed changes.
func allowlistedAttributeFor(t *testing.T, resourceType string) string {
	t.Helper()
	attrs := AllowlistFor(resourceType)
	if len(attrs) == 0 {
		t.Fatalf("no allowlisted attributes for %q", resourceType)
	}
	return attrs[0]
}
