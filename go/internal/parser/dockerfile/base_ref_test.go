// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"reflect"
	"testing"
)

// stageAtLine returns the stage declared on the given Dockerfile line.
func stageAtLine(t *testing.T, metadata Metadata, line int) Stage {
	t.Helper()
	for _, stage := range metadata.Stages {
		if stage.LineNumber == line {
			return stage
		}
	}
	t.Fatalf("no stage on line %d: %#v", line, metadata.Stages)
	return Stage{}
}

func TestBaseRefExpandsGlobalArgDefault(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`ARG NODE_VERSION=24.1.0
FROM node:${NODE_VERSION}-slim AS app
`)
	stage := stageAtLine(t, got, 2)
	if stage.BaseImage != "node" || stage.BaseTag != "24.1.0-slim" {
		t.Fatalf("base = %q:%q, want node:24.1.0-slim", stage.BaseImage, stage.BaseTag)
	}
	if stage.BaseRefTemplate != "node:${NODE_VERSION}-slim" {
		t.Fatalf("BaseRefTemplate = %q, want the FROM reference as written", stage.BaseRefTemplate)
	}
	if len(stage.UnresolvedArgs) != 0 {
		t.Fatalf("UnresolvedArgs = %v, want none", stage.UnresolvedArgs)
	}
	if stage.Alias != "app" || stage.Name != "app" {
		t.Fatalf("alias/name = %q/%q, want app/app", stage.Alias, stage.Name)
	}
}

func TestBaseRefExpandsEveryDockerSubstitutionForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		source    string
		wantImage string
		wantTag   string
	}{
		{"dollar form", "ARG V=3.20\nFROM alpine:$V\n", "alpine", "3.20"},
		{"braced form", "ARG V=3.20\nFROM alpine:${V}\n", "alpine", "3.20"},
		{"whole reference including tag", "ARG BASE=golang:1.24\nFROM $BASE\n", "golang", "1.24"},
		{"whole reference with registry port", "ARG BASE=registry.local:5000/team/app:7\nFROM ${BASE}\n", "registry.local:5000/team/app", "7"},
		{"default operator uses declared value", "ARG V=3.20\nFROM alpine:${V:-edge}\n", "alpine", "3.20"},
		{"default operator on undeclared variable", "FROM alpine:${V:-edge}\n", "alpine", "edge"},
		{"default operator on empty declared value", "ARG V=\nFROM alpine:${V:-edge}\n", "alpine", "edge"},
		{"dash operator keeps empty declared value", "ARG V=\nFROM alpine${V-:edge}\n", "alpine", ""},
		{"alternate operator on set variable", "ARG V=3.20\nFROM alpine:${V:+stable}\n", "alpine", "stable"},
		{"alternate operator on undeclared variable", "FROM alpine${V:+:stable}\n", "alpine", ""},
		{"default word references another arg", "ARG A=3.20\nFROM alpine:${V:-$A}\n", "alpine", "3.20"},
		{"two variables", "ARG N=node\nARG V=24\nFROM ${N}:${V}-slim\n", "node", "24-slim"},
		{"arg default references earlier arg", "ARG A=24\nARG B=${A}.1\nFROM node:${B}\n", "node", "24.1"},
		{"last global declaration wins", "ARG V=1\nARG V=2\nFROM node:${V}\n", "node", "2"},
		{"multi-arg instruction", "ARG A=node B=24\nFROM ${A}:${B}\n", "node", "24"},
		{"digest pin through variable", "ARG IMG=gcr.io/team/img@sha256:abc123\nFROM ${IMG}\n", "gcr.io/team/img@sha256:abc123", ""},
		{"digest pin untouched", "ARG V=1\nFROM gcr.io/team/img@sha256:abc123\n", "gcr.io/team/img@sha256:abc123", ""},
		{"scratch untouched", "ARG V=1\nFROM scratch\n", "scratch", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stages := RuntimeMetadata(tc.source).Stages
			if len(stages) != 1 {
				t.Fatalf("stages = %#v, want one", stages)
			}
			stage := stages[0]
			if stage.BaseImage != tc.wantImage || stage.BaseTag != tc.wantTag {
				t.Fatalf("base = %q:%q, want %q:%q", stage.BaseImage, stage.BaseTag, tc.wantImage, tc.wantTag)
			}
			if len(stage.UnresolvedArgs) != 0 {
				t.Fatalf("UnresolvedArgs = %v, want none", stage.UnresolvedArgs)
			}
		})
	}
}

