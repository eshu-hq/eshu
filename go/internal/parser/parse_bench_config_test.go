// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// configBenchCase binds one config/manifest sub-benchmark to a deterministic
// synthesized document and the on-disk name the registry dispatches on. Config
// and manifest parsers (Dockerfile, go.mod, HCL, YAML, lockfiles, build files)
// lack a gzip regression corpus, so each case builds a realistic, deterministic
// >= minBenchmarkLOC document instead of reusing a language corpus.
type configBenchCase struct {
	// language is the sub-benchmark name reported by b.Run.
	language string
	// parserKey is the registered Definition.ParserKey this case benchmarks.
	parserKey string
	// fileName is the exact on-disk base name written under b.TempDir(). It MUST
	// match a Definition.ExactNames entry (e.g. "go.mod", "Dockerfile") or carry
	// a registered extension so LookupByPath dispatches to parserKey.
	fileName string
	// build returns a complete, format-valid document of at least minLOC lines.
	// Each builder produces a single valid document (not a naive repeat of a
	// fragment) so the parser measures the normal parse path, never error
	// recovery on malformed input.
	build func(minLOC int) []byte
}

// configBenchCases enumerates the config/manifest parsers benchmarked with
// synthesized input. Each fileName honors the registry's ExactNames or extension
// dispatch (LookupByPath), so the engine routes to the intended parser. The
// coverage guard (TestBenchmarkParseCoversEveryRegisteredParser) fails if a
// registered parser is missing here, in parseBenchCases, and in
// benchExemptParsers.
var configBenchCases = []configBenchCase{
	{language: "dockerfile", parserKey: "__dockerfile__", fileName: "Dockerfile", build: buildDockerfile},
	{language: "jenkinsfile", parserKey: "__jenkinsfile__", fileName: "Jenkinsfile", build: buildJenkinsfile},
	{language: "gomod", parserKey: "gomod", fileName: "go.mod", build: buildGoMod},
	{language: "gradle", parserKey: "gradle", fileName: "build.gradle", build: buildGradle},
	{language: "hcl", parserKey: "hcl", fileName: "main.tf", build: buildHCL},
	{language: "json", parserKey: "json", fileName: "config.json", build: buildJSON},
	{language: "maven", parserKey: "maven", fileName: "pom.xml", build: buildMavenPOM},
	{language: "node_lockfile", parserKey: "node_lockfile", fileName: "yarn.lock", build: buildYarnLock},
	{language: "nuget_project", parserKey: "nuget_project", fileName: "project.csproj", build: buildCsproj},
	{language: "python_requirements", parserKey: "python_requirements", fileName: "requirements.txt", build: buildRequirements},
	{language: "python_toml", parserKey: "python_toml", fileName: "pyproject.toml", build: buildPyproject},
	{language: "yaml", parserKey: "yaml", fileName: "values.yaml", build: buildYAMLValues},
}

// BenchmarkParseConfig reports parse cost (ns/op, B/op, allocs/op), throughput
// (MB/s), and input size (LOC) for the config/manifest parsers that have no
// gzip regression corpus. Each case builds a deterministic, format-valid
// >= 10K-LOC document, writes it under b.TempDir() with the exact name/extension
// the parser expects, and parses it through the shared engine. Inputs are valid
// for their format so ParsePath measures the normal parse path, not error
// recovery.
func BenchmarkParseConfig(b *testing.B) {
	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, tc := range configBenchCases {
		b.Run(tc.language, func(b *testing.B) {
			source := tc.build(minBenchmarkLOC)
			loc := bytes.Count(source, []byte("\n"))
			repoRoot := b.TempDir()
			filePath := filepath.Join(repoRoot, tc.fileName)
			if err := os.WriteFile(filePath, source, 0o644); err != nil {
				b.Fatalf("write %s: %v", filePath, err)
			}

			// Fail loudly if the synthesized name does not dispatch to the parser
			// the case claims to benchmark; a silent mismatch would report another
			// parser's cost under this label.
			def, ok := engine.registry.LookupByPath(filePath)
			if !ok || def.ParserKey != tc.parserKey {
				b.Fatalf("LookupByPath(%s) = %q (ok=%v), want parser key %q",
					tc.fileName, def.ParserKey, ok, tc.parserKey)
			}

			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
					b.Fatalf("ParsePath(%s) error = %v, want nil", tc.language, err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(loc), "LOC")
		})
	}
}

