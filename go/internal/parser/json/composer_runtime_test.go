// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestComposerRuntimeEvidenceDigestBindsAcceptedAndMalformedSource(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantError  bool
	}{
		{"accepted", `{"require":{"php":"8.2"}}`, false},
		{"malformed", `{"require":{"php":"8.2",}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence, err := ExtractComposerRuntimeEvidence("composer.json", []byte(tc.body))
			if (err != nil) != tc.wantError {
				t.Fatalf("parse error = %v, want error %v", err, tc.wantError)
			}
			want := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.body)))
			if evidence.Digest != want {
				t.Fatalf("source digest = %q, want SHA-256 %q", evidence.Digest, want)
			}
		})
	}
}

func TestComposerRuntimeEvidenceLastWinsWithRealLine(t *testing.T) {
	const body = "{\n  \"require\": {\"php\": \"7.4\"},\n  \"require\": {\"php\": \"^8.2\"}\n}"
	got, err := ExtractComposerRuntimeEvidence("composer.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Values) != 1 || got.Values[0].SourceKind != "composer_json_require_php" || got.Values[0].Value != "^8.2" || got.Values[0].Line != 3 {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestComposerParserSharesPHPValueAndLine(t *testing.T) {
	body := "{\n  \"require\": {\"php\": \"7.4\"},\n  \"require\": {\"php\": \"^8.2\"}\n}"
	path := filepath.Join(t.TempDir(), "composer.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := payload["composer_runtime_evidence"].(ComposerRuntimeEvidence)
	if !ok || len(evidence.Values) != 1 {
		t.Fatalf("evidence = %+v", evidence)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows = %+v", payload["variables"])
	}
	if rows[0]["value"] != evidence.Values[0].Value || rows[0]["line_number"] != evidence.Values[0].Line {
		t.Fatalf("dependency row %+v differs from runtime evidence %+v", rows[0], evidence.Values[0])
	}
}

func TestComposerParserUsesOriginalLineAfterNormalizedPrefix(t *testing.T) {
	const body = "{{ generated }}\n\n\n{\n  \"require\": {\"php\": \"^8.2\"}\n}"
	path := filepath.Join(t.TempDir(), "composer.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := payload["composer_runtime_evidence"].(ComposerRuntimeEvidence)
	if !ok || len(evidence.Values) != 1 || evidence.Values[0].Line != 5 {
		t.Fatalf("original source line not preserved: %+v", evidence)
	}
	legacy, err := ExtractComposerRuntimeEvidence("composer.json", []byte(body))
	if err != nil || len(legacy.Values) != 1 || legacy.Values[0].Line != 5 {
		t.Fatalf("legacy evidence = %+v, err = %v", legacy, err)
	}
}

func TestComposerRuntimeEvidenceCanonicalizesWhitespace(t *testing.T) {
	const body = `{"require":{"php":" ^8.2 "}}`
	got, err := ExtractComposerRuntimeEvidence("composer.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Values) != 1 || got.Values[0].Value != "^8.2" {
		t.Fatalf("evidence = %+v, want one trimmed PHP spec", got)
	}
}

func TestComposerRuntimeEvidenceDropsCredentialBeforeHandoff(t *testing.T) {
	const secret = "0123456789abcdef-secret"
	got, err := ExtractComposerRuntimeEvidence("composer.json", []byte(`{"require":{"php":"`+secret+`"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Values) != 0 || got.Rejected != 1 {
		t.Fatalf("evidence retained credential: %+v", got)
	}
	if strings.Contains(fmt.Sprint(got), secret) {
		t.Fatal("credential leaked through typed evidence")
	}
}

func TestComposerParserDoesNotPersistRejectedPHPValue(t *testing.T) {
	const secret = "0123456789abcdef-secret"
	path := filepath.Join(t.TempDir(), "composer.json")
	if err := os.WriteFile(path, []byte(`{"require":{"php":"`+secret+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(payload["variables"]), secret) {
		t.Fatal("rejected PHP value entered parsed dependency rows")
	}
}
