// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestTrustedPRWorkflowDefinitionsKeepNonGateEventSemantics(t *testing.T) {
	defs, err := trustedPRWorkflowDefinitions("../../..")
	if err != nil {
		t.Fatal(err)
	}
	byFile := make(map[string]cigates.PRWorkflowDefinition)
	for _, def := range defs {
		byFile[def.File] = def
	}
	for _, file := range []string{"test.yml", "build.yml", "post_discord_invite.yml", "refresh-cassettes.yml"} {
		if _, ok := byFile[file]; !ok {
			t.Fatalf("missing PR workflow %s", file)
		}
	}
	if !byFile["test.yml"].Synchronize || !byFile["build.yml"].Synchronize {
		t.Fatal("synchronize workflows were not parsed")
	}
	if byFile["post_discord_invite.yml"].Synchronize || byFile["refresh-cassettes.yml"].Synchronize {
		t.Fatal("opened/labeled-only workflows must not join synchronize plan")
	}
	if !reflect.DeepEqual(byFile["build.yml"].Branches, []string{"main"}) {
		t.Fatalf("build workflow lost base branch filter: %v", byFile["build.yml"].Branches)
	}
	if len(byFile["build.yml"].PathsIgnore) == 0 {
		t.Fatal("non-gate build workflow lost paths-ignore")
	}
}

func TestBuildPRPlanWorkflowDefinitionChangeForcesFull(t *testing.T) {
	t.Parallel()
	runner := &endpointRunner{routes: map[string]string{
		"--json headRefOid":   headSHAFixture,
		"--json changedFiles": "1",
		"--json baseRefName":  "main",
		"/pulls/42/files":     `[[{"filename":".github/workflows/test.yml"}]]`,
	}}
	defs := []cigates.PRWorkflowDefinition{
		{File: "test.yml", Name: "Build Test", Synchronize: true, Jobs: []string{"go-core", "go-race-complete"}},
		{File: "opened.yml", Name: "Opened", Synchronize: false, Jobs: []string{"invite"}},
	}
	got, err := buildPRPlan(context.Background(), runner, &cigates.Registry{}, defs, "eshu-hq/eshu", 42, headSHAFixture)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Mode != "full" || got.HeadSHA != headSHAFixture || !reflect.DeepEqual(got.Workflows, []string{"test.yml"}) || !reflect.DeepEqual(got.Jobs["test.yml"], []string{"go-core", "go-race-complete"}) {
		t.Fatalf("plan = %#v", got)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "git/commits") || strings.Contains(call, "graphql") {
			t.Fatalf("definition change tried a transfer probe: %s", call)
		}
	}
}

func TestValidatePRPlanRejectsMissingAndUnsupportedOutputs(t *testing.T) {
	t.Parallel()
	good := prPlan{SchemaVersion: 1, Mode: "full", HeadSHA: headSHAFixture, Workflows: []string{"test.yml"}, Jobs: map[string][]string{"test.yml": {"go-core"}}, Reason: "full"}
	if err := validatePRPlan(good); err != nil {
		t.Fatalf("valid plan: %v", err)
	}
	bad := []prPlan{
		{SchemaVersion: 2, Mode: good.Mode, HeadSHA: good.HeadSHA, Workflows: good.Workflows, Jobs: good.Jobs, Reason: good.Reason},
		{SchemaVersion: 1, Mode: "full", HeadSHA: headSHAFixture, Workflows: []string{}, Jobs: map[string][]string{}, Reason: "full"},
		{SchemaVersion: 1, Mode: "reuse", HeadSHA: headSHAFixture, Workflows: []string{}, Jobs: map[string][]string{}, Reason: "reuse"},
		{SchemaVersion: 1, Mode: "smoke", HeadSHA: headSHAFixture, Workflows: []string{"test.yml"}, Jobs: map[string][]string{}, Reason: "smoke"},
	}
	for _, plan := range bad {
		if err := validatePRPlan(plan); err == nil {
			t.Fatalf("invalid plan accepted: %#v", plan)
		}
	}
}
