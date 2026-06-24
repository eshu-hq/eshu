// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"

	autoscalingservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/autoscaling"
)

// TestAdapterAPIClientForbidsMutationAndCapacityControl is the security
// acceptance gate from issue #830: the Auto Scaling SDK adapter must never be
// able to create, update, or delete an Auto Scaling resource, set desired
// capacity, or terminate an instance in an Auto Scaling group. We reflect over
// the adapter-local apiClient interface and fail the build if any forbidden
// operation becomes reachable.
func TestAdapterAPIClientForbidsMutationAndCapacityControl(t *testing.T) {
	forbiddenExact := []string{
		"SetDesiredCapacity",
		"TerminateInstanceInAutoScalingGroup",
		"SetInstanceHealth",
		"SetInstanceProtection",
		"ExecutePolicy",
		"AttachInstances", "DetachInstances",
		"AttachLoadBalancerTargetGroups", "DetachLoadBalancerTargetGroups",
		"RecordLifecycleActionHeartbeat", "CompleteLifecycleAction",
	}
	// Any method whose name begins with one of these verbs is a write or
	// lifecycle operation and must not exist on the metadata-only adapter.
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Set",
		"Attach", "Detach",
		"Enable", "Disable", "Suspend", "Resume",
		"Start", "Stop", "Execute", "Terminate",
		"Enter", "Exit",
		"Record", "Complete", "Cancel", "Rollback",
		"Tag", "Untag",
		"BatchPut", "BatchDelete",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden mutation/capacity method %q; the Auto Scaling adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Auto Scaling adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the apiClient interface
// is a Describe read so the read surface stays explicit and auditable.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the Auto Scaling read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a Describe read", name)
		}
	}
}

// TestLaunchConfigurationTypeHasNoUserDataField is a structural guard: the
// scanner-owned LaunchConfiguration type must never declare a UserData field
// (or any other launch-detail field), so launch configuration UserData, which
// can hold bootstrap secrets, can never be persisted. A leak would fail to
// compile the production code; this test makes the intent explicit and fails
// loudly if the type ever grows a field beyond identity.
func TestLaunchConfigurationTypeHasNoUserDataField(t *testing.T) {
	typ := reflect.TypeOf(autoscalingservice.LaunchConfiguration{})
	allowed := map[string]struct{}{
		"ARN":  {},
		"Name": {},
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i).Name
		if _, ok := allowed[field]; !ok {
			t.Fatalf("LaunchConfiguration grew field %q; the type must carry identity only so UserData and launch detail can never be persisted", field)
		}
	}
}
