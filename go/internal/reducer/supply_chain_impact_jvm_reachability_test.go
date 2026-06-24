// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsMarksJVMReachableFromParserImport(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-jvm-parser", "CVE-2026-118701", 9.1),
		jvmAffectedPackageRangeFact(
			"affected-jvm-parser",
			"CVE-2026-118701",
			"pkg:maven/org.apache.logging.log4j/log4j-core",
			"org.apache.logging.log4j:log4j-core",
			"[2.0.0,2.17.1)",
			"2.17.1",
		),
		jvmManifestDependencyFact(
			"repo://example/jvm",
			"pom.xml",
			"org.apache.logging.log4j:log4j-core",
			"maven",
			"2.14.1",
			map[string]any{
				"package_api_packages":        []any{"org.apache.logging.log4j"},
				"package_api_identity_source": "maven_resolver",
				"dependency_resolution_state": "resolved",
				"source_set":                  "main",
			},
		),
		jvmParsedFileFact("repo://example/jvm", "src/main/java/example/App.java", map[string]any{
			"lang": "java",
			"imports": []any{
				map[string]any{"source": "org.apache.logging.log4j.Logger"},
			},
		}),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-118701"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.Confidence != "exact" {
		t.Fatalf("Confidence = %q, want exact impact confidence", got.Confidence)
	}
	if got.RuntimeReachability != "jvm_package_api_reachable" {
		t.Fatalf("RuntimeReachability = %q, want jvm_package_api_reachable", got.RuntimeReachability)
	}
	if got.Reachability == nil {
		t.Fatal("Reachability = nil, want JVM envelope")
	}
	if got.Reachability.State != SupplyChainReachabilityReachable {
		t.Fatalf("Reachability.State = %q, want reachable", got.Reachability.State)
	}
	if got.Reachability.Confidence != "partial" {
		t.Fatalf("Reachability.Confidence = %q, want partial", got.Reachability.Confidence)
	}
	if got.Reachability.Source != "jvm_parser_resolver" {
		t.Fatalf("Reachability.Source = %q, want jvm_parser_resolver", got.Reachability.Source)
	}
	assertContainsString(t, got.Reachability.MissingEvidence, "jvm reflection evidence incomplete")
	assertContainsString(t, got.Reachability.MissingEvidence, "jvm dependency-injection evidence incomplete")
}

func TestBuildSupplyChainImpactFindingsMarksJVMReachableFromSCIPEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-jvm-scip", "CVE-2026-118702", 8.4),
		jvmAffectedPackageRangeFact(
			"affected-jvm-scip",
			"CVE-2026-118702",
			"pkg:maven/org.springframework/spring-core",
			"org.springframework:spring-core",
			"[5.3.0,5.3.30)",
			"5.3.30",
		),
		jvmManifestDependencyFact(
			"repo://example/gradle",
			"build.gradle.kts",
			"org.springframework:spring-core",
			"gradle",
			"5.3.20",
			map[string]any{
				"package_api_packages":        []any{"org.springframework.core"},
				"package_api_identity_source": "gradle_resolver",
				"dependency_resolution_state": "resolved",
				"source_set":                  "main",
			},
		),
		jvmParsedFileFact("repo://example/gradle", "src/main/kotlin/example/App.kt", map[string]any{
			"lang": "kotlin",
			"function_calls_scip": []any{
				map[string]any{
					"callee_symbol": "scip-java maven org.springframework/spring-core org.springframework.core/ResolvableType#forClass().",
				},
			},
		}),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-118702"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "jvm_package_api_reachable" {
		t.Fatalf("RuntimeReachability = %q, want jvm_package_api_reachable", got.RuntimeReachability)
	}
	if got.Reachability.State != SupplyChainReachabilityReachable {
		t.Fatalf("Reachability.State = %q, want reachable", got.Reachability.State)
	}
	if got.Reachability.Evidence != "jvm_package_api_reachable" {
		t.Fatalf("Reachability.Evidence = %q, want jvm_package_api_reachable", got.Reachability.Evidence)
	}
}

func TestBuildSupplyChainImpactFindingsKeepsJVMGapsUnknownWithoutAPIIdentity(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-jvm-no-api", "CVE-2026-118703", 7.8),
		jvmAffectedPackageRangeFact(
			"affected-jvm-no-api",
			"CVE-2026-118703",
			"pkg:maven/com.example/misleading-artifact",
			"com.example:misleading-artifact",
			"[1.0.0,1.2.0)",
			"1.2.0",
		),
		jvmManifestDependencyFact(
			"repo://example/no-api",
			"pom.xml",
			"com.example:misleading-artifact",
			"maven",
			"1.1.0",
			map[string]any{
				"dependency_resolution_state": "resolved",
			},
		),
		jvmParsedFileFact("repo://example/no-api", "src/main/java/example/App.java", map[string]any{
			"lang": "java",
			"imports": []any{
				map[string]any{"source": "com.example.MaybeNotFromArtifact"},
			},
		}),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-118703"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability == "jvm_package_api_reachable" {
		t.Fatalf("RuntimeReachability = %q, want unknown without proven API identity", got.RuntimeReachability)
	}
	if got.Reachability.State != SupplyChainReachabilityUnknown {
		t.Fatalf("Reachability.State = %q, want unknown", got.Reachability.State)
	}
	assertContainsString(t, got.Reachability.MissingEvidence, "jvm package API identity evidence missing")
}

