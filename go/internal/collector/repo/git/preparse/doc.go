// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package preparse decides, before the parse, whether a forced reconcile of a
// git repository would emit the same facts as the scope's active generation
// (#7978 follow-up, ruling a9-143).
//
// The daily reconcile sweep used to fetch, check out, discover, parse, and
// build the whole fact stream of every unchanged repository, and only then let
// the ingestion store compare the post-parse freshness hint and re-project the
// active generation. Identity is a cheap stand-in for that hint: the content
// version, the commit, the default branch, and every configuration input that
// changes emitted facts (Config). A snapshot is a deterministic function of
// those inputs, so an equal identity means an equal hint, and the parse can
// be skipped.
//
// Decide applies the rules: only interval_elapsed and graph_dirty may skip;
// the active generation must be a full at the remote head with an equal stored
// identity; repositories with submodules and dirty checkouts always parse; and
// a valve forces a parse at least every MaxSkips intervals, bounding any input
// the identity does not cover. Every other outcome parses as before.
//
// FoldedEnvKeys and NonOutputEnvKeys classify every env key read by the git
// collector, discovery, and the parsers; TestEveryEnvKeyIsClassified fails on
// a new key until someone decides whether it changes output.
//
// The package is pure: no I/O, no clock, no git. The git collector supplies
// the inputs and runs the tree check.
package preparse
