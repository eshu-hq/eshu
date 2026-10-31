// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
)

func TestRunningImageObservationValidation(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	base := RunningImageObservation{
		ProducerClass:         RunningImageProducerKubernetesPod,
		ObservationClass:      RunningImageClassRunningContainer,
		ProducerInstanceID:    "k8s-collector",
		SourceScopeID:         "scope-pods",
		SourceGenerationID:    "generation-1",
		ObjectID:              "pod-uid",
		SlotID:                "container:web",
		ObservedRevision:      "resource-version-1",
		Registry:              "registry.example",
		Repository:            "team/web",
		RuntimeImageReference: "registry.example/team/web:stable",
		RuntimeDigest:         digest,
		RuntimeDigestKind:     RunningImageDigestUnknown,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid Kubernetes running observation: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*RunningImageObservation)
	}{
		{name: "missing immutable digest", mutate: func(o *RunningImageObservation) { o.RuntimeDigest = "" }},
		{name: "mismatched repository", mutate: func(o *RunningImageObservation) { o.RuntimeImageReference = "registry.example/other/web:stable" }},
		{name: "embedded whitespace in reference", mutate: func(o *RunningImageObservation) { o.RuntimeImageReference = "registry.example/team/web:stable token" }},
		{name: "missing slot", mutate: func(o *RunningImageObservation) { o.SlotID = "" }},
		{name: "oversized object identity", mutate: func(o *RunningImageObservation) { o.ObjectID = strings.Repeat("x", 4097) }},
		{name: "oversized update context", mutate: func(o *RunningImageObservation) { o.LastUpdateStatus = strings.Repeat("x", 65) }},
		{name: "wrong class for Kubernetes", mutate: func(o *RunningImageObservation) { o.ObservationClass = RunningImageClassActiveDeployment }},
		{name: "unknown producer", mutate: func(o *RunningImageObservation) { o.ProducerClass = "legacy_fact" }},
		{name: "unsupported digest kind", mutate: func(o *RunningImageObservation) { o.RuntimeDigestKind = "tag" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := base
			test.mutate(&observation)
			if err := observation.Validate(); err == nil {
				t.Fatal("Validate() = nil, want rejection")
			}
		})
	}
}

func TestRunningImageObservationClassesStaySeparate(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	ecs := RunningImageObservation{
		ProducerClass:         RunningImageProducerECSTask,
		ObservationClass:      RunningImageClassRunningContainer,
		ProducerInstanceID:    "ecs-collector",
		SourceScopeID:         "scope-tasks",
		SourceGenerationID:    "generation-1",
		ObjectID:              "task-arn",
		SlotID:                "container:api",
		Registry:              "registry.example",
		Repository:            "team/api",
		RuntimeImageReference: "registry.example/team/api:prod",
		RuntimeDigest:         digest,
		RuntimeDigestKind:     RunningImageDigestManifest,
	}
	if err := ecs.Validate(); err != nil {
		t.Fatalf("valid ECS running observation: %v", err)
	}

	lambda := RunningImageObservation{
		ProducerClass:         RunningImageProducerLambdaFunction,
		ObservationClass:      RunningImageClassActiveDeployment,
		ProducerInstanceID:    "lambda-collector",
		SourceScopeID:         "scope-functions",
		SourceGenerationID:    "generation-1",
		ObjectID:              "function-arn",
		SlotID:                "version:12",
		ObservedRevision:      "last-modified-1",
		Registry:              "registry.example",
		Repository:            "team/api",
		RuntimeImageReference: "registry.example/team/api@" + digest,
		RuntimeDigest:         digest,
		RuntimeDigestKind:     RunningImageDigestManifest,
		LastUpdateStatus:      "in_progress",
	}
	if err := lambda.Validate(); err != nil {
		t.Fatalf("active Lambda deployment with update uncertainty: %v", err)
	}
	lambda.ObservationClass = RunningImageClassRunningContainer
	if err := lambda.Validate(); err == nil {
		t.Fatal("Lambda deployment accepted as a running container")
	}
	lambda.ObservationClass = RunningImageClassActiveDeployment
	lambda.ObservedRevision = ""
	if err := lambda.Validate(); err == nil {
		t.Fatal("Lambda deployment without immutable observed revision accepted")
	}
}
