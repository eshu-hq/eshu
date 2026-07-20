// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"path/filepath"
	"strings"
)

// This file resolves the `environment` field for image_overrides rows
// (image_overrides.go, issue #5440), split out to keep both files under the
// repo's 500-line package-file cap. Two independent signals, in priority
// order: the ".../environments/<env>/..." path-segment signal
// (imageOverrideDirectoryEnvironment, an explicit author declaration), and
// the values-<env>.yaml/values.<env>.yaml filename signal for Helm
// (helmValuesEnvironment, a gated inference). See helmImageOverrideEnvironment
// below for how the two combine.

// helmImageOverrideEnvironmentTokens is the closed set of filename suffixes
// helmValuesEnvironment accepts as a real deployment environment. It mirrors
// isDeploymentEnvironmentToken
// (go/internal/query/repository_deployment_evidence_read_model.go:331-338)
// so the two "which words mean environment" answers agree.
//
// The token set is deliberately duplicated here rather than imported: the
// query package imports internal/parser, so importing query from this
// package would be an import cycle. This duplication is accepted for #5440;
// issue #5444 owns hoisting both call sites onto one shared implementation.
// Keep any edit to the query-side list mirrored here by hand until then.
var helmImageOverrideEnvironmentTokens = map[string]struct{}{
	"dev":         {},
	"development": {},
	"test":        {},
	"qa":          {},
	"stage":       {},
	"staging":     {},
	"uat":         {},
	"preprod":     {},
	"prod":        {},
	"production":  {},
	"sandbox":     {},
	"preview":     {},
}

// helmValuesEnvironment infers a Helm values override file's environment
// from its filename -- "values-prod.yaml" or "values.prod.yaml" -> "prod" --
// returning "" for the base values.yaml/values.yml and for any name it
// cannot confidently parse. The returned value is already lowercase (it is
// extracted from `lower`, the lowercased filename) -- see
// helmImageOverrideEnvironment's doc comment for why that matters.
//
// Accuracy guardrail (#5440 P1): a bare "values-<X>.yaml"/"values.<X>.yaml"
// split is not enough -- "values.schema.yaml" (a values-schema convention),
// "values.example.yaml" (documentation), and "values.template.yaml"
// (scaffolding) all match that shape without being an environment. The
// parsed suffix is therefore accepted ONLY when it is a member of
// helmImageOverrideEnvironmentTokens; an unrecognized suffix returns "",
// never a guess. This is a deliberately narrow, filename-only inference on
// top of that gate: it does not scan arbitrary path segments for
// environment-like keywords. Issue #5444 owns broader environment detection;
// this stays the conservative #5440 subset.
func helmValuesEnvironment(filename string) string {
	lower := strings.ToLower(filename)
	ext := filepath.Ext(lower)
	if ext != ".yaml" && ext != ".yml" {
		return ""
	}
	base := strings.TrimSuffix(lower, ext)
	for _, sep := range []string{"values-", "values."} {
		env, cut := strings.CutPrefix(base, sep)
		if !cut || env == "" {
			continue
		}
		if _, known := helmImageOverrideEnvironmentTokens[env]; known {
			return env
		}
	}
	return ""
}

