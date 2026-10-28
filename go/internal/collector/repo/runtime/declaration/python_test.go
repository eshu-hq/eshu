// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

func TestPythonTOMLSourceKindsClassifySpecShapes(t *testing.T) {
	sources := []struct{ path, prefix, suffix, kind string }{
		{"pyproject.toml", "[project]\nrequires-python = \"", "\"\n", "pyproject_requires_python"},
		{"pyproject.toml", "[tool.poetry.dependencies]\npython = \"", "\"\n", "poetry_python"},
		{"Pipfile", "[requires]\npython_version = \"", "\"\n", "pipfile_python_version"},
		{"Pipfile", "[requires]\npython_full_version = \"", "\"\n", "pipfile_python_full_version"},
	}
	specs := []struct{ value, kind string }{
		{"3.12.1", "exact"},
		{">=3.10", "range"},
		{"latest", "alias"},
		{"${PYTHON_VERSION}", "unresolved"},
	}
	for _, source := range sources {
		for _, spec := range specs {
			t.Run(source.kind+"/"+spec.kind, func(t *testing.T) {
				facts := emitDecoded(t, source.path, source.prefix+spec.value+source.suffix)
				if len(facts) != 1 || facts[0].SourceKind != source.kind || facts[0].RawSpec != spec.value || facts[0].SpecKind != spec.kind || facts[0].Runtime != "python" {
					t.Fatalf("facts = %+v", facts)
				}
			})
		}
	}
}

func TestPythonTOMLRejectsMalformedAndCredentialValues(t *testing.T) {
	for _, source := range []struct{ path, prefix, suffix string }{
		{"pyproject.toml", "[project]\nrequires-python = \"", "\"\n"},
		{"pyproject.toml", "[tool.poetry.dependencies]\npython = \"", "\"\n"},
		{"Pipfile", "[requires]\npython_version = \"", "\"\n"},
		{"Pipfile", "[requires]\npython_full_version = \"", "\"\n"},
	} {
		for _, tc := range []struct {
			name, body, reason string
			rejected           int
		}{
			{"malformed", source.prefix + "3.12" + source.suffix + "invalid = [\n", SkipReasonManifestUnparseable, 0},
			{"credential", source.prefix + "0123456789abcdef-secret" + source.suffix, SkipReasonValueRejected, 1},
		} {
			t.Run(source.path+"/"+tc.name, func(t *testing.T) {
				facts, skip := EmitFile(testContext(source.path), "repo-1", source.path, tc.body)
				if len(facts) != 0 || skip.Reason != tc.reason || skip.Rejected != tc.rejected {
					t.Fatalf("facts = %d, skip = %+v", len(facts), skip)
				}
			})
		}
	}
}

func TestPythonSetupCFGSpecShapes(t *testing.T) {
	for _, spec := range []struct{ raw, kind string }{
		{"3.12.1", "exact"},
		{">=3.10", "range"},
		{"latest", "alias"},
		{"${PYTHON_VERSION}", "unresolved"},
	} {
		t.Run(spec.kind, func(t *testing.T) {
			facts := emitDecoded(t, "setup.cfg", "[metadata]\nname = example\n[options]\npython_requires = "+spec.raw+"\n")
			if len(facts) != 1 || facts[0].SourceKind != "setup_cfg_python_requires" || facts[0].RawSpec != spec.raw || facts[0].SpecKind != spec.kind || facts[0].LineNumber != 4 {
				t.Fatalf("facts = %+v", facts)
			}
		})
	}
}

func TestPythonSetupCFGSkipsMalformedAndCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		rejected           int
	}{
		{"malformed", "[options\npython_requires = 3.12\n", SkipReasonManifestUnparseable, 0},
		{"duplicate", "[options]\npython_requires = 3.12\npython_requires = 3.13\n", SkipReasonManifestUnparseable, 0},
		{"credential", "[options]\npython_requires = 0123456789abcdef-secret\n", SkipReasonValueRejected, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts, skip := EmitFile(testContext("setup.cfg"), "repo-1", "setup.cfg", tc.body)
			if len(facts) != 0 || skip.Reason != tc.reason || skip.Rejected != tc.rejected {
				t.Fatalf("facts = %d, skip = %+v", len(facts), skip)
			}
		})
	}
}

