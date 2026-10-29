// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import "testing"

func TestRuntimeEvidenceIgnoresCommentAndStringLookalikes(t *testing.T) {
	source := []byte("// sourceCompatibility = '8'\n/* languageVersion = JavaLanguageVersion.of(11) */\nval note = \"sourceCompatibility = '13'\"\njava { toolchain { languageVersion = JavaLanguageVersion.of(21) } }\n")
	evidence := ExtractRuntimeEvidence(source)
	if len(evidence.Values) != 1 || evidence.Values[0].Value != "21" || evidence.Values[0].Line != 4 {
		t.Fatalf("runtime evidence = %+v", evidence)
	}
}

func TestRuntimeEvidenceReadsKotlinSetAndTargetCompatibility(t *testing.T) {
	source := []byte("java { toolchain { languageVersion.set(JavaLanguageVersion.of(21)) } }\ntargetCompatibility = JavaVersion.VERSION_17\n")
	evidence := ExtractRuntimeEvidence(source)
	if len(evidence.Values) != 2 || evidence.Values[0].SourceKind != "gradle_java_toolchain" || evidence.Values[1].SourceKind != "gradle_java_target_compatibility" || evidence.Values[1].Value != "17" {
		t.Fatalf("runtime evidence = %+v", evidence)
	}
}

func TestRuntimeEvidenceDoesNotPromoteExpressionPrefixesOrConditionalBlocks(t *testing.T) {
	source := []byte("def sourceCompatibility = '17'\nsourceCompatibility = '21' + suffix\nif (flag) { java { toolchain { languageVersion = JavaLanguageVersion.of(19) } } }\njava { toolchain { languageVersion = JavaLanguageVersion.of(21) } }\n")
	evidence := ExtractRuntimeEvidence(source)
	if len(evidence.Values) != 1 || evidence.Values[0].SourceKind != "gradle_java_toolchain" || evidence.Values[0].Value != "21" || evidence.Values[0].Line != 4 {
		t.Fatalf("runtime evidence = %+v", evidence)
	}
}
