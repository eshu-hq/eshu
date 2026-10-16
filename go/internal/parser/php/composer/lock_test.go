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

// writeTree writes files under a fresh repository root and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	repoRoot := t.TempDir()
	for relative, body := range files {
		path := filepath.Join(repoRoot, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repoRoot
}

// TestLocksOwnNamespaceYieldsOnlyToALongerLockedPrefix pins the own-namespace
// rule against Composer's own order, which tries the longest psr-4 prefix
// first: an own prefix suppresses a key only when it is at least as long as the
// winning locked prefix. MyCorp\ (own) loses to mycorp/logging's
// MyCorp\Logging\, while App\ (own) ties laravel/pint's App\ and suppresses,
// and a longer own MyCorp\Logging\Internal\ beats the locked MyCorp\Logging\.
func TestLocksOwnNamespaceYieldsOnlyToALongerLockedPrefix(t *testing.T) {
	t.Parallel()

	repoRoot := writeTree(t, map[string]string{
		"composer.json": `{"name": "mycorp/app", "autoload": {"psr-4": {"MyCorp\\": "src/", "App\\": "app/"}}, "autoload-dev": {"psr-4": {"MyCorp\\Logging\\Internal\\": "tests/"}}}`,
		"composer.lock": `{"packages": [
			{"name": "mycorp/logging", "autoload": {"psr-4": {"MyCorp\\Logging\\": "src/"}}},
			{"name": "laravel/pint", "autoload": {"psr-4": {"App\\": "app/"}}}
		]}`,
	})
	locks := LocksFor(repoRoot, filepath.Join(repoRoot, "src", "Service.php"))
	for _, tc := range []struct {
		fqcn string
		want string
	}{
		{`MyCorp\Logging\Logger`, "mycorp/logging"},
		{`MyCorp\Logging\Internal\Probe`, ""},
		{`MyCorp\Billing\Invoice`, ""},
		{`App\Models\User`, ""},
	} {
		if got := locks.Package(tc.fqcn); got != tc.want {
			t.Errorf("Package(%s) = %q, want %q", tc.fqcn, got, tc.want)
		}
	}
}

// TestLocksOwnFallbackPrefixNeverOutranksALockedPrefix: Composer searches the
// empty fallback after every prefixed directory, so it never beats a locked
// prefix that covers the class.
func TestLocksOwnFallbackPrefixNeverOutranksALockedPrefix(t *testing.T) {
	t.Parallel()

	repoRoot := writeTree(t, map[string]string{
		"composer.json": `{"name": "mycorp/app", "autoload": {"psr-0": {"": "src/"}}}`,
		"composer.lock": `{"packages": [{"name": "mycorp/logging", "autoload": {"psr-4": {"MyCorp\\Logging\\": "src/"}}}]}`,
	})
	locks := LocksFor(repoRoot, filepath.Join(repoRoot, "src", "Service.php"))
	if got := locks.Package(`MyCorp\Logging\Logger`); got != "mycorp/logging" {
		t.Fatalf("Package() = %q, want mycorp/logging", got)
	}
	if got := locks.Package(`App\Models\User`); got != "" {
		t.Fatalf("Package(own class) = %q, want none", got)
	}
}

// TestLockPSR4PrefixWithoutTrailingBackslashOnlyBlocks: Composer refuses a
// psr-4 prefix that does not end in '\' and the producer rule drops it, so a
// lock entry like "Acme" must never key a class: it would match AcmeOther\
// off a namespace boundary, and for Acme\Thing it is the longest covering
// prefix (Acm\ does not cover it). Such an entry still competes in the
// longest-match rule, so "A\B" blocks the shorter valid A\ for A\Bee\Thing,
// while A\Cee\Thing, which "A\B" does not cover, keys to acme/a.
func TestLockPSR4PrefixWithoutTrailingBackslashOnlyBlocks(t *testing.T) {
	t.Parallel()

	index := parseLock([]byte(`{"packages": [
		{"name": "acme/noslash", "autoload": {"psr-4": {"Acme": "src/"}}},
		{"name": "acme/root", "autoload": {"psr-4": {"Acm\\": "src/"}}},
		{"name": "acme/a", "autoload": {"psr-4": {"A\\": "src/"}}},
		{"name": "acme/ab", "autoload": {"psr-4": {"A\\B": "src/"}}}
	]}`))
	for _, fqcn := range []string{`AcmeOther\Thing`, `Acme\Thing`, `A\Bee\Thing`} {
		if name, covered := index.lookup(fqcn); name != "" || !covered {
			t.Errorf("lookup(%s) = (%q, %v), want (\"\", true)", fqcn, name, covered)
		}
	}
	if name, covered := index.lookup(`A\Cee\Thing`); name != "acme/a" || !covered {
		t.Errorf("lookup(A\\Cee\\Thing) = (%q, %v), want (\"acme/a\", true)", name, covered)
	}
}
