// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanMarkdownFindsConcreteEnvTokensAndFencedEshuFlags(t *testing.T) {
	t.Parallel()

	content := "" +
		"Set `ESHU_API_KEY` and use the `ESHU_WORKFLOW_COORDINATOR_*` family.\n" +
		"Prose --not-a-command is outside the flag scope.\n" +
		"```bash\n" +
		"eshu docs verify docs/public \\\n" +
		"  --fail-on contradicted --json\n" +
		"$ eshu graph status --workspace-root=/repo\n" +
		"cat input.json | eshu service-report --from input.json\n" +
		"```\n" +
		"```text\n" +
		"eshu docs verify --ignored-text-fence\n" +
		"```\n"

	got := scanMarkdown("reference/example.md", content)
	want := []reference{
		{Kind: referenceKindEnv, Document: "reference/example.md", Value: "ESHU_API_KEY"},
		{Kind: referenceKindFlag, Document: "reference/example.md", Command: "docs/verify/docs/public", Value: "--fail-on"},
		{Kind: referenceKindFlag, Document: "reference/example.md", Command: "docs/verify/docs/public", Value: "--json"},
		{Kind: referenceKindFlag, Document: "reference/example.md", Command: "graph/status", Value: "--workspace-root"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

func TestParseCLIHelpFindsLongFlagsAndDirectSubcommands(t *testing.T) {
	t.Parallel()

	help := "" +
		"Usage:\n  eshu docs [command]\n\n" +
		"Available Commands:\n  verify      Verify docs\n  render      Render docs\n\n" +
		"Flags:\n      --local-flag string   Local flag; unlike --other-command-flag\n  -h, --help                help\n\n" +
		"Global Flags:\n      --database string     Database\n"

	flags, subcommands := parseCLIHelp(help)
	if want := []string{"--database", "--help", "--local-flag"}; !reflect.DeepEqual(flags, want) {
		t.Fatalf("flags = %#v, want %#v", flags, want)
	}
	if want := []string{"render", "verify"}; !reflect.DeepEqual(subcommands, want) {
		t.Fatalf("subcommands = %#v, want %#v", subcommands, want)
	}
}

func TestScanMarkdownSkipsCommandFlagsAfterLeadingRootFlag(t *testing.T) {
	t.Parallel()

	content := "~~~bash\neshu --database nornicdb docs verify --not-scanned\n~~~\n"
	got := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Value: "--database"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

func TestScanMarkdownSkipsEshuLeadingShellLists(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"eshu docs verify --json | eshu definitely-not-a-command --json\n" +
		"eshu docs verify --json && eshu docs verify --unknown-after-and\n" +
		"eshu docs verify --json ; eshu docs verify --unknown-after-semicolon\n" +
		"```\n"
	if got := scanMarkdown("guide.md", content); len(got) != 0 {
		t.Fatalf("scanMarkdown() = %#v, want pipeline/list lines outside v1 scope", got)
	}
}

func TestFlagsFromEshuCommandKeepsQuotedAndEscapedOperatorsInScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{name: "double quoted pipe", line: `eshu docs verify "a|b" --quoted-pipe-invalid`},
		{name: "single quoted semicolon", line: `eshu docs verify 'a;b' --quoted-semicolon-invalid`},
		{name: "escaped ampersand", line: `eshu docs verify a\&b --escaped-ampersand-invalid`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, flags := flagsFromEshuCommand(test.line)
			if len(flags) != 1 {
				t.Fatalf("flagsFromEshuCommand(%q) flags = %#v, want one checked flag", test.line, flags)
			}
		})
	}
}

