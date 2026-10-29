// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "testing"

func TestJVMManifestRuntimeDeclarations(t *testing.T) {
	for _, tc := range []struct{ path, body, kind, runtime, spec string }{
		{"pom.xml", "<project><properties><maven.compiler.release>21</maven.compiler.release></properties></project>", "pom_maven_compiler_release", "java", "partial"},
		{"build.gradle", "java { toolchain { languageVersion = JavaLanguageVersion.of(21) } }", "gradle_java_toolchain", "java", "partial"},
		{"build.gradle.kts", "kotlin { jvmToolchain(21) }", "gradle_kotlin_jvm_toolchain", "java", "partial"},
		{"build.sbt", "scalaVersion := \"3.3.1\"", "sbt_scala_version", "scala", "exact"},
		{"project/build.properties", "sbt.version=1.9.9\n", "sbt_build_properties_version", "sbt", "exact"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			facts := emitDecoded(t, tc.path, tc.body)
			if len(facts) != 1 || facts[0].SourceKind != tc.kind || facts[0].Runtime != tc.runtime || facts[0].SpecKind != tc.spec {
				t.Fatalf("runtime declarations = %+v", facts)
			}
		})
	}
}

func TestJVMManifestSpecAndFailClosedCases(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, kind string
		facts, rejected        int
		skip                   string
	}{
		{"pom-range", "pom.xml", "<project><properties><java.version>>=17</java.version></properties></project>", "range", 1, 0, ""},
		{"pom-alias", "pom.xml", "<project><properties><java.version>latest</java.version></properties></project>", "alias", 1, 0, ""},
		{"pom-unresolved", "pom.xml", "<project><properties><java.version>${JAVA_VERSION}</java.version></properties></project>", "unresolved", 1, 0, ""},
		{"pom-malformed", "pom.xml", "<project><properties><java.version>21", "", 0, 0, SkipReasonManifestUnparseable},
		{"pom-credential", "pom.xml", "<project><properties><java.version>0123456789abcdef-secret</java.version></properties></project>", "", 0, 1, SkipReasonValueRejected},
		{"gradle-range", "build.gradle", "sourceCompatibility = '>=17 <22'", "range", 1, 0, ""},
		{"gradle-alias", "build.gradle", "sourceCompatibility = 'latest'", "alias", 1, 0, ""},
		{"gradle-unresolved", "build.gradle", "sourceCompatibility = '${JAVA_VERSION}'", "unresolved", 1, 0, ""},
		{"gradle-malformed", "build.gradle", "java { toolchain { languageVersion = JavaLanguageVersion.of(21)", "", 0, 0, SkipReasonManifestUnparseable},
		{"gradle-credential", "build.gradle", "sourceCompatibility = '0123456789abcdef-secret'", "", 0, 1, SkipReasonValueRejected},
		{"sbt-alias", "build.sbt", "scalaVersion := \"latest\"", "alias", 1, 0, ""},
		{"sbt-unresolved", "build.sbt", "scalaVersion := \"${SCALA_VERSION}\"", "unresolved", 1, 0, ""},
		{"sbt-malformed", "build.sbt", "scalaVersion := sys.env(\"SCALA_VERSION\")", "", 0, 0, SkipReasonManifestUnparseable},
		{"sbt-credential", "build.sbt", "scalaVersion := \"0123456789abcdef-secret\"", "", 0, 1, SkipReasonValueRejected},
		{"properties-range", "project/build.properties", "sbt.version=>=1.9 <2", "range", 1, 0, ""},
		{"properties-alias", "project/build.properties", "sbt.version=latest", "alias", 1, 0, ""},
		{"properties-unresolved", "project/build.properties", "sbt.version=${SBT_VERSION}", "unresolved", 1, 0, ""},
		{"properties-malformed", "project/build.properties", "sbt.version=", "", 0, 0, SkipReasonManifestUnparseable},
		{"properties-credential", "project/build.properties", "sbt.version=0123456789abcdef-secret", "", 0, 1, SkipReasonValueRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extraction, ok := Extract(tc.path, tc.body)
			if !ok {
				t.Fatalf("%s not admitted", tc.path)
			}
			facts, skip := EmitExtraction(testContext(tc.path), "repo-1", tc.path, extraction)
			if len(facts) != tc.facts || skip.Rejected != tc.rejected || skip.Reason != tc.skip {
				t.Fatalf("facts=%d skip=%+v, want %d rejected=%d reason=%s", len(facts), skip, tc.facts, tc.rejected, tc.skip)
			}
			if tc.facts == 1 {
				if got := classifyDeclaration(sectionSourceKinds[extraction.declarations[0].section], extraction.declarations[0]).Kind; got != tc.kind {
					t.Fatalf("spec kind=%q, want %q", got, tc.kind)
				}
			}
		})
	}
}
