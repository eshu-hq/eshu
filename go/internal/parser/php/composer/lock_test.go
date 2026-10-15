// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockLookupLongestPrefixTiesAndBlockers(t *testing.T) {
	t.Parallel()

	index := parseLock([]byte(`{
  "packages": [
    {"name": "acme/all", "autoload": {"psr-4": {"Acme\\": "src/"}}},
    {"name": "acme/logging", "autoload": {"psr-4": {"\\Acme\\Logging\\": ["src/", "lib/"]}}},
    {"name": "acme/one", "autoload": {"psr-4": {"Tie\\": "src/"}}},
    {"name": "acme/two", "autoload": {"psr-4": {"Tie\\": "src/"}}},
    {"name": "acme/legacy", "autoload": {"psr-0": {"Acme\\Legacy\\": "src/"}}},
    {"name": "Acme/Bad", "autoload": {"psr-4": {"Acme\\Bad\\": "src/"}}}
  ],
  "packages-dev": [
    {"name": "acme/dev", "autoload": {"psr-4": {"Dev\\": "src/"}}}
  ]
}`))
	for _, tc := range []struct {
		fqcn        string
		wantName    string
		wantCovered bool
	}{
		{`Acme\Logging\Logger`, "acme/logging", true},
		{`Acme\Other\Thing`, "acme/all", true},
		{`Tie\Thing`, "", true},         // two packages, one prefix
		{`Acme\Legacy\Thing`, "", true}, // psr-0 blocks the shorter Acme\ prefix
		{`Acme\Bad\Thing`, "", true},    // a malformed name blocks it too
		{`Dev\Fake`, "acme/dev", true},  // packages-dev count
		{`Acme`, "", false},             // the namespace itself is not a class under it
		{`Other\Thing`, "", false},
	} {
		name, covered := index.lookup(tc.fqcn)
		if name != tc.wantName || covered != tc.wantCovered {
			t.Errorf("lookup(%s) = (%q, %v), want (%q, %v)", tc.fqcn, name, covered, tc.wantName, tc.wantCovered)
		}
	}
}

func TestParseLockToleratesMalformedDocuments(t *testing.T) {
	t.Parallel()

	for _, content := range []string{`{"packages": 3}`, `[`, `{"packages": [{"name": "a/b", "autoload": []}]}`} {
		if index := parseLock([]byte(content)); index.size() != 0 {
			t.Errorf("parseLock(%s) holds %d prefixes, want none", content, index.size())
		}
	}
}

func TestProducerPackageMatchesEveryMappedDirectory(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	write := func(relative, body string) {
		t.Helper()
		path := filepath.Join(repoRoot, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("composer.json", `{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": ["src/", "lib"], "Acme\\NoSlash": "x/"}}}`)
	for _, tc := range []struct {
		path string
		fqcn string
		want string
	}{
		{"src/Logger.php", `Acme\Logging\Logger`, "acme/logging"},
		{"lib/Handler/Stream.php", `Acme\Logging\Handler\Stream`, "acme/logging"},
		{"src/Handler/Stream.php", `Acme\Logging\Stream`, ""},
		{"x/Thing.php", `Acme\NoSlashThing`, ""}, // a prefix without the trailing '\' is invalid
	} {
		if got := ProducerPackage(repoRoot, filepath.Join(repoRoot, tc.path), tc.fqcn); got != tc.want {
			t.Errorf("ProducerPackage(%s, %s) = %q, want %q", tc.path, tc.fqcn, got, tc.want)
		}
	}
}
