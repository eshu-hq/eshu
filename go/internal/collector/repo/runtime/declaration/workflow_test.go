// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

const workflowSetupInputs = `name: ci
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-node@v4
        with:
          node-version: 20
      - uses: actions/setup-python@v5
        with:
          python-version: "3.11"
      - uses: actions/setup-go@v5
        with:
          go-version: "${{ inputs.go-version }}"
  call:
    uses: ./.github/workflows/reusable.yml
    with:
      node-version: "22"
      node-version-file: .nvmrc
`

func TestEmitWorkflowSetupVersionInputs(t *testing.T) {
	t.Parallel()

	got := emitDecoded(t, ".github/workflows/ci.yml", workflowSetupInputs)
	checkFacts(t, got, []wantFact{
		{".github/workflows", "workflow_setup_version", "node", "<nil>", "20", "partial", "<nil>", 9, 0, 20, -1, -1},
		{".github/workflows", "workflow_setup_version", "python", "<nil>", "3.11", "partial", "<nil>", 12, 0, 3, 11, -1},
		{".github/workflows", "workflow_setup_version", "go", "<nil>", "${{ inputs.go-version }}", "unresolved", "<nil>", 15, 0, -1, -1, -1},
		{".github/workflows", "workflow_setup_version", "node", "<nil>", "22", "partial", "<nil>", 19, 0, 22, -1, -1},
		{".github/workflows", "workflow_setup_version", "node", "<nil>", ".nvmrc", "alias", "<nil>", 20, 0, -1, -1, -1},
	})
}

func TestEmitWorkflowSetupVersionClassifiesValues(t *testing.T) {
	t.Parallel()

	withBlock := func(key, value string) string {
		return "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          " + key + ": " + value + "\n"
	}
	cases := []struct {
		name, key, value, wantRaw, wantKind, wantRuntime string
		major, minor, patch                              int
	}{
		{"exact", "node-version", `"20.11.1"`, "20.11.1", SpecKindExact, "node", 20, 11, 1},
		{"partial", "node-version", `20`, "20", SpecKindPartial, "node", 20, -1, -1},
		{"range", "node-version", `">=18"`, ">=18", SpecKindRange, "node", -1, -1, -1},
		{"channel alias", "node-version", `"lts/*"`, "lts/*", SpecKindAlias, "node", -1, -1, -1},
		{"expression is unresolved", "node-version", `"${{ matrix.node }}"`, "${{ matrix.node }}", SpecKindUnresolved, "node", -1, -1, -1},
		{"shell placeholder is unresolved", "node-version", `"${NODE_VERSION}"`, "${NODE_VERSION}", SpecKindUnresolved, "node", -1, -1, -1},
		{"key prefix names the runtime", "python-version", `"3.11"`, "3.11", SpecKindPartial, "python", 3, 11, -1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := emitDecoded(t, ".github/workflows/ci.yml", withBlock(tc.key, tc.value))
			checkFacts(t, got, []wantFact{
				{".github/workflows", "workflow_setup_version", tc.wantRuntime, "<nil>", tc.wantRaw, tc.wantKind, "<nil>", 7, 0, tc.major, tc.minor, tc.patch},
			})
		})
	}
}

func TestEmitWorkflowSetupVersionReadsAnyVersionSuffixedInput(t *testing.T) {
	t.Parallel()

	// The runtime is the key prefix, kept open like a .tool-versions tool, so
	// a setup action for another runtime reads without a code change.
	body := "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-java@v4\n        with:\n          java-version: \"17\"\n          distribution: temurin\n"
	got := emitDecoded(t, ".github/workflows/ci.yml", body)
	checkFacts(t, got, []wantFact{
		{".github/workflows", "workflow_setup_version", "java", "<nil>", "17", "partial", "<nil>", 7, 0, 17, -1, -1},
	})
}

func TestEmitWorkflowSetupVersionIgnoresNonVersionInputs(t *testing.T) {
	t.Parallel()

	// Only with: keys ending in -version (and the version-file pointer)
	// declare a runtime. Checkout repositories, trigger inputs, run scripts
	// and environment never emit, and a workflow_call input definition is not
	// a declaration either.
	body := `on:
  workflow_call:
    inputs:
      node-version:
        type: string
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      node-version: "20"
    steps:
      - uses: actions/checkout@v4
        with:
          repository: acme/app
      - run: echo "${{ inputs.node-version }}"
`
	if got := emitDecoded(t, ".github/workflows/ci.yml", body); len(got) != 0 {
		t.Fatalf("emitted %+v, want 0", got)
	}
}

