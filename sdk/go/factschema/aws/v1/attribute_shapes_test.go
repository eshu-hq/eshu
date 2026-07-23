// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "testing"

func TestDecodeResourceEC2VolumeAttributes(t *testing.T) {
	t.Run("valid encrypted and attachments decode identically to today's raw read", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"encrypted": true,
					"attachments": []any{
						map[string]any{"instance_id": "i-1", "state": "attached"},
						map[string]any{"instance_id": "i-2", "state": "detaching"},
					},
				},
			},
		}
		got, err := DecodeResourceEC2VolumeAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Encrypted == nil || !*got.Encrypted {
			t.Fatalf("Encrypted = %v, want true", got.Encrypted)
		}
		if len(got.Attachments) != 2 {
			t.Fatalf("len(Attachments) = %d, want 2", len(got.Attachments))
		}
		if got.Attachments[0].InstanceID != "i-1" || got.Attachments[0].State != "attached" {
			t.Fatalf("Attachments[0] = %+v", got.Attachments[0])
		}
	})

	t.Run("absent encrypted and attachments decode as nil, not an error", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{"attributes": map[string]any{}}}
		got, err := DecodeResourceEC2VolumeAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Encrypted != nil {
			t.Fatalf("Encrypted = %v, want nil", got.Encrypted)
		}
		if got.Attachments != nil {
			t.Fatalf("Attachments = %v, want nil", got.Attachments)
		}
	})

	t.Run("encrypted present as wrong type is a visible decode failure, not a silent nil", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{"encrypted": "yes"},
			},
		}
		_, err := DecodeResourceEC2VolumeAttributes(resource)
		if err == nil {
			t.Fatal("want error for encrypted present as a non-bool string, got nil")
		}
	})

	t.Run("attachments present as a non-array is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{"attachments": "not-a-list"},
			},
		}
		_, err := DecodeResourceEC2VolumeAttributes(resource)
		if err == nil {
			t.Fatal("want error for attachments present as a non-array, got nil")
		}
	})

	t.Run("attachment instance_id present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"attachments": []any{
						map[string]any{"instance_id": 42, "state": "attached"},
					},
				},
			},
		}
		_, err := DecodeResourceEC2VolumeAttributes(resource)
		if err == nil {
			t.Fatal("want error for attachment instance_id present as a non-string, got nil")
		}
	})
}

func TestDecodeResourceKMSKeyAttributes(t *testing.T) {
	t.Run("valid key_manager decodes", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{"attributes": map[string]any{"key_manager": "CUSTOMER"}},
		}
		got, err := DecodeResourceKMSKeyAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.KeyManager != "CUSTOMER" {
			t.Fatalf("KeyManager = %q, want CUSTOMER", got.KeyManager)
		}
	})

	t.Run("absent key_manager decodes as empty string, not an error", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{"attributes": map[string]any{}}}
		got, err := DecodeResourceKMSKeyAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.KeyManager != "" {
			t.Fatalf("KeyManager = %q, want empty", got.KeyManager)
		}
	})

	t.Run("key_manager present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{"attributes": map[string]any{"key_manager": 7}},
		}
		_, err := DecodeResourceKMSKeyAttributes(resource)
		if err == nil {
			t.Fatal("want error for key_manager present as a non-string, got nil")
		}
	})
}

func TestDecodeResourceIAMInstanceProfileAttributes(t *testing.T) {
	t.Run("valid role_arns decodes", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"role_arns": []any{"arn:aws:iam::1:role/a", "arn:aws:iam::1:role/b"},
				},
			},
		}
		got, err := DecodeResourceIAMInstanceProfileAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.RoleARNs) != 2 || got.RoleARNs[0] != "arn:aws:iam::1:role/a" {
			t.Fatalf("RoleARNs = %v", got.RoleARNs)
		}
	})

	t.Run("absent role_arns decodes as nil, not an error", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{"attributes": map[string]any{}}}
		got, err := DecodeResourceIAMInstanceProfileAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.RoleARNs != nil {
			t.Fatalf("RoleARNs = %v, want nil", got.RoleARNs)
		}
	})

	t.Run("role_arns entry present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{"role_arns": []any{"arn:aws:iam::1:role/a", 99}},
			},
		}
		_, err := DecodeResourceIAMInstanceProfileAttributes(resource)
		if err == nil {
			t.Fatal("want error for role_arns entry present as a non-string, got nil")
		}
	})
}

func TestDecodeResourceEC2InstanceAttributes(t *testing.T) {
	t.Run("valid ami_id decodes", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{"attributes": map[string]any{"ami_id": "ami-0000000000000000a"}},
		}
		got, err := DecodeResourceEC2InstanceAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.AMIID != "ami-0000000000000000a" {
			t.Fatalf("AMIID = %q, want ami-0000000000000000a", got.AMIID)
		}
	})

	t.Run("absent ami_id decodes as empty string, not an error", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{"attributes": map[string]any{}}}
		got, err := DecodeResourceEC2InstanceAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.AMIID != "" {
			t.Fatalf("AMIID = %q, want empty", got.AMIID)
		}
	})

	t.Run("ami_id present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{"attributes": map[string]any{"ami_id": 7}},
		}
		_, err := DecodeResourceEC2InstanceAttributes(resource)
		if err == nil {
			t.Fatal("want error for ami_id present as a non-string, got nil")
		}
	})
}

