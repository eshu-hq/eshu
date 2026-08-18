// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reposelector turns an operator-supplied repository selector -- a
// canonical ID, a repository name, an org/name slug, or a filesystem path --
// into the canonical repository ID the Eshu API keys everything else by.
//
// Resolve lists repositories through the injected Getter and returns the one
// entry the selector names, erroring when the selector matches nothing or
// matches more than one repository. Matches is the same rule applied to a
// single already-fetched Entry, which is what the `eshu first-run` and
// `eshu hosted setup` scope checks need: they hold the listing already and
// only want the predicate. ListResponse and Entry are the wire shapes both
// paths decode.
//
// Path fields and identity fields are matched differently on purpose. ID,
// Name, and RepoSlug must equal the selector byte for byte, while Path and
// LocalPath also match after filepath.Clean and after symlink resolution on
// both sides. A repository whose Name happens to look like a path is
// therefore not canonicalized into matching a different path.
//
// The symlink resolution reads the real filesystem, so it is host-dependent
// by design: it is what lets an operator name a repository by the path they
// are standing in when that path reaches the checkout through a symlink. It
// is best-effort -- a selector naming nothing on disk matches on the cleaned
// form alone.
//
// The package holds no cobra, no process streams, and no environment reads.
// go/cmd/eshu/repository_selector.go is the wrapper that owns those: it reads
// the --repo and --repo-id flags, short-circuits --repo-id as already exact,
// and hands the built *APIClient in as a Getter.
package reposelector
