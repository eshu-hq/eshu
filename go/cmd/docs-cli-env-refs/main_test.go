// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestFlagsFromEshuCommandKeepsQuotedFlagValuesWithWhitespace(t *testing.T) {
	t.Parallel()

	command, flags := flagsFromEshuCommand(`eshu docs verify "--not-a-real-flag=two words"`)
	if command != "docs/verify" {
		t.Fatalf("flagsFromEshuCommand() command = %q, want docs/verify", command)
	}
	if want := []string{"--not-a-real-flag"}; !reflect.DeepEqual(flags, want) {
		t.Fatalf("flagsFromEshuCommand() flags = %#v, want %#v", flags, want)
	}
}

func TestFlagsFromEshuCommandKeepsFlagsBeforeUnmatchedQuote(t *testing.T) {
	t.Parallel()

	command, flags := flagsFromEshuCommand(`eshu docs verify --not-a-real-flag "unterminated`)
	if command != "docs/verify" {
		t.Fatalf("flagsFromEshuCommand() command = %q, want docs/verify", command)
	}
	if want := []string{"--not-a-real-flag"}; !reflect.DeepEqual(flags, want) {
		t.Fatalf("flagsFromEshuCommand() flags = %#v, want %#v", flags, want)
	}
}

func TestFlagsFromEshuCommandDoesNotTreatQuotedOrEscapedHashAsComment(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		`eshu docs verify "#literal" --not-a-real-flag`,
		`eshu docs verify \#literal --not-a-real-flag`,
	} {
		command, flags := flagsFromEshuCommand(line)
		if !strings.HasPrefix(command, "docs/verify/") {
			t.Fatalf("flagsFromEshuCommand(%q) command = %q, want docs/verify positional suffix", line, command)
		}
		if want := []string{"--not-a-real-flag"}; !reflect.DeepEqual(flags, want) {
			t.Fatalf("flagsFromEshuCommand(%q) flags = %#v, want %#v", line, flags, want)
		}
	}
}

func TestFlagsFromEshuCommandKeepsEscapedLiteralQuotesPositional(t *testing.T) {
	t.Parallel()

	_, flags := flagsFromEshuCommand(`eshu docs verify \"--not-a-real-flag\"`)
	if len(flags) != 0 {
		t.Fatalf("flagsFromEshuCommand() flags = %#v, want escaped literal quotes outside flag scope", flags)
	}
}

