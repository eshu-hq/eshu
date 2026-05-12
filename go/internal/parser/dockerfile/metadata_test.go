package dockerfile

import (
	"reflect"
	"testing"
)

func TestRuntimeMetadataExtractsStagesAndRuntimeSignals(t *testing.T) {
	t.Parallel()

	source := `FROM golang:1.24 AS builder
ARG TARGETOS=linux
ENV CGO_ENABLED=0

FROM alpine:3.20
COPY --from=gcr.io/go-containerregistry/crane@sha256:abc123 /crane /usr/bin/crane
LABEL org.opencontainers.image.source="github.com/example/repo"
EXPOSE 8080/tcp
ENTRYPOINT ["/app"]
`

	got := RuntimeMetadata(source)
	if len(got.Stages) != 2 {
		t.Fatalf("Stages len = %d, want 2: %#v", len(got.Stages), got.Stages)
	}
	if got.Stages[0].Name != "alpine" ||
		got.Stages[0].CopiesFrom != "gcr.io/go-containerregistry/crane@sha256:abc123" {
		t.Fatalf("final stage = %#v, want alpine copied from digest image after sorting", got.Stages[0])
	}
	if len(got.Args) != 1 || got.Args[0].Name != "TARGETOS" || got.Args[0].DefaultValue != "linux" {
		t.Fatalf("Args = %#v, want TARGETOS default linux", got.Args)
	}
	if len(got.Envs) != 1 || got.Envs[0].Name != "CGO_ENABLED" || got.Envs[0].Stage != "builder" {
		t.Fatalf("Envs = %#v, want CGO_ENABLED on builder", got.Envs)
	}
	if len(got.Ports) != 1 || got.Ports[0].Name != "alpine:8080" || got.Ports[0].Protocol != "tcp" {
		t.Fatalf("Ports = %#v, want alpine:8080/tcp", got.Ports)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "org.opencontainers.image.source" {
		t.Fatalf("Labels = %#v, want OCI source label", got.Labels)
	}
}

func TestRuntimeMetadataMapPreservesPayloadShape(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`FROM alpine:3.20
ARG TARGETOS=linux
ENV CGO_ENABLED=0
EXPOSE 8080
`).Map()
	for _, key := range []string{
		"modules",
		"module_inclusions",
		"dockerfile_stages",
		"dockerfile_ports",
		"dockerfile_args",
		"dockerfile_envs",
		"dockerfile_labels",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("Map() missing key %q: %#v", key, got)
		}
	}
	if _, ok := got["dockerfile_stages"].([]map[string]any); !ok {
		t.Fatalf("dockerfile_stages = %T, want []map[string]any", got["dockerfile_stages"])
	}

	wantStages := []map[string]any{{
		"name":        "alpine",
		"line_number": 1,
		"stage_index": 0,
		"base_image":  "alpine",
		"base_tag":    "3.20",
		"alias":       "",
		"path":        "alpine",
		"lang":        "dockerfile",
	}}
	if !reflect.DeepEqual(got["dockerfile_stages"], wantStages) {
		t.Fatalf("dockerfile_stages = %#v, want %#v", got["dockerfile_stages"], wantStages)
	}

	wantArgs := []map[string]any{{
		"name":          "TARGETOS",
		"line_number":   2,
		"default_value": "linux",
		"stage":         "alpine",
	}}
	if !reflect.DeepEqual(got["dockerfile_args"], wantArgs) {
		t.Fatalf("dockerfile_args = %#v, want %#v", got["dockerfile_args"], wantArgs)
	}

	wantEnvs := []map[string]any{{
		"name":        "CGO_ENABLED",
		"value":       "0",
		"line_number": 3,
		"stage":       "alpine",
	}}
	if !reflect.DeepEqual(got["dockerfile_envs"], wantEnvs) {
		t.Fatalf("dockerfile_envs = %#v, want %#v", got["dockerfile_envs"], wantEnvs)
	}

	wantPorts := []map[string]any{{
		"name":        "alpine:8080",
		"port":        "8080",
		"protocol":    "tcp",
		"line_number": 4,
		"stage":       "alpine",
	}}
	if !reflect.DeepEqual(got["dockerfile_ports"], wantPorts) {
		t.Fatalf("dockerfile_ports = %#v, want %#v", got["dockerfile_ports"], wantPorts)
	}
}