func TestPythonRuntimeTXTSpecShapes(t *testing.T) {
	for _, spec := range []struct{ raw, kind string }{
		{"3.12.1", "exact"},
		{">=3.10", "range"},
		{"latest", "alias"},
		{"${PYTHON_VERSION}", "unresolved"},
	} {
		t.Run(spec.kind, func(t *testing.T) {
			facts := emitDecoded(t, "runtime.txt", "python-"+spec.raw+"\n")
			if len(facts) != 1 || facts[0].SourceKind != "runtime_txt" || facts[0].RawSpec != "python-"+spec.raw || facts[0].SpecKind != spec.kind {
				t.Fatalf("facts = %+v", facts)
			}
		})
	}
}

func TestPythonRuntimeTXTSkipsMalformedAndCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		rejected           int
	}{
		{"malformed", "ruby-3.2.0\n", SkipReasonManifestUnparseable, 0},
		{"credential", "python-0123456789abcdef-secret\n", SkipReasonValueRejected, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts, skip := EmitFile(testContext("runtime.txt"), "repo-1", "runtime.txt", tc.body)
			if len(facts) != 0 || skip.Reason != tc.reason || skip.Rejected != tc.rejected {
				t.Fatalf("facts = %d, skip = %+v", len(facts), skip)
			}
		})
	}
}

func TestPythonSetupPYSpecShapes(t *testing.T) {
	for _, spec := range []struct{ raw, kind string }{
		{"3.12.1", "exact"},
		{">=3.10", "range"},
		{"latest", "alias"},
		{"${PYTHON_VERSION}", "unresolved"},
	} {
		t.Run(spec.kind, func(t *testing.T) {
			body := "from setuptools import setup\nsetup(python_requires='" + spec.raw + "')\n"
			facts := emitDecoded(t, "setup.py", body)
			if len(facts) != 1 || facts[0].SourceKind != "setup_py_python_requires" || facts[0].RawSpec != spec.raw || facts[0].SpecKind != spec.kind || facts[0].LineNumber != 2 {
				t.Fatalf("facts = %+v", facts)
			}
		})
	}
}

func TestPythonSetupPYSkipsMalformedAndCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		rejected           int
	}{
		{"malformed", "from setuptools import setup\nsetup(python_requires='3.12'\n", SkipReasonManifestUnparseable, 0},
		{"credential", "from setuptools import setup\nsetup(python_requires='0123456789abcdef-secret')\n", SkipReasonValueRejected, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts, skip := EmitFile(testContext("setup.py"), "repo-1", "setup.py", tc.body)
			if len(facts) != 0 || skip.Reason != tc.reason || skip.Rejected != tc.rejected {
				t.Fatalf("facts = %d, skip = %+v", len(facts), skip)
			}
		})
	}
}

func TestPythonSetupCFGSectionNamesAreCaseSensitive(t *testing.T) {
	if got := emitDecoded(t, "setup.cfg", "[Options]\npython_requires = 3.12\n"); len(got) != 0 {
		t.Fatalf("non-options section emitted %+v", got)
	}
	if got := emitDecoded(t, "setup.cfg", "[ options ]\npython_requires = 3.12\n"); len(got) != 0 {
		t.Fatalf("space-padded section emitted %+v", got)
	}
	got := emitDecoded(t, "setup.cfg", "[Options]\npython_requires = 3.12\n[options]\npython_requires = 3.11\n")
	if len(got) != 1 || got[0].RawSpec != "3.11" || got[0].LineNumber != 4 {
		t.Fatalf("case-distinct options sections emitted %+v", got)
	}
}

func TestPythonSetupPYOverTokenCapSkipsWholeFile(t *testing.T) {
	var body strings.Builder
	body.WriteString("from setuptools import setup\n")
	for i := 0; i < MaxTokensPerFile+1; i++ {
		body.WriteString("setup(python_requires='3.12')\n")
	}
	facts, skip := EmitFile(testContext("setup.py"), "repo-1", "setup.py", body.String())
	if len(facts) != 0 || skip.Reason != SkipReasonTokenCap || skip.TokenCount != MaxTokensPerFile+1 || skip.ByteSize != body.Len() {
		t.Fatalf("over-token setup.py emitted %d facts, skip %+v", len(facts), skip)
	}
}
