// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/pythondep"
)

func TestEnginePythonRuntimeEvidenceBindsOriginalBareCRBytes(t *testing.T) {
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, body string }{
		{"pyproject.toml", "[project]\rrequires-python = '3.12'\r"},
		{"Pipfile", "[requires]\rpython_version = '3.12'\r"},
		{"setup.py", "from setuptools import setup\rsetup(python_requires='3.12')\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, tc.name)
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			parsed, err := engine.ParsePath(root, path, false, Options{})
			if err != nil {
				t.Fatal(err)
			}
			evidence, ok := parsed["python_runtime_evidence"].(pythondep.PythonRuntimeEvidence)
			if !ok || evidence.Digest != pythondep.PythonRuntimeDigest([]byte(tc.body)) || len(evidence.Values) != 1 {
				t.Fatalf("original-byte evidence = %+v", evidence)
			}
		})
	}
}