func TestScanMarkdownFindsQuotedFlagsInNestedShellFences(t *testing.T) {
	t.Parallel()

	content := "1. Nested example:\n" +
		"    ```bash\n" +
		"    eshu docs verify \"--quoted-invalid\" --plain-invalid\n" +
		"    ```\n"
	got := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--plain-invalid"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--quoted-invalid"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

func TestScanMarkdownDistinguishesIndentedLiteralAndFenceCloseSuffix(t *testing.T) {
	t.Parallel()

	content := "" +
		"    ```bash\n" +
		"    eshu docs verify --literal-block-ignored\n" +
		"    ```\n" +
		"    1. list-looking literal\n" +
		"       ```bash\n" +
		"       eshu docs verify --literal-list-block-ignored\n" +
		"       ```\n" +
		"```bash\n" +
		"```not-a-close\n" +
		"eshu docs verify --after-suffix\n" +
		"```\n"
	got := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--after-suffix"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

func TestScanMarkdownRejectsInvalidFenceClosers(t *testing.T) {
	t.Parallel()

	content := "" +
		"```bash\n" +
		"```\u00a0\n" +
		"eshu docs verify --after-nbsp\n" +
		"    ```\n" +
		"eshu docs verify --after-over-indent\n" +
		"```\n"
	got := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--after-nbsp"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--after-over-indent"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

func TestIsFenceCloseRejectsUnicodeLowByteCollision(t *testing.T) {
	t.Parallel()

	if isFenceClose("```Š", "```", 3) {
		t.Fatal("isFenceClose() = true for a non-marker rune whose low byte matches the marker")
	}
	if isFenceClose("```", "", 3) {
		t.Fatal("isFenceClose() = true for an empty marker")
	}
}

func TestScanDocsRejectsSymlinkOutsideRoot(t *testing.T) {
	t.Parallel()

	docsRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("Set `ESHU_OUTSIDE_ROOT`.\n"), 0o600); err != nil {
		t.Fatalf("write outside fixture: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(docsRoot, "outside.md")); err != nil {
		t.Skipf("symlink fixture is unavailable: %v", err)
	}

	refs, err := scanDocs(docsRoot)
	if err == nil {
		t.Fatalf("scanDocs() = %#v, nil, want fail-closed error for a symlink escaping the docs root", refs)
	}
}

func TestCommandFlagTruthRejectsUnknownRootCommand(t *testing.T) {
	t.Parallel()

	truth := map[string]map[string]struct{}{
		"":             {"--help": {}},
		"docs":         {"--help": {}},
		"docs/verify":  {"--json": {}},
		"graph/status": {"--workspace-root": {}},
	}
	if command, known := commandFlagTruth("definitely-not-a-command", "--help", truth); known || command != "definitely-not-a-command" {
		t.Fatalf("commandFlagTruth() = %q, %t, want unknown original command", command, known)
	}
	if command, known := commandFlagTruth("docs/definitely-not-a-command", "--help", truth); known || command != "docs/definitely-not-a-command" {
		t.Fatalf("nested commandFlagTruth() = %q, %t, want unknown original command", command, known)
	}
}

func TestCompareReferencesUsesRegistryAndCLITruth(t *testing.T) {
	t.Parallel()

	references := []reference{
		{Kind: referenceKindEnv, Document: "a.md", Value: "ESHU_API_KEY"},
		{Kind: referenceKindEnv, Document: "a.md", Value: "ESHU_NOT_REGISTERED"},
		{Kind: referenceKindFlag, Document: "b.md", Command: "docs/verify/docs/public", Value: "--json"},
		{Kind: referenceKindFlag, Document: "b.md", Command: "docs/verify/docs/public", Value: "--not-registered"},
		{Kind: referenceKindFlag, Document: "b.md", Command: "docs/verify/docs/public", Value: "--workspace-root"},
	}
	knownFlags := map[string]map[string]struct{}{
		"":             {"--database": {}},
		"docs":         {"--help": {}},
		"docs/verify":  {"--json": {}},
		"graph/status": {"--workspace-root": {}},
	}

	got := unresolvedReferences(references, knownFlags)
	want := []reference{
		{Kind: referenceKindEnv, Document: "a.md", Value: "ESHU_NOT_REGISTERED"},
		{Kind: referenceKindFlag, Document: "b.md", Command: "docs/verify", Value: "--not-registered"},
		{Kind: referenceKindFlag, Document: "b.md", Command: "docs/verify", Value: "--workspace-root"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unresolvedReferences() = %#v, want %#v", got, want)
	}
}