// imageOverrideDirectoryEnvironment resolves the
// ".../environments/<env>/..." path-segment signal shared by both
// image_overrides producers (helmImageOverrideEnvironment below, and the
// Kustomize call site in language.go), applying guards on top of what
// environmentFromPath (observability_helpers.go) itself provides:
//
//  1. <env> must be a real DIRECTORY: at least one further path segment
//     (the values/kustomization file itself, or a deeper directory) must
//     follow it. A file sitting directly inside a directory literally named
//     "environments/" -- "environments/values.yaml",
//     "charts/foo/environments/values.yaml" -- has no author-declared
//     environment at all. environmentFromPath would otherwise return the
//     FILE'S OWN BASENAME as the "environment": the identical
//     invented-environment defect class already fixed above for the
//     values.schema.yaml filename case, just reached through the path
//     signal instead (issue #5440 P1, independent review).
//  2. The result is lowercased. environmentFromPath returns the segment's
//     raw case, while helmValuesEnvironment above always returns lowercase
//     (it works off an already-lowercased filename). Without this,
//     "environments/Prod/values.yaml" and "values-PROD.yaml" would resolve
//     the SAME declared environment to two different strings -- a case
//     fragmentation issue #5441 is about to turn into two different graph
//     join-key values instead of one (issue #5440 P1, independent review).
//  3. When a path contains more than one "environments" marker, the LAST
//     one that satisfies the guards below wins -- the declaration closest
//     to the file, not the first one encountered (issue #5440 P2-1, round-2
//     independent review). A guard-failing later marker is skipped rather
//     than clearing an earlier valid one: it carries no information of its
//     own, so an earlier, still-valid declaration is preferred over
//     discarding it.
//
// This re-walks path segments locally rather than calling
// environmentFromPath and validating its result, and the guard is
// intentionally NOT added to that shared helper: environmentFromPath has six
// callers of its own (observability.go:102,149, observability_applied.go:155,
// observability_log_routes.go:16, observability_metric_routes.go:16,
// observability_trace_routes.go:16) whose existing behavior and tests are
// out of scope for #5440 and must not change here -- the image_overrides
// Kustomize call site (language.go) no longer calls environmentFromPath
// directly at all; it calls this function instead. Those six callers have
// the identical directory-guard, multiple-marker, and case-fragmentation
// defects; that is reported to issue #5444, which owns environment
// detection, not fixed in this change.
func imageOverrideDirectoryEnvironment(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	env := ""
	for index, part := range parts {
		if part != "environments" {
			continue
		}
		// Need the candidate <env> segment (parts[index+1]) AND at least one
		// segment after THAT (the file, or a deeper directory) for <env> to
		// be a real directory rather than the file's own basename.
		if index+2 >= len(parts) {
			continue
		}
		candidate := strings.ToLower(strings.TrimSpace(parts[index+1]))
		// The captured segment can itself BE the marker keyword when two
		// "environments" markers sit back to back
		// ("environments/environments/values.yaml"): at this index the
		// directory guard above passes only because parts[index+1] is the
		// START of the NEXT marker, not a real environment name -- the
		// identical "syntactically plausible but wrong" defect class guard
		// (3) above was written to eliminate, reached through a different
		// path (issue #5440 P2, round-3 independent review). Recording it
		// would be worse than "": a values-prod.yaml sibling would have its
		// correct filename-inferred "prod" silently discarded, since
		// helmImageOverrideEnvironment only falls through to that inference
		// when the path signal is empty. Treat this occurrence as
		// guard-failing (skip, do not record) so a real marker elsewhere in
		// the path -- earlier or later -- still wins, and the filename
		// fallback can still fire when none exists.
		if candidate == "environments" {
			continue
		}
		env = candidate
	}
	return env
}

// helmImageOverrideEnvironment resolves the environment for a Helm values
// file. The two signals are deliberately asymmetric:
//
//   - The ".../environments/<env>/..." path segment
//     (imageOverrideDirectoryEnvironment above) is an explicit AUTHOR
//     DECLARATION -- someone chose to lay the repo out with an
//     "environments" directory naming this environment -- so it takes
//     priority and is NOT gated by helmImageOverrideEnvironmentTokens: it
//     returns whatever directory name the author wrote, even one outside
//     that allowlist. It IS still required to be a real directory and is
//     still lowercased, per imageOverrideDirectoryEnvironment's own guards.
//   - The values-<env>.yaml/values.<env>.yaml filename fallback is an
//     INFERENCE from a filename convention that also matches non-environment
//     files (values.schema.yaml, values.example.yaml), so it is GATED by the
//     token allowlist above and answers "" rather than guessing.
func helmImageOverrideEnvironment(path string) string {
	if env := imageOverrideDirectoryEnvironment(path); env != "" {
		return env
	}
	return helmValuesEnvironment(filepath.Base(path))
}
