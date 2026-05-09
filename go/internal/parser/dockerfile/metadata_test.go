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
COPY --from=builder /out/app /app
LABEL org.opencontainers.image.source="github.com/example/repo"
EXPOSE 8080/tcp
ENTRYPOINT ["/app"]
`

	got := RuntimeMetadata(source)
	if len(got.Stages) != 2 {
		t.Fatalf("Stages len = %d, want 2: %#v", len(got.Stages), got.Stages)
	}
	if got.Stages[0].Name != "alpine" || got.Stages[0].CopiesFrom != "builder" {
		t.Fatalf("final stage = %#v, want alpine copied from builder after sorting", got.Stages[0])
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