func TestEmitWorkflowSetupVersionLastDuplicateKeyWins(t *testing.T) {
	t.Parallel()

	// A repeated key behaves as JSON.parse does: the last occurrence wins,
	// and only that one emits.
	body := "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: 18\n          node-version: 20\n"
	got := emitDecoded(t, ".github/workflows/ci.yml", body)
	checkFacts(t, got, []wantFact{
		{".github/workflows", "workflow_setup_version", "node", "<nil>", "20", "partial", "<nil>", 8, 0, 20, -1, -1},
	})
}

func TestEmitWorkflowSetupVersionSkipsNonScalarInputs(t *testing.T) {
	t.Parallel()

	// A mapping or sequence value declares nothing, the same as a non-string
	// package.json member: absent, not rejected.
	body := "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: {major: 20}\n          npm-version: [9, 10]\n          cache: true\n"
	if got := emitDecoded(t, ".github/workflows/ci.yml", body); len(got) != 0 {
		t.Fatalf("emitted %+v, want 0", got)
	}
}

func TestEmitWorkflowSetupVersionRejectsEmptyAndCredentialShapedValues(t *testing.T) {
	t.Parallel()

	// Built by concatenation: GitHub push protection blocks the whole literal.
	token := "gh" + "p_" + "0123456789abcdefghijklmnopqrstuvwxyzAB"
	body := "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: " + token + "\n          npm-version: \"\"\n          yarn-version: 9\n"
	envelopes, skip := EmitFile(testContext(".github/workflows/ci.yml"), "repo-1", ".github/workflows/ci.yml", body)
	if len(envelopes) != 1 {
		t.Fatalf("emitted %d envelopes, want only the safe neighbour", len(envelopes))
	}
	if skip.Reason != SkipReasonValueRejected || skip.Rejected != 2 {
		t.Fatalf("skip = %+v, want the token and the empty value rejected", skip)
	}
}

func TestEmitWorkflowSetupVersionFailsClosedOnMalformedYAML(t *testing.T) {
	t.Parallel()

	// An unparseable workflow declares nothing: the part before the error
	// would pass for the file, as with package.json.
	body := "jobs:\n\tbuild: [unclosed\n"
	envelopes, skip := EmitFile(testContext(".github/workflows/ci.yml"), "repo-1", ".github/workflows/ci.yml", body)
	if len(envelopes) != 0 || skip.Reason != SkipReasonManifestUnparseable {
		t.Fatalf("EmitFile = %d envelopes, skip %+v, want an unparseable skip", len(envelopes), skip)
	}
}

func TestEmitWorkflowSetupVersionAdmitsOnlyWorkflowPaths(t *testing.T) {
	t.Parallel()

	body := "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: 20\n"
	for _, path := range []string{".github/workflows/ci.yml", ".github/workflows/ci.yaml"} {
		if got := emitDecoded(t, path, body); len(got) != 1 {
			t.Errorf("%s emitted %d facts, want 1", path, len(got))
		}
	}
	// Composite actions, nested workflow directories, other YAML files and a
	// bare extension never emit, per the shared workflow-path gate.
	for _, path := range []string{
		".github/actions/setup/action.yml", ".github/workflows/team/ci.yml",
		"examples/.github/workflows/ci.yml", ".github/workflows/.yml",
		"workflows/ci.yml", "docker-compose.yml",
	} {
		if got := emitDecoded(t, path, body); len(got) != 0 {
			t.Errorf("%s emitted %d facts, want 0", path, len(got))
		}
	}
}

func TestEmitWorkflowSetupVersionSkipsOverTheManifestByteCap(t *testing.T) {
	t.Parallel()

	body := "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: 20\n" + strings.Repeat("# padding\n", MaxManifestBytesPerFile/10)
	envelopes, skip := EmitFile(testContext(".github/workflows/ci.yml"), "repo-1", ".github/workflows/ci.yml", body)
	if len(envelopes) != 0 || skip.Reason != SkipReasonByteCap || skip.ByteCap != MaxManifestBytesPerFile {
		t.Fatalf("EmitFile = %d envelopes, skip %+v, want a byte-cap skip at the manifest cap", len(envelopes), skip)
	}
}
