// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestDefaultEngineParsePathPHPImportKindReadsKindToken pins #8222: the import
// kind comes from the grammar's `function`/`const` kind token, never from the
// statement prefix. A namespace that merely starts with Function or Const is a
// class import.
func TestDefaultEngineParsePathPHPImportKindReadsKindToken(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "import_kinds.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace App;

use Functional\Collection;
use Constraint\Rule;
use FUNCTIONAL\Upper;
use Constants\Limits as Lim;
use functional\Lower;
use function Fn\helper;
use FUNCTION Fn\Shouted;
use const Cn\LIMIT;
use CONST Cn\SHOUTED as LOUD;
use Functional\{Alpha, Beta as B};
use function Fn\{gamma, delta as d};
use const Cn\{EPS, ZETA as Z};
/* lead */ use /* mid */ function Fn\commented;
	use   const   Cn\SPACED;
use Functional\Multi, Constraint\Multi2;
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	cases := []struct {
		name string
		kind string
	}{
		{`Functional\Collection`, "use"},
		{`Constraint\Rule`, "use"},
		{`FUNCTIONAL\Upper`, "use"},
		{`Constants\Limits`, "use"},
		{`functional\Lower`, "use"},
		{`Fn\helper`, "function"},
		{`Fn\Shouted`, "function"},
		{`Cn\LIMIT`, "const"},
		{`Cn\SHOUTED`, "const"},
		{`Functional\Alpha`, "use"},
		{`Functional\Beta`, "use"},
		{`Fn\gamma`, "function"},
		{`Fn\delta`, "function"},
		{`Cn\EPS`, "const"},
		{`Cn\ZETA`, "const"},
		{`Fn\commented`, "function"},
		{`Cn\SPACED`, "const"},
		{`Functional\Multi`, "use"},
		{`Constraint\Multi2`, "use"},
	}
	for _, tc := range cases {
		item := parsertest.AssertBucketItemByName(t, got, "imports", tc.name)
		parsertest.AssertStringFieldValue(t, item, "import_type", tc.kind)
	}
	parsertest.AssertStringFieldValue(
		t,
		parsertest.AssertBucketItemByName(t, got, "imports", `Constants\Limits`),
		"alias", "Lim",
	)
}

// TestDefaultEngineParsePathPHPKeysClassInNamespaceStartingWithKindWord pins the
// #7601 symptom behind #8222: a class imported from a namespace that starts
// with Function or Const is a class import, so `new` on it gets a package key.
func TestDefaultEngineParsePathPHPKeysClassInNamespaceStartingWithKindWord(t *testing.T) {
	t.Parallel()

	source := `<?php
namespace App;

use Functional\Collection;
use Constraint\Rule;

class Service
{
    public function run(): void
    {
        $items = new Collection();
        $rule = new Rule();
    }
}
`
	got := parsePHPProducerRepo(t, map[string]string{
		"composer.json": `{"name": "acme/app", "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"composer.lock": `{"packages": [
  {"name": "acme/functional", "autoload": {"psr-4": {"Functional\\": "src/"}}},
  {"name": "acme/constraint", "autoload": {"psr-4": {"Constraint\\": "src/"}}}
]}`,
		"app/Service.php": source,
	}, "app/Service.php")

	for _, tc := range []struct{ marker, fullName, want string }{
		{"$items = new Collection();", "Collection", `package:acme/functional#Functional\Collection`},
		{"$rule = new Rule();", "Rule", `package:acme/constraint#Constraint\Rule`},
	} {
		call := phpCallOnLine(t, got, source, tc.marker, tc.fullName)
		parsertest.AssertStringFieldValue(t, call, "package_export_symbol", tc.want)
	}
}