func TestRuntimeMetadataParsesFromPlatformAndRegistryPortTags(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`FROM --platform=$BUILDPLATFORM registry.example.com:5000/team/base:1.2 AS build
FROM alpine@sha256:abc123 AS runtime
`)
	if len(got.Stages) != 2 {
		t.Fatalf("Stages len = %d, want 2: %#v", len(got.Stages), got.Stages)
	}

	byName := stagesByName(got.Stages)
	build := byName["build"]
	if build.BaseImage != "registry.example.com:5000/team/base" || build.BaseTag != "1.2" || build.Platform != "$BUILDPLATFORM" {
		t.Fatalf("build stage = %#v, want image with registry port, tag, and platform", build)
	}

	runtime := byName["runtime"]
	if runtime.BaseImage != "alpine@sha256:abc123" || runtime.BaseTag != "" || runtime.Platform != "" {
		t.Fatalf("runtime stage = %#v, want digest image without tag/platform", runtime)
	}

	rows := RuntimeMetadata(`FROM --platform=$TARGETPLATFORM example.com/app:2.0 AS app`).Map()
	stages := rows["dockerfile_stages"].([]map[string]any)
	if gotPlatform, want := stages[0]["platform"], "$TARGETPLATFORM"; gotPlatform != want {
		t.Fatalf("stage platform = %#v, want %#v", gotPlatform, want)
	}
}

func TestRuntimeMetadataParsesMultipleArgsAndQuotedKeyValues(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`FROM alpine
ARG TARGETOS=linux TARGETARCH
ENV MY_NAME="John Doe" MY_DOG=Rex\ The\ Dog MY_CAT=fluffy
LABEL "com.example.vendor"="ACME Incorporated" description="This text spans words" multi.label=value
`)

	if len(got.Args) != 2 {
		t.Fatalf("Args = %#v, want TARGETOS and TARGETARCH", got.Args)
	}
	args := argsByName(got.Args)
	if args["TARGETOS"].DefaultValue != "linux" || args["TARGETARCH"].Name != "TARGETARCH" {
		t.Fatalf("Args = %#v, want TARGETOS default and TARGETARCH without default", got.Args)
	}

	envs := envsByName(got.Envs)
	if envs["MY_NAME"].Value != "John Doe" || envs["MY_DOG"].Value != "Rex The Dog" || envs["MY_CAT"].Value != "fluffy" {
		t.Fatalf("Envs = %#v, want quoted and escaped values normalized", got.Envs)
	}

	labels := labelsByName(got.Labels)
	if labels["com.example.vendor"].Value != "ACME Incorporated" ||
		labels["description"].Value != "This text spans words" ||
		labels["multi.label"].Value != "value" {
		t.Fatalf("Labels = %#v, want quoted keys and values normalized", got.Labels)
	}
}

func TestRuntimeMetadataParsesLegacyEnvKeyValueForm(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(`FROM alpine
ENV NODE_ENV production
ENV ONE TWO= THREE=world
`)

	envs := envsByName(got.Envs)
	if envs["NODE_ENV"].Value != "production" {
		t.Fatalf("NODE_ENV = %#v, want production", envs["NODE_ENV"])
	}
	if envs["ONE"].Value != "TWO= THREE=world" {
		t.Fatalf("ONE = %#v, want full legacy value", envs["ONE"])
	}
}

func TestRuntimeMetadataHonorsEscapeDirective(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata(
		"# escape=`\n" +
			"FROM mcr.microsoft.com/windows/nanoserver:ltsc2022 AS runtime\n" +
			"ENV APP_HOME=\"C:\\Program Files\\App\" `\n" +
			"    APP_NAME=Rex` The` Dog\n" +
			"LABEL description=\"Windows` label\"\n",
	)

	envs := envsByName(got.Envs)
	if envs["APP_HOME"].Value != `C:\Program Files\App` {
		t.Fatalf("APP_HOME = %#v, want Windows path with backslashes preserved", envs["APP_HOME"])
	}
	if envs["APP_NAME"].Value != "Rex The Dog" {
		t.Fatalf("APP_NAME = %#v, want backtick-escaped spaces", envs["APP_NAME"])
	}

	labels := labelsByName(got.Labels)
	if labels["description"].Value != "Windows label" {
		t.Fatalf("description = %#v, want backtick-escaped label value", labels["description"])
	}
}

func stagesByName(stages []Stage) map[string]Stage {
	result := make(map[string]Stage, len(stages))
	for _, stage := range stages {
		result[stage.Name] = stage
	}
	return result
}

func argsByName(args []Arg) map[string]Arg {
	result := make(map[string]Arg, len(args))
	for _, arg := range args {
		result[arg.Name] = arg
	}
	return result
}

func envsByName(envs []Env) map[string]Env {
	result := make(map[string]Env, len(envs))
	for _, env := range envs {
		result[env.Name] = env
	}
	return result
}

func labelsByName(labels []Label) map[string]Label {
	result := make(map[string]Label, len(labels))
	for _, label := range labels {
		result[label.Name] = label
	}
	return result
}
