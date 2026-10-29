// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestJVMRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"pom.xml":                  "<project><properties><maven.compiler.release>21</maven.compiler.release></properties></project>",
		"build.gradle":             "java { toolchain { languageVersion = JavaLanguageVersion.of(21) } }",
		"build.gradle.kts":         "kotlin { jvmToolchain(21) }",
		"build.sbt":                "scalaVersion := \"3.3.1\"\n",
		"project/build.properties": "sbt.version=1.9.9\npassword=private-token\n",
	} {
		writeCollectorTestFile(t, filepath.Join(root, name), body)
	}
	observed := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.JVMRuntime) != 5 {
		t.Fatalf("JVM handoffs = %d, want 5", len(snapshot.JVMRuntime))
	}
	for _, parsed := range snapshot.FileData {
		if _, ok := parsed["jvm_runtime_evidence"]; ok {
			t.Fatal("private JVM evidence persisted")
		}
		if parsed["language"] == "sbt_properties" {
			if _, ok := parsed["source"]; ok {
				t.Fatal("build.properties body persisted")
			}
		}
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-jvm", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(declarations) != 5 {
		t.Fatalf("runtime declarations = %+v", declarations)
	}
	kinds := map[string]bool{}
	for _, row := range declarations {
		kinds[row.SourceKind] = true
		if row.LineNumber < 1 || row.RawSpec == "private-token" {
			t.Fatalf("unsafe JVM declaration = %+v", row)
		}
	}
	for _, kind := range []string{"pom_maven_compiler_release", "gradle_java_toolchain", "gradle_kotlin_jvm_toolchain", "sbt_scala_version", "sbt_build_properties_version"} {
		if !kinds[kind] {
			t.Errorf("missing %s", kind)
		}
	}
}
