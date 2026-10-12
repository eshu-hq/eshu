// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/openapi"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// workloadContextLimitationEnum reads the closed limitations enum the
// getServiceContext, getWorkloadContext and getWorkloadStory responses are
// documented with, from the assembled OpenAPI document.
func workloadContextLimitationEnum(t *testing.T) []string {
	t.Helper()
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Items struct {
						Enum []string `json:"enum"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(openapi.Spec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(openapi.Spec()): %v", err)
	}
	enum := spec.Components.Schemas["WorkloadContext"].Properties["limitations"].Items.Enum
	if len(enum) == 0 {
		t.Fatal("WorkloadContext.limitations.items.enum is empty or missing")
	}
	return enum
}

// TestWorkloadContextLimitationEnumListsEveryDeploymentArtifactBound keeps
// the documented closed enum in step with what the handlers can emit. The
// emitted set is derived from the reason list the loader appends from, not
// from a copy of the enum, so a new bound reason that is not documented fails
// here; and the values a real response carried above must be in the enum too.
func TestWorkloadContextLimitationEnumListsEveryDeploymentArtifactBound(t *testing.T) {
	t.Parallel()

	enum := workloadContextLimitationEnum(t)
	for _, reason := range querycontract.DeploymentArtifactBoundReasons() {
		if !slices.Contains(enum, reason) {
			t.Errorf("WorkloadContext.limitations enum = %v, missing emitted reason %q", enum, reason)
		}
	}

	data, _ := getOutlierJSON(t,
		boundsHandler(5001, map[string]int{"repo-a": 5000, "repo-b": 5000, "repo-c": 5000, "repo-d": 5001}),
		"/api/v0/services/svc/context", "service_name", boundsService)
	for _, got := range stringSlice(t, data["limitations"]) {
		if !slices.Contains(enum, got) {
			t.Errorf("response limitation %q is not in the WorkloadContext.limitations enum %v", got, enum)
		}
	}
}
