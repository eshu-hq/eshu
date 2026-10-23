// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestBuildRepositoryRuntimeArtifactsExpandsDockerfileBaseTagFromArgDefault(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryRuntimeArtifacts([]querycontract.FileContent{{
		RelativePath: "Dockerfile",
		ArtifactType: "dockerfile",
		Content: `ARG NODE_VERSION=24.1.0
FROM node:${NODE_VERSION}-slim AS app
`,
	}})
	artifacts := got["deployment_artifacts"].([]map[string]any)
	if len(artifacts) != 1 {
		t.Fatalf("deployment_artifacts = %#v, want one", artifacts)
	}
	app := artifacts[0]
	if app["base_image"] != "node" || app["base_tag"] != "24.1.0-slim" {
		t.Fatalf("base = %v:%v, want node:24.1.0-slim", app["base_image"], app["base_tag"])
	}
	if app["base_ref_template"] != "node:${NODE_VERSION}-slim" {
		t.Fatalf("base_ref_template = %#v, want the FROM reference as written", app["base_ref_template"])
	}
	for _, key := range []string{"base_ref_unresolved", "base_ref_unresolved_args", "base_ref_arg_defaults"} {
		if _, ok := app[key]; ok {
			t.Fatalf("resolved row carries %q: %#v", key, app)
		}
	}
}

func TestBuildRepositoryRuntimeArtifactsMarksUnresolvedDockerfileBaseRef(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryRuntimeArtifacts([]querycontract.FileContent{{
		RelativePath: "Dockerfile",
		ArtifactType: "dockerfile",
		Content: `FROM node:${NODE_VERSION}-slim AS app
ARG NODE_VERSION=24.1.0
`,
	}})
	artifacts := got["deployment_artifacts"].([]map[string]any)
	if len(artifacts) != 1 {
		t.Fatalf("deployment_artifacts = %#v, want one", artifacts)
	}
	app := artifacts[0]
	if app["base_tag"] != "${NODE_VERSION}-slim" {
		t.Fatalf("base_tag = %#v, want the placeholder kept", app["base_tag"])
	}
	if app["base_ref_unresolved"] != true {
		t.Fatalf("base_ref_unresolved = %#v, want true", app["base_ref_unresolved"])
	}
	if !reflect.DeepEqual(app["base_ref_unresolved_args"], []string{"NODE_VERSION"}) {
		t.Fatalf("base_ref_unresolved_args = %#v", app["base_ref_unresolved_args"])
	}
	if !reflect.DeepEqual(app["base_ref_arg_defaults"], map[string]string{"NODE_VERSION": "24.1.0"}) {
		t.Fatalf("base_ref_arg_defaults = %#v, want the stage-local default as evidence", app["base_ref_arg_defaults"])
	}
}

func TestBuildRepositoryRuntimeArtifactsLeavesPlainDockerfileRowsUnchanged(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryRuntimeArtifacts([]querycontract.FileContent{{
		RelativePath: "Dockerfile",
		ArtifactType: "dockerfile",
		Content:      "FROM alpine:3.20\n",
	}})
	row := got["deployment_artifacts"].([]map[string]any)[0]
	for _, key := range []string{"base_ref_template", "base_ref_unresolved", "base_ref_unresolved_args", "base_ref_arg_defaults"} {
		if _, ok := row[key]; ok {
			t.Fatalf("plain row carries %q: %#v", key, row)
		}
	}
}

func TestBuildRepositoryRuntimeArtifactsCarriesDockerfileArgDefaultMarkers(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryRuntimeArtifacts([]querycontract.FileContent{{
		RelativePath: "Dockerfile",
		ArtifactType: "dockerfile",
		Content: "FROM node:${V}-slim AS app\nARG V=" + strings.Repeat("9", 1000) + "\n" +
			"ARG A=1\nARG B=2\nARG C=3\nARG D=4\nARG E=5\nARG F=6\nARG G=7\nARG H=8\nARG I=9\n" +
			"ARG J=1\nARG K=2\nARG L=3\nARG M=4\nARG N=5\nARG O=6\nARG P=7\nARG Q=8\n",
	}, {
		RelativePath: "Dockerfile.many",
		ArtifactType: "dockerfile",
		Content: "FROM x:${V}-${A}-${B}-${C}-${D}-${E}-${F}-${G}-${H}-${I}-${J}-${K}-${L}-${M}-${N}-${O}-${P}-${Q}\n" +
			"ARG V=1\nARG A=1\nARG B=2\nARG C=3\nARG D=4\nARG E=5\nARG F=6\nARG G=7\nARG H=8\nARG I=9\n" +
			"ARG J=1\nARG K=2\nARG L=3\nARG M=4\nARG N=5\nARG O=6\nARG P=7\nARG Q=8\n",
	}})
	rows := got["deployment_artifacts"].([]map[string]any)
	byName := map[string]map[string]any{}
	for _, row := range rows {
		byName[row["relative_path"].(string)] = row
	}
	cut := byName["Dockerfile"]
	defaults, _ := cut["base_ref_arg_defaults"].(map[string]string)
	if len(defaults["V"]) == 0 || len(defaults["V"]) >= 1000 {
		t.Fatalf("base_ref_arg_defaults[V] has %d bytes, want the capped prefix", len(defaults["V"]))
	}
	if !reflect.DeepEqual(cut["base_ref_arg_defaults_truncated"], []string{"V"}) {
		t.Fatalf("base_ref_arg_defaults_truncated = %#v, want [V]", cut["base_ref_arg_defaults_truncated"])
	}
	many := byName["Dockerfile.many"]
	if many["base_ref_arg_defaults_omitted"] != true {
		t.Fatalf("base_ref_arg_defaults_omitted = %#v, want true for 18 unresolved names", many["base_ref_arg_defaults_omitted"])
	}
}