func TestBaseRefStageLocalArgIsNotVisibleToItsOwnFrom(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`FROM node:${V}-slim AS app
ARG V=24.1.0
RUN echo ${V}
`)
	stage := stageAtLine(t, got, 1)
	if stage.BaseImage != "node" || stage.BaseTag != "${V}-slim" {
		t.Fatalf("base = %q:%q, want the placeholder kept", stage.BaseImage, stage.BaseTag)
	}
	if !reflect.DeepEqual(stage.UnresolvedArgs, []string{"V"}) {
		t.Fatalf("UnresolvedArgs = %v, want [V]", stage.UnresolvedArgs)
	}
	if stage.ArgDefaults["V"] != "24.1.0" {
		t.Fatalf("ArgDefaults = %v, want the stage-local default kept as evidence", stage.ArgDefaults)
	}
	if stage.BaseRefTemplate != "node:${V}-slim" {
		t.Fatalf("BaseRefTemplate = %q", stage.BaseRefTemplate)
	}
}

func TestBaseRefArgFromEarlierStageDoesNotLeakIntoLaterFrom(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`FROM alpine:3.20 AS first
ARG V=1.2.3
FROM node:${V} AS second
`)
	stage := stageAtLine(t, got, 3)
	if stage.BaseTag != "${V}" || !reflect.DeepEqual(stage.UnresolvedArgs, []string{"V"}) {
		t.Fatalf("stage = %#v, want V unresolved because the ARG belongs to the first stage", stage)
	}
}

func TestBaseRefGlobalArgDoesNotNeedRedeclarationForFromButStageArgsStayRaw(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`ARG V=24
FROM node:${V} AS app
ARG V
`)
	if got.Stages[0].BaseTag != "24" {
		t.Fatalf("BaseTag = %q, want 24", got.Stages[0].BaseTag)
	}
	if len(got.Args) != 2 {
		t.Fatalf("Args = %#v, want both ARG instructions kept unchanged", got.Args)
	}
}

func TestBaseRefUnresolvedWhenValueIsNotKnowable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		source       string
		wantTag      string
		wantArgs     []string
		wantDefaults map[string]string
	}{
		{"no default", "ARG V\nFROM node:${V}\n", "${V}", []string{"V"}, nil},
		{"undeclared variable", "FROM node:${V}\n", "${V}", []string{"V"}, nil},
		{"default operator cannot hide a build argument", "ARG V\nFROM node:${V:-lts}\n", "${V:-lts}", []string{"V"}, nil},
		{"partly resolvable keeps whole placeholder", "ARG A=24\nARG B\nFROM node:${A}-${B}\n", "${A}-${B}", []string{"B"}, nil},
		{"pattern expression", "ARG V=24.1.0\nFROM node:${V%%.*}\n", "${V%%.*}", []string{"${V%%.*}"}, nil},
		{"required expression", "ARG V=24\nFROM node:${V:?missing}\n", "${V:?missing}", []string{"${V:?missing}"}, nil},
		{"command substitution is not an arg", "FROM node:$(VERSION)\n", "$(VERSION)", []string{"$(VERSION)"}, nil},
		{"unterminated brace", "ARG V=1\nFROM node:${V\n", "${V", []string{"${V"}, nil},
		{"default that is itself unresolved", "ARG A\nARG B=${A}-x\nFROM node:${B}\n", "${B}", []string{"B"}, map[string]string{"B": "${A}-x"}},
		{"predefined platform arg", "FROM node:${TARGETARCH}\n", "${TARGETARCH}", []string{"TARGETARCH"}, nil},
		{"predefined platform arg default is only a default", "ARG TARGETARCH=amd64\nFROM node:${TARGETARCH}\n", "${TARGETARCH}", []string{"TARGETARCH"}, map[string]string{"TARGETARCH": "amd64"}},
		{"empty result", "ARG V=\nFROM ${V}\n", "", []string{"V"}, map[string]string{"V": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stages := RuntimeMetadata(tc.source).Stages
			if len(stages) != 1 {
				t.Fatalf("stages = %#v, want one", stages)
			}
			stage := stages[0]
			if stage.BaseTag != tc.wantTag {
				t.Fatalf("BaseTag = %q, want %q (image %q)", stage.BaseTag, tc.wantTag, stage.BaseImage)
			}
			if !reflect.DeepEqual(stage.UnresolvedArgs, tc.wantArgs) {
				t.Fatalf("UnresolvedArgs = %#v, want %#v", stage.UnresolvedArgs, tc.wantArgs)
			}
			if !reflect.DeepEqual(stage.ArgDefaults, tc.wantDefaults) {
				t.Fatalf("ArgDefaults = %#v, want %#v", stage.ArgDefaults, tc.wantDefaults)
			}
		})
	}
}