func TestBuildSupplyChainImpactFindingsNeverMarksJVMNotCalledWithoutAnalyzer(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-jvm-not-called", "CVE-2026-118704", 7.5),
		jvmAffectedPackageRangeFact(
			"affected-jvm-not-called",
			"CVE-2026-118704",
			"pkg:maven/io.netty/netty-codec-http2",
			"io.netty:netty-codec-http2",
			"[4.1.0.Final,4.1.100.Final)",
			"4.1.100.Final",
		),
		jvmManifestDependencyFact(
			"repo://example/netty",
			"build.gradle",
			"io.netty:netty-codec-http2",
			"gradle",
			"4.1.90.Final",
			map[string]any{
				"package_api_packages":        []any{"io.netty.handler.codec.http2"},
				"package_api_identity_source": "gradle_resolver",
				"dependency_resolution_state": "resolved",
			},
		),
		jvmParsedFileFact("repo://example/netty", "src/main/scala/example/App.scala", map[string]any{
			"lang": "scala",
			"imports": []any{
				map[string]any{"source": "scala.concurrent.Future"},
			},
		}),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-118704"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.Reachability.State == SupplyChainReachabilityNotCalled {
		t.Fatal("Reachability.State = not_called, want unknown without JVM not-called analyzer")
	}
	assertContainsString(t, got.Reachability.MissingEvidence, "jvm parser or SCIP package usage evidence missing")
}

func TestSupplyChainImpactHandlerLoadsActiveJVMReachabilityFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	loader := &manifestBackedSupplyChainImpactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-handler-jvm", "CVE-2026-118705", 8.8),
			jvmAffectedPackageRangeFact(
				"affected-handler-jvm",
				"CVE-2026-118705",
				"pkg:maven/org.apache.logging.log4j/log4j-core",
				"org.apache.logging.log4j:log4j-core",
				"[2.0.0,2.17.1)",
				"2.17.1",
			),
		},
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				"repo://example/handler-jvm",
				"handler-jvm",
				"pom.xml",
				"org.apache.logging.log4j:log4j-core",
				"maven",
				"2.14.1",
				observedAt,
				map[string]any{
					"package_api_packages":        []any{"org.apache.logging.log4j"},
					"package_api_identity_source": "maven_resolver",
					"dependency_resolution_state": "resolved",
				},
			),
		},
		jvmReachabilityFacts: []facts.Envelope{
			jvmParsedFileFact("repo://example/handler-jvm", "src/main/java/example/App.java", map[string]any{
				"lang": "java",
				"imports": []any{
					map[string]any{"source": "org.apache.logging.log4j.Logger"},
				},
			}),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-handler-jvm",
		ScopeID:      "vuln-intel://osv/maven/log4j-core",
		GenerationID: "generation-handler-jvm",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.jvmReachabilityCalls != 1 {
		t.Fatalf("ListActiveJVMReachabilityFacts() calls = %d, want 1", loader.jvmReachabilityCalls)
	}
	if got, want := len(loader.jvmFilters), 1; got != want {
		t.Fatalf("JVM reachability filters = %d, want %d", got, want)
	}
	if got, want := loader.jvmFilters[0].RepositoryIDs, []string{"repo://example/handler-jvm"}; !slices.Equal(got, want) {
		t.Fatalf("JVM reachability repository filter = %v, want %v", got, want)
	}
	if got, want := loader.jvmFilters[0].APIPackages, []string{"org.apache.logging.log4j"}; !slices.Equal(got, want) {
		t.Fatalf("JVM reachability API package filter = %v, want %v", got, want)
	}
	got := writer.write.Findings[0]
	if got.RuntimeReachability != "jvm_package_api_reachable" {
		t.Fatalf("RuntimeReachability = %q, want jvm_package_api_reachable", got.RuntimeReachability)
	}
}

func jvmManifestDependencyFact(
	repositoryID string,
	relativePath string,
	dependencyName string,
	packageManager string,
	dependencyRange string,
	metadata map[string]any,
) facts.Envelope {
	return packageManifestDependencyFactWithMetadata(
		repositoryID,
		"jvm-app",
		relativePath,
		dependencyName,
		packageManager,
		dependencyRange,
		time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
		metadata,
	)
}

func jvmParsedFileFact(repositoryID string, relativePath string, parsed map[string]any) facts.Envelope {
	parsed["path"] = relativePath
	return facts.Envelope{
		FactID:   "file:" + repositoryID + ":" + relativePath,
		FactKind: factKindFile,
		Payload: map[string]any{
			"repo_id":           repositoryID,
			"relative_path":     relativePath,
			"parsed_file_data":  parsed,
			"source_generation": "generation-jvm",
		},
	}
}