func TestDecodeRelationshipCloudWatchAlarmObservesMetricAttributes(t *testing.T) {
	t.Run("valid dimensions decode", func(t *testing.T) {
		rel := Relationship{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"dimensions": []any{
						map[string]any{"name": "InstanceId", "value": "i-1"},
					},
				},
			},
		}
		got, err := DecodeRelationshipCloudWatchAlarmObservesMetricAttributes(rel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Dimensions) != 1 || got.Dimensions[0].Value != "i-1" {
			t.Fatalf("Dimensions = %+v", got.Dimensions)
		}
	})

	t.Run("absent dimensions decodes as nil, not an error", func(t *testing.T) {
		rel := Relationship{Attributes: map[string]any{"attributes": map[string]any{}}}
		got, err := DecodeRelationshipCloudWatchAlarmObservesMetricAttributes(rel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Dimensions != nil {
			t.Fatalf("Dimensions = %v, want nil", got.Dimensions)
		}
	})

	t.Run("dimension value present as wrong type is a visible decode failure", func(t *testing.T) {
		rel := Relationship{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"dimensions": []any{
						map[string]any{"name": "InstanceId", "value": 12345},
					},
				},
			},
		}
		_, err := DecodeRelationshipCloudWatchAlarmObservesMetricAttributes(rel)
		if err == nil {
			t.Fatal("want error for dimension value present as a non-string, got nil")
		}
	})

	t.Run("dimensions present as a non-array is a visible decode failure", func(t *testing.T) {
		rel := Relationship{
			Attributes: map[string]any{"attributes": map[string]any{"dimensions": "not-a-list"}},
		}
		_, err := DecodeRelationshipCloudWatchAlarmObservesMetricAttributes(rel)
		if err == nil {
			t.Fatal("want error for dimensions present as a non-array, got nil")
		}
	})
}

func TestDecodeRelationshipXRaySamplingRuleMatchesServiceAttributes(t *testing.T) {
	t.Run("valid service_name decodes", func(t *testing.T) {
		rel := Relationship{
			Attributes: map[string]any{"attributes": map[string]any{"service_name": "checkout"}},
		}
		got, err := DecodeRelationshipXRaySamplingRuleMatchesServiceAttributes(rel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ServiceName != "checkout" {
			t.Fatalf("ServiceName = %q, want checkout", got.ServiceName)
		}
	})

	t.Run("service_name present as wrong type is a visible decode failure", func(t *testing.T) {
		rel := Relationship{
			Attributes: map[string]any{"attributes": map[string]any{"service_name": []any{"a", "b"}}},
		}
		_, err := DecodeRelationshipXRaySamplingRuleMatchesServiceAttributes(rel)
		if err == nil {
			t.Fatal("want error for service_name present as a non-string, got nil")
		}
	})
}

func TestDecodeResourceAnchorAttributes(t *testing.T) {
	t.Run("scalar and slice workload/service anchors merge like today's payloadStrings union", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"workload_id":   "wl-1",
				"workload_ids":  []any{"wl-2"},
				"service_name":  "svc-a",
				"service_names": []any{"svc-b"},
				"environment":   "prod",
			},
		}
		got, err := DecodeResourceAnchorAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.WorkloadIDs) != 2 || got.WorkloadIDs[0] != "wl-1" || got.WorkloadIDs[1] != "wl-2" {
			t.Fatalf("WorkloadIDs = %v", got.WorkloadIDs)
		}
		if len(got.ServiceNames) != 2 || got.ServiceNames[0] != "svc-a" || got.ServiceNames[1] != "svc-b" {
			t.Fatalf("ServiceNames = %v", got.ServiceNames)
		}
		if got.Environment != "prod" {
			t.Fatalf("Environment = %q, want prod", got.Environment)
		}
	})

	t.Run("absent anchor fields decode as zero values, not an error", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{}}
		got, err := DecodeResourceAnchorAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.WorkloadIDs != nil || got.ServiceNames != nil || got.Environment != "" {
			t.Fatalf("got = %+v, want all zero", got)
		}
	})

	t.Run("environment present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{"environment": 3}}
		_, err := DecodeResourceAnchorAttributes(resource)
		if err == nil {
			t.Fatal("want error for environment present as a non-string, got nil")
		}
	})

	t.Run("workload_ids present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{"workload_ids": "not-a-list"}}
		_, err := DecodeResourceAnchorAttributes(resource)
		if err == nil {
			t.Fatal("want error for workload_ids present as a non-array, got nil")
		}
	})
}

func TestDecodeResourceNestedAnchorAttributes(t *testing.T) {
	t.Run("valid nested service_names decodes", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{"service_names": []any{"svc-a", "svc-b"}},
			},
		}
		got, err := DecodeResourceNestedAnchorAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.ServiceNames) != 2 {
			t.Fatalf("ServiceNames = %v", got.ServiceNames)
		}
	})

	t.Run("nested service_name present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{"attributes": map[string]any{"service_name": 5}},
		}
		_, err := DecodeResourceNestedAnchorAttributes(resource)
		if err == nil {
			t.Fatal("want error for nested service_name present as a non-string, got nil")
		}
	})
}