func TestFlagsFromEshuCommandIgnoresOperatorsInsideTrailingComment(t *testing.T) {
	t.Parallel()

	command, flags := flagsFromEshuCommand(`eshu docs verify --not-a-real-flag # explanation | example`)
	if command != "docs/verify" {
		t.Fatalf("flagsFromEshuCommand() command = %q, want docs/verify", command)
	}
	if want := []string{"--not-a-real-flag"}; !reflect.DeepEqual(flags, want) {
		t.Fatalf("flagsFromEshuCommand() flags = %#v, want %#v", flags, want)
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

func TestBaselineUpdateRejectsNewDebt(t *testing.T) {
	t.Parallel()

	existing := map[string]struct{}{
		referenceKey(reference{Kind: referenceKindEnv, Document: "guide.md", Value: "ESHU_OLD_DEBT"}): {},
	}
	unresolved := []reference{
		{Kind: referenceKindEnv, Document: "guide.md", Value: "ESHU_OLD_DEBT"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--new-debt"},
	}

	err := validateBaselineUpdate(unresolved, existing)
	if err == nil {
		t.Fatal("validateBaselineUpdate() error = nil, want new-debt rejection")
	}
	if !strings.Contains(err.Error(), "--new-debt") {
		t.Fatalf("validateBaselineUpdate() error = %q, want new reference named", err)
	}
}

func TestBaselineMembershipRejectsAtomicDebtAddition(t *testing.T) {
	t.Parallel()

	frozen := map[string]struct{}{
		referenceKey(reference{Kind: referenceKindEnv, Document: "guide.md", Value: "ESHU_OLD_DEBT"}): {},
	}
	current := map[string]struct{}{
		referenceKey(reference{Kind: referenceKindEnv, Document: "guide.md", Value: "ESHU_OLD_DEBT"}):        {},
		referenceKey(reference{Kind: referenceKindEnv, Document: "guide.md", Value: "ESHU_ATOMIC_NEW_DEBT"}): {},
	}

	err := validateBaselineMembership(current, frozen)
	if err == nil {
		t.Fatal("validateBaselineMembership() error = nil, want atomic debt rejection")
	}
	if !strings.Contains(err.Error(), "ESHU_ATOMIC_NEW_DEBT") {
		t.Fatalf("validateBaselineMembership() error = %q, want new debt named", err)
	}
}

func TestFrozenCeilingRejectsGrowthPastCodeOwnedCount(t *testing.T) {
	t.Parallel()

	ceiling := make(map[string]struct{}, frozenCeilingReferenceCount+1)
	for index := 0; index <= frozenCeilingReferenceCount; index++ {
		ceiling[fmt.Sprintf("reference-%d", index)] = struct{}{}
	}
	err := validateFrozenCeiling(ceiling)
	if err == nil {
		t.Fatal("validateFrozenCeiling() error = nil, want growth rejection")
	}
	if !strings.Contains(err.Error(), "code-owned reference count") {
		t.Fatalf("validateFrozenCeiling() error = %q, want authority named", err)
	}
}

func TestFrozenCeilingRejectsSameCountMembershipReplacement(t *testing.T) {
	t.Parallel()

	ceiling := make(map[string]struct{}, frozenCeilingReferenceCount)
	for index := 0; index < frozenCeilingReferenceCount; index++ {
		ceiling[fmt.Sprintf("replacement-%d", index)] = struct{}{}
	}
	err := validateFrozenCeiling(ceiling)
	if err == nil {
		t.Fatal("validateFrozenCeiling() error = nil, want membership rejection")
	}
	if !strings.Contains(err.Error(), "code-owned digest") {
		t.Fatalf("validateFrozenCeiling() error = %q, want digest authority named", err)
	}
}

func TestFrozenCeilingRejectsRemovedMembership(t *testing.T) {
	t.Parallel()

	ceiling := make(map[string]struct{}, frozenCeilingReferenceCount-1)
	for index := 0; index < frozenCeilingReferenceCount-1; index++ {
		ceiling[fmt.Sprintf("remaining-%d", index)] = struct{}{}
	}
	err := validateFrozenCeiling(ceiling)
	if err == nil {
		t.Fatal("validateFrozenCeiling() error = nil, want immutable membership rejection")
	}
	if !strings.Contains(err.Error(), "code-owned") {
		t.Fatalf("validateFrozenCeiling() error = %q, want code-owned authority named", err)
	}
}

func TestCeilingMembershipDigestPreservesFieldBoundaries(t *testing.T) {
	t.Parallel()

	left := "env\x00a:b\x00\x00c"
	right := "env\x00a\x00b:\x00c"
	if strings.ReplaceAll(left, "\x00", ":") != strings.ReplaceAll(right, "\x00", ":") {
		t.Fatal("fixture does not reproduce the old lossy colon encoding")
	}
	leftDigest := ceilingMembershipDigest(map[string]struct{}{left: {}})
	rightDigest := ceilingMembershipDigest(map[string]struct{}{right: {}})
	if leftDigest == rightDigest {
		t.Fatalf("boundary-aware digests collide: %s", leftDigest)
	}
}

func TestRootCommandFlagBaselineRoundTrip(t *testing.T) {
	t.Parallel()

	command, flags := flagsFromEshuCommand(`eshu --root-leading-invalid docs verify`)
	if command != "" || !reflect.DeepEqual(flags, []string{"--root-leading-invalid"}) {
		t.Fatalf("flagsFromEshuCommand() = %q, %#v, want root command flag", command, flags)
	}
	refs := unresolvedReferences([]reference{{
		Kind:     referenceKindFlag,
		Document: "guide.md",
		Command:  command,
		Value:    flags[0],
	}}, map[string]map[string]struct{}{"": {}})
	if len(refs) != 1 || refs[0].Command != "" {
		t.Fatalf("unresolvedReferences() = %#v, want one root-command reference", refs)
	}

	path := filepath.Join(t.TempDir(), "baseline.txt")
	if err := writeBaseline(path, refs); err != nil {
		t.Fatalf("writeBaseline() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written baseline: %v", err)
	}
	if !strings.Contains(string(content), "<root>::--root-leading-invalid") {
		t.Fatalf("writeBaseline() output = %q, want stable root-command sentinel", content)
	}
	baseline, err := readBaseline(path)
	if err != nil {
		t.Fatalf("readBaseline() error = %v", err)
	}
	if missing := difference(refs, baseline); len(missing) != 0 {
		t.Fatalf("baseline round trip lost root-command refs: %#v", missing)
	}
	if err := validateBaselineUpdate(refs, baseline); err != nil {
		t.Fatalf("validateBaselineUpdate() after round trip error = %v", err)
	}
}