// buildDockerfile returns a multi-stage Dockerfile of >= minLOC lines covering
// the instruction variety (FROM/RUN/COPY/ENV/ARG/WORKDIR/EXPOSE/ENTRYPOINT) the
// dockerfile parser walks. Each repeated stage uses a distinct stage name so the
// document reads as a long, valid multi-stage build.
func buildDockerfile(minLOC int) []byte {
	var b bytes.Buffer
	stage := 0
	for b.Len() == 0 || bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "FROM golang:1.24 AS build%04d\n", stage)
		fmt.Fprintf(&b, "ARG VERSION=1.%d.0\n", stage%10)
		b.WriteString("ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly\n")
		b.WriteString("WORKDIR /src\n")
		b.WriteString("COPY go.mod go.sum ./\n")
		b.WriteString("RUN go mod download\n")
		b.WriteString("COPY . .\n")
		b.WriteString("RUN go build -o /out/app ./cmd/app\n")
		b.WriteString("EXPOSE 8080\n")
		b.WriteString("ENTRYPOINT [\"/out/app\"]\n\n")
		stage++
	}
	return b.Bytes()
}

// buildJenkinsfile returns a declarative-pipeline Groovy document of >= minLOC
// lines. The __jenkinsfile__ definition routes through the groovy grammar, so
// the body is wrapped in a single valid pipeline { ... } block with many stages.
func buildJenkinsfile(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("pipeline {\n    agent any\n    stages {\n")
	stage := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "        stage('Step%04d') {\n", stage)
		b.WriteString("            steps {\n")
		fmt.Fprintf(&b, "                sh 'go test ./pkg/mod%04d/...'\n", stage)
		b.WriteString("            }\n")
		b.WriteString("        }\n")
		stage++
	}
	b.WriteString("    }\n}\n")
	return b.Bytes()
}

// buildGoMod returns a valid go.mod document of >= minLOC lines: a module path,
// a go directive, and a single require ( ... ) block with many entries.
// modfile.Parse rejects malformed go.mod, so the envelope is required for the
// benchmark to measure the normal parse path.
func buildGoMod(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("module example.com/benchmark/app\n\n")
	b.WriteString("go 1.24\n\n")
	b.WriteString("require (\n")
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "\texample.com/mod%06d/pkg v1.%d.%d\n", i, i%10, i%100)
		i++
	}
	b.WriteString(")\n")
	return b.Bytes()
}

// buildGradle returns a Gradle (Groovy DSL) build script of >= minLOC lines: a
// plugins block, a repositories block, and a dependencies block with many
// declarations.
func buildGradle(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("plugins {\n    id 'java'\n    id 'application'\n}\n\n")
	b.WriteString("repositories {\n    mavenCentral()\n}\n\n")
	b.WriteString("dependencies {\n")
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "    implementation 'com.example.group%04d:artifact-%04d:1.%d.0'\n", i, i, i%10)
		i++
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// buildHCL returns a Terraform/HCL document of >= minLOC lines with many
// resource blocks plus interpolation expressions and tags.
func buildHCL(minLOC int) []byte {
	unit := []byte(`resource "aws_instance" "worker_%[1]d" {
  ami           = "ami-0123456789abcdef0"
  instance_type = "t3.medium"

  tags = {
    Name = "worker-%[1]d"
    Env  = "prod"
  }
}

`)
	var b bytes.Buffer
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, string(unit), i)
		i++
	}
	return b.Bytes()
}

// buildJSON returns a single valid JSON array document of >= minLOC lines.
// Repeating a JSON object verbatim would yield invalid JSON, so the document is
// assembled as one array.
func buildJSON(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("[\n")
	i := 0
	for {
		fmt.Fprintf(&b, "  {\n"+
			"    \"id\": %d,\n"+
			"    \"name\": \"package-%06d\",\n"+
			"    \"version\": \"1.%d.%d\",\n"+
			"    \"enabled\": %t\n"+
			"  }", i, i, i%10, i%100, i%2 == 0)
		i++
		// Reserve room for the closing line; stop once the floor is reached.
		if bytes.Count(b.Bytes(), []byte("\n")) >= minLOC {
			b.WriteString("\n")
			break
		}
		b.WriteString(",\n")
	}
	b.WriteString("]\n")
	return b.Bytes()
}