func TestBaseRefKeepsMultiStagePlatformAndNamingBehavior(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`ARG GO=1.24
ARG IMG=gcr.io/team/runtime@sha256:abc123
FROM --platform=$BUILDPLATFORM golang:${GO} AS builder
FROM --platform=linux/amd64 ${IMG}
FROM builder AS final
FROM scratch
`)
	builder := stageAtLine(t, got, 3)
	if builder.BaseImage != "golang" || builder.BaseTag != "1.24" || builder.Platform != "$BUILDPLATFORM" {
		t.Fatalf("builder = %#v, want expanded tag and the raw platform kept", builder)
	}
	pinned := stageAtLine(t, got, 4)
	if pinned.BaseImage != "gcr.io/team/runtime@sha256:abc123" || pinned.BaseTag != "" ||
		pinned.Platform != "linux/amd64" || pinned.Name != "gcr.io/team/runtime@sha256:abc123" {
		t.Fatalf("pinned = %#v, want the digest reference whole and the platform kept", pinned)
	}
	final := stageAtLine(t, got, 5)
	if final.BaseImage != "builder" || final.BaseTag != "" || final.Name != "final" || final.BaseRefTemplate != "" {
		t.Fatalf("final = %#v, want a plain stage reference with no template", final)
	}
	scratch := stageAtLine(t, got, 6)
	if scratch.BaseImage != "scratch" || scratch.BaseRefTemplate != "" {
		t.Fatalf("scratch = %#v", scratch)
	}
}

func TestBaseRefMapEmitsEvidenceOnlyWhenAVariableWasInvolved(t *testing.T) {
	t.Parallel()

	plain := RuntimeMetadata("FROM alpine:3.20\n").Map()["dockerfile_stages"].([]map[string]any)[0]
	for _, key := range []string{"base_ref_template", "base_ref_unresolved_args", "base_ref_arg_defaults"} {
		if _, ok := plain[key]; ok {
			t.Fatalf("plain stage carries %q: %#v", key, plain)
		}
	}

	resolved := RuntimeMetadata("ARG V=3.20\nFROM alpine:${V}\n").Map()["dockerfile_stages"].([]map[string]any)[0]
	if resolved["base_tag"] != "3.20" || resolved["base_ref_template"] != "alpine:${V}" {
		t.Fatalf("resolved = %#v", resolved)
	}
	if _, ok := resolved["base_ref_unresolved_args"]; ok {
		t.Fatalf("resolved stage carries unresolved args: %#v", resolved)
	}

	unresolved := RuntimeMetadata("FROM alpine:${V}\nARG V=3.20\n").Map()["dockerfile_stages"].([]map[string]any)[0]
	if unresolved["base_tag"] != "${V}" ||
		!reflect.DeepEqual(unresolved["base_ref_unresolved_args"], []any{"V"}) ||
		!reflect.DeepEqual(unresolved["base_ref_arg_defaults"], map[string]any{"V": "3.20"}) {
		t.Fatalf("unresolved = %#v", unresolved)
	}
}
