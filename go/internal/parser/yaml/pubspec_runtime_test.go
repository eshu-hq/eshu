// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestPubspecEnvironmentSDKRuntimeEvidence(t *testing.T) {
	t.Parallel()

	payload := parsePubspecFixture(t, "pubspec.yaml", "name: demo\nenvironment:\n  sdk: '>=3.0.0 <4.0.0'\n")
	evidence, ok := payload["dart_runtime_evidence"].(shared.RuntimeEvidence)
	if !ok || len(evidence.Values) != 1 {
		t.Fatalf("runtime evidence = %#v", payload["dart_runtime_evidence"])
	}
	got := evidence.Values[0]
	if got.SourceKind != "dart_pubspec_environment_sdk" || got.Runtime != "dart" || got.Value != ">=3.0.0 <4.0.0" || got.Line != 3 {
		t.Fatalf("value = %+v", got)
	}
}

func TestPubspecRuntimeEvidenceAbsentStaysEmpty(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, filename, body string }{
		{"no environment", "pubspec.yaml", "name: demo\ndependencies:\n  http: ^1.2.0\n"},
		{"no sdk", "pubspec.yaml", "name: demo\nenvironment:\n  flutter: '>=3.0.0'\n"},
		{"yml manifest", "pubspec.yml", "name: demo\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := parsePubspecFixture(t, tc.filename, tc.body)
			evidence, ok := payload["dart_runtime_evidence"].(shared.RuntimeEvidence)
			if !ok || len(evidence.Values) != 0 || evidence.State != "parsed_empty" {
				t.Fatalf("evidence = %#v", payload["dart_runtime_evidence"])
			}
		})
	}
}

func TestPubspecLockCarriesNoRuntimeEvidence(t *testing.T) {
	t.Parallel()

	payload := parsePubspecFixture(t, "pubspec.lock", "packages:\n  http:\n    version: \"1.2.2\"\n")
	if _, found := payload["dart_runtime_evidence"]; found {
		t.Fatalf("lock file carries runtime evidence: %#v", payload["dart_runtime_evidence"])
	}
}

func TestPubspecRuntimeEvidenceRejectsCredential(t *testing.T) {
	t.Parallel()

	payload := parsePubspecFixture(t, "pubspec.yaml", "name: demo\nenvironment:\n  sdk: 'https://user:password@example.invalid'\n")
	evidence, ok := payload["dart_runtime_evidence"].(shared.RuntimeEvidence)
	if !ok || len(evidence.Values) != 0 || evidence.Rejected != 1 {
		t.Fatalf("evidence = %#v", payload["dart_runtime_evidence"])
	}
}