// buildMavenPOM returns a valid Maven POM of >= minLOC lines wrapping many
// <dependency> entries in a single <project><dependencies> envelope. xml.Unmarshal
// rejects multi-root XML, so the envelope is required.
func buildMavenPOM(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<project xmlns="http://maven.apache.org/POM/4.0.0">` + "\n")
	b.WriteString("  <modelVersion>4.0.0</modelVersion>\n")
	b.WriteString("  <groupId>com.example</groupId>\n")
	b.WriteString("  <artifactId>benchmark</artifactId>\n")
	b.WriteString("  <version>1.0.0</version>\n")
	b.WriteString("  <dependencies>\n")
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "    <dependency>\n"+
			"      <groupId>com.example.group%04d</groupId>\n"+
			"      <artifactId>artifact-%04d</artifactId>\n"+
			"      <version>1.%d.0</version>\n"+
			"    </dependency>\n", i, i, i%10)
		i++
	}
	b.WriteString("  </dependencies>\n")
	b.WriteString("</project>\n")
	return b.Bytes()
}

// buildYarnLock returns a yarn.lock document of >= minLOC lines with the
// header/version/resolved/integrity fields the node_lockfile parser walks.
func buildYarnLock(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("# THIS IS AN AUTOGENERATED FILE. DO NOT EDIT THIS FILE DIRECTLY.\n")
	b.WriteString("# yarn lockfile v1\n\n")
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "\"@scope/pkg-%06d@^1.%d.0\":\n"+
			"  version \"1.%d.%d\"\n"+
			"  resolved \"https://registry.example.com/pkg-%06d/-/pkg-1.%d.%d.tgz\"\n"+
			"  integrity sha512-AAAA%06dBBBB==\n\n", i, i%10, i%10, i%100, i, i%10, i%100, i)
		i++
	}
	return b.Bytes()
}

// buildCsproj returns a valid MSBuild .csproj of >= minLOC lines wrapping many
// <PackageReference> entries in a single <Project> envelope. The nuget parser
// decodes via encoding/xml and rejects multi-root XML.
func buildCsproj(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString(`<Project Sdk="Microsoft.NET.Sdk">` + "\n")
	b.WriteString("  <PropertyGroup>\n")
	b.WriteString("    <TargetFramework>net8.0</TargetFramework>\n")
	b.WriteString("  </PropertyGroup>\n")
	b.WriteString("  <ItemGroup>\n")
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "    <PackageReference Include=\"Example.Package%04d\" Version=\"1.%d.0\" />\n", i, i%10)
		i++
	}
	b.WriteString("  </ItemGroup>\n")
	b.WriteString("</Project>\n")
	return b.Bytes()
}

// buildRequirements returns a pip requirements.txt of >= minLOC pinned
// dependency lines; the format is line-oriented so a repeated unit stays valid.
func buildRequirements(minLOC int) []byte {
	var b bytes.Buffer
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "example-package-%06d==1.%d.%d\n", i, i%10, i%100)
		i++
	}
	return b.Bytes()
}

// buildPyproject returns a pyproject.toml of >= minLOC lines built from many
// distinct [tool.sectionNNN] tables; distinct section names keep the TOML valid.
func buildPyproject(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("[build-system]\n")
	b.WriteString("requires = [\"setuptools>=68\"]\n")
	b.WriteString("build-backend = \"setuptools.build_meta\"\n\n")
	b.WriteString("[project]\n")
	b.WriteString("name = \"benchmark\"\n")
	b.WriteString("version = \"1.0.0\"\n\n")
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "[tool.section%06d]\n"+
			"name = \"package-%06d\"\n"+
			"version = \"1.%d.%d\"\n"+
			"dependencies = [\"dep-a>=1.0\", \"dep-b<2.0\"]\n\n", i, i, i%10, i%100)
		i++
	}
	return b.Bytes()
}

// buildYAMLValues returns a single valid Helm values.yaml document of >= minLOC
// lines: scalar knobs plus a deployments map whose many entries (distinct keys)
// keep it one valid YAML document rather than a multi-document stream.
func buildYAMLValues(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("replicaCount: 3\n")
	b.WriteString("image:\n")
	b.WriteString("  repository: registry.example.com/app\n")
	b.WriteString("  tag: \"1.2.3\"\n")
	b.WriteString("  pullPolicy: IfNotPresent\n")
	b.WriteString("deployments:\n")
	i := 0
	for bytes.Count(b.Bytes(), []byte("\n")) < minLOC {
		fmt.Fprintf(&b, "  service%06d:\n"+
			"    enabled: true\n"+
			"    replicas: %d\n"+
			"    image: registry.example.com/svc-%06d:1.%d.%d\n"+
			"    resources:\n"+
			"      limits:\n"+
			"        cpu: 500m\n"+
			"        memory: 512Mi\n", i, i%5+1, i, i%10, i%100)
		i++
	}
	return b.Bytes()
}
