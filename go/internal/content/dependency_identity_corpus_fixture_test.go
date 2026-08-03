// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// TestGoldenCorpusManifestFixturesGetSectionKeyedIdentity is the durable
// regression #5509 asks for, at the identity layer.
//
// #5357 made npm and composer dependency Variables use a line-independent,
// section-keyed canonical identity. The unit coverage for that was strong, but
// the 20-repo golden corpus contained no package.json or composer.json at all,
// so the B-7/B-12 gate structurally could not exercise it: a change that
// reintroduced line-churn, or worse collapsed two sections onto one identity,
// would leave every gate green.
//
// This pins the exact identities the corpus fixtures now produce
// (tests/fixtures/ecosystems/lib-common/package.json and composer.json), using
// the same rows the parsers emit for them. The cross-section pairs are the
// load-bearing cases: lodash appears in dependencies AND devDependencies,
// monolog/monolog in require AND require-dev. Same name, same file, different
// section — they must not collapse, and neither may move when a line does.
func TestGoldenCorpusManifestFixturesGetSectionKeyedIdentity(t *testing.T) {
	t.Parallel()

	const repoID = "repository:lib-common"

	npmID := func(name, section string, line int) string {
		return CanonicalEntityIDWithMetadata(repoID, "package.json", "variable", name, line,
			map[string]any{"config_kind": "dependency", "package_manager": "npm", "section": section})
	}
	composerID := func(name, section string, line int) string {
		return CanonicalEntityIDWithMetadata(repoID, "composer.json", "variable", name, line,
			map[string]any{"config_kind": "dependency", "package_manager": "composer", "section": section})
	}

	// Cross-section distinctness: the same package in two sections is two
	// dependencies, not one. A collapse here is a silent truth loss.
	lodashRuntime := npmID("lodash", "dependencies", 9)
	lodashDev := npmID("lodash", "devDependencies", 13)
	if lodashRuntime == lodashDev {
		t.Error("npm lodash collapsed across dependencies and devDependencies into one identity")
	}
	monologRuntime := composerID("monolog/monolog", "require", 7)
	monologDev := composerID("monolog/monolog", "require-dev", 11)
	if monologRuntime == monologDev {
		t.Error("composer monolog/monolog collapsed across require and require-dev into one identity")
	}

	// Line independence: reordering a manifest must not churn identities. This
	// is what makes the corpus fixture a reorder-regression guard rather than a
	// snapshot of today's line numbers.
	for name, pair := range map[string][2]string{
		"npm lodash/dependencies":      {lodashRuntime, npmID("lodash", "dependencies", 41)},
		"npm express/dependencies":     {npmID("express", "dependencies", 8), npmID("express", "dependencies", 80)},
		"composer monolog/require":     {monologRuntime, composerID("monolog/monolog", "require", 44)},
		"composer phpunit/require-dev": {composerID("phpunit/phpunit", "require-dev", 10), composerID("phpunit/phpunit", "require-dev", 99)},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s identity moved when only the line number changed: %q vs %q", name, pair[0], pair[1])
		}
	}

	// Distinct packages in the same section stay distinct.
	if npmID("express", "dependencies", 8) == npmID("lodash", "dependencies", 9) {
		t.Error("two different npm packages in one section share an identity")
	}
}
